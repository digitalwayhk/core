package oltp

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/zeromicro/go-zero/core/logx"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

var mysqldsn = "%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local&timeout=%ds&readTimeout=%ds&writeTimeout=%ds"

// 表缓存相关（参考 sqlite.go）
var (
	mysqlTableCache    sync.Map
	mysqlMigrationLock sync.Mutex
)

type MysqlTableCacheKey struct {
	DSN       string
	TableName string
}

type Mysql struct {
	Name          string `json:"name"`
	Host          string `json:"host"`
	Port          uint   `json:"port"`
	ConMax        uint   // 最大连接数
	ConPool       uint   // 连接池大小
	User          string `json:"user"`
	Pass          string `json:"pass"`
	db            *gorm.DB
	tx            *gorm.DB
	TimeOut       uint `json:"timeout"`
	ReadTimeOut   uint
	WriteTimeOut  uint
	isTransaction bool
	IsLog         bool
	AutoTable     bool
}

func (own *Mysql) init(data interface{}) error {
	if own.Name == "" {
		err := own.GetDBName(data)
		if err != nil {
			return err
		}
	}

	// 🔧 健康检查与重建连接
	if err := own.ensureValidConnection(); err != nil {
		return err
	}

	if own.isTransaction {
		if own.tx == nil {
			own.tx = own.db.Begin()
		}
	}

	return nil
}
func ClearMysqlTableCache() {
	mysqlTableCache = sync.Map{}
}

// 🔧 新增：确保连接有效
func (own *Mysql) ensureValidConnection() error {
	if own.db == nil {
		_, err := own.GetDB()
		return err
	}

	// 检查连接健康状态
	sqlDB, err := own.db.DB()
	if err != nil {
		logx.Errorf("获取底层数据库连接失败: %v", err)
		return own.recreateConnection()
	}

	// 测试连接
	if err := sqlDB.Ping(); err != nil {
		logx.Errorf("数据库连接ping失败: %v", err)
		return own.recreateConnection()
	}

	return nil
}

// 🔧 新增：重建连接
func (own *Mysql) recreateConnection() error {
	own.cleanupCurrentConnection()
	newDB, err := own.GetDB()
	if err != nil {
		return fmt.Errorf("重建数据库连接失败: %v", err)
	}
	own.db = newDB
	logx.Infof("MySQL 数据库连接已重建: %s", own.Name)
	return nil
}
func (own *Mysql) RecreateConnection() error {
	return own.recreateConnection()
}

// 🔧 新增：清理当前连接
func (own *Mysql) cleanupCurrentConnection() {
	if own.db != nil {
		if sqlDB, err := own.db.DB(); err == nil {
			sqlDB.Close()
		}
		own.db = nil
	}
	// 从连接池中移除
	dsn := fmt.Sprintf(mysqldsn, own.User, own.Pass, own.Host, own.Port, own.Name, own.TimeOut, own.ReadTimeOut, own.WriteTimeOut)
	connManager.SetConnection(dsn, nil)
}

// 🔧 新增：延迟表检查方法
func (own *Mysql) ensureTable(data interface{}) error {
	return own.HasTable(data)
}

func NewMysql(host, user, pass string, port uint, islog bool, autotable bool) *Mysql {
	return &Mysql{
		Host:         host,
		Port:         port,
		ConMax:       100,
		ConPool:      20,
		User:         user,
		Pass:         pass,
		TimeOut:      10,
		ReadTimeOut:  30,
		WriteTimeOut: 60,
		IsLog:        islog,
		AutoTable:    autotable,
	}
}

func (own *Mysql) GetDBName(data interface{}) error {
	if idb, ok := data.(types.IDBName); ok {
		own.Name = idb.GetRemoteDBName()
		if own.Name == "" {
			return errors.New("db name is empty")
		}
		return nil
	}
	return errors.New("db name is empty")
}

func (own *Mysql) GetModelDB(model interface{}) (interface{}, error) {
	err := own.init(model)
	return own.db, err
}

func (own *Mysql) GetDB() (*gorm.DB, error) {
	if own.db == nil {
		dsn := fmt.Sprintf(mysqldsn, own.User, own.Pass, own.Host, own.Port, own.Name, own.TimeOut, own.ReadTimeOut, own.WriteTimeOut)

		// 🔧 检查连接池缓存
		if db, ok := connManager.GetConnection(dsn); ok {
			if db != nil {
				// 检查连接健康状态
				if sqlDB, err := db.DB(); err == nil {
					if err := sqlDB.Ping(); err == nil {
						own.db = db
						return db, nil
					} else {
						// 连接不健康，关闭并清理
						sqlDB.Close()
						connManager.SetConnection(dsn, nil)
					}
				}
			}
		}

		dia := mysql.Open(dsn)
		db, err := gorm.Open(dia, &gorm.Config{
			NamingStrategy: schema.NamingStrategy{
				SingularTable: true,
				NoLowerCase:   true,
			},
			PrepareStmt:              true,  // 🔧 启用预编译语句
			DisableAutomaticPing:     false, // 🔧 启用ping检测
			DisableNestedTransaction: true,
			SkipDefaultTransaction:   true,
		})

		if config.INITSERVER && !utils.IsTest() {
			db.DryRun = true
		} else {
			if own.IsLog {
				db.Logger = logger.Default.LogMode(logger.Info)
			} else {
				db.Logger = logger.Default.LogMode(logger.Error)
			}
			db.DryRun = false
		}

		if err != nil {
			return nil, err
		}

		mysqldb, err := db.DB()
		if err != nil {
			return nil, err
		}

		// 🔧 优化连接池参数
		mysqldb.SetMaxOpenConns(int(own.ConMax))
		mysqldb.SetMaxIdleConns(int(own.ConPool))
		mysqldb.SetConnMaxLifetime(30 * time.Minute) // 🔧 延长连接生存时间
		mysqldb.SetConnMaxIdleTime(10 * time.Minute) // 🔧 新增：空闲超时

		own.db = db
		if !db.DryRun {
			connManager.SetConnection(dsn, db)
		}
	}
	return own.db, nil
}

func (own *Mysql) HasTable(model interface{}) error {
	// 🔧 移除 db.DryRun 检查,测试环境必须创建表
	if config.INITSERVER && !utils.IsTest() {
		return nil
	}

	if own.db == nil {
		db, err := own.GetDB()
		if err != nil {
			return err
		}
		own.db = db
	}

	tableName := own.db.NamingStrategy.TableName(reflect.TypeOf(model).Elem().Name())
	dsn := fmt.Sprintf(mysqldsn, own.User, own.Pass, own.Host, own.Port, own.Name, own.TimeOut, own.ReadTimeOut, own.WriteTimeOut)
	cacheKey := MysqlTableCacheKey{
		DSN:       dsn,
		TableName: tableName,
	}

	// 🔧 快速路径:检查缓存(无锁)
	if _, exists := mysqlTableCache.Load(cacheKey); exists {
		return nil
	}

	// 🔧 慢路径:加锁后再次检查并迁移
	mysqlMigrationLock.Lock()
	defer mysqlMigrationLock.Unlock()

	// 🔧 双重检查(避免重复迁移)
	if _, exists := mysqlTableCache.Load(cacheKey); exists {
		return nil
	}

	// 🔧 快速检查表是否存在(无需AutoMigrate)
	var count int64
	err := own.db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=? AND table_name=?",
		own.Name, tableName).Scan(&count).Error
	if err == nil && count > 0 {
		mysqlTableCache.Store(cacheKey, true)
		return nil
	}

	// 🔧 表不存在时才执行迁移
	err = own.db.AutoMigrate(model)
	if err != nil {
		// 🔧 忽略"表已存在"错误
		if strings.Contains(err.Error(), "already exists") ||
			strings.Contains(err.Error(), "42S01") {
			mysqlTableCache.Store(cacheKey, true)
			return nil
		}
		return fmt.Errorf("表迁移失败 %s: %v", tableName, err)
	}

	mysqlTableCache.Store(cacheKey, true)
	return own.processNestedTablesOptimized(model, make(map[string]bool), 0, 2)
}

// 🔧 新增：优化嵌套表处理
func (own *Mysql) processNestedTablesOptimized(model interface{}, processed map[string]bool, depth, maxDepth int) error {
	if depth >= maxDepth {
		return nil
	}

	typeName := utils.GetTypeName(model)
	if processed[typeName] {
		return nil
	}
	processed[typeName] = true

	utils.DeepForItem(model, func(field, parent reflect.StructField, kind utils.TypeKind) {
		if kind == utils.Array {
			t := field.Type.Elem()
			if t.Kind() == reflect.Ptr {
				t = t.Elem()
			}
			name1 := t.Name()
			pname := utils.GetTypeName(model)
			if name1 == pname {
				return
			}
			obj := reflect.New(t).Interface()
			err := own.db.AutoMigrate(obj)
			if err != nil {
				logx.Errorf("处理嵌套表失败: %s -> %s, 错误: %v", pname, name1, err)
			}
			own.processNestedTablesOptimized(obj, processed, depth+1, maxDepth)
		}
	})

	return nil
}

func (own *Mysql) Load(item *types.SearchItem, result interface{}) error {
	err := own.init(item.Model)
	if err != nil {
		return err
	}
	// 🔧 确保表存在
	err = own.ensureTable(item.Model)
	if err != nil {
		return err
	}
	if item.IsStatistical {
		return sum(own.db, item, result)
	}
	if own.isTransaction {
		return load(own.tx, item, result)
	}
	return load(own.db, item, result)
}

func (own *Mysql) Raw(sql string, data interface{}) error {
	obj := utils.NewArrayItem(data)
	err := own.init(obj)
	if err != nil {
		return err
	}
	own.db.Raw(sql).Scan(data)
	return own.db.Error
}

func (own *Mysql) Exec(sql string, data interface{}) error {
	err := own.init(data)
	if err != nil {
		return err
	}
	own.db.Exec(sql, data)
	return own.db.Error
}

func (own *Mysql) Transaction() {
	own.isTransaction = true
}

func (own *Mysql) Insert(data interface{}) error {
	err := own.init(data)
	if err != nil {
		return err
	}

	// 🔧 在事务外确保表存在(避免事务中调用HasTable导致死锁)
	if !own.isTransaction {
		err = own.ensureTable(data)
		if err != nil {
			return err
		}
	}

	if own.isTransaction {
		err := createData(own.tx, data)
		if err != nil {
			// 🔧 事务中发生错误时自动回滚
			// 不在这里回滚，让调用者决定是否回滚
			return err
		}
		return nil
	}

	err = createData(own.db, data)
	if err != nil {
		return own.errorHandler(err, data, createData)
	}
	return err
}

func (own *Mysql) Update(data interface{}) error {
	err := own.init(data)
	if err != nil {
		return err
	}
	err = own.ensureTable(data)
	if err != nil {
		return err
	}

	if own.isTransaction {
		err := updateData(own.tx, data)
		if err != nil {
			// 不在这里回滚，让调用者决定是否回滚
			return err
		}
		return nil
	}

	err = updateData(own.db, data)
	if err != nil {
		err = own.errorHandler(err, data, updateData)
	}
	return err
}

func (own *Mysql) Delete(data interface{}) error {
	err := own.init(data)
	if err != nil {
		return err
	}
	err = own.ensureTable(data)
	if err != nil {
		return err
	}

	if own.isTransaction {
		err := deleteData(own.tx, data)
		if err != nil {
			// 不在这里回滚，让调用者决定是否回滚
			return err
		}
		return nil
	}

	err = deleteData(own.db, data)
	if err != nil {
		err = own.errorHandler(err, data, deleteData)
	}
	return err
}

// 🔧 新增：错误处理（参考 sqlite.go）
func (own *Mysql) errorHandler(err error, data interface{}, fn func(db *gorm.DB, data interface{}) error) error {
	if err == nil {
		return nil
	}
	// 检查是否是列不存在的错误
	if strings.Contains(err.Error(), "Unknown column") ||
		strings.Contains(err.Error(), "doesn't exist") ||
		strings.Contains(err.Error(), "Duplicate column name") {
		err := own.db.AutoMigrate(data)
		if err == nil {
			return fn(own.db, data)
		}
	}
	return err
}

func (own *Mysql) Commit() error {
	own.isTransaction = false
	if own.tx != nil {
		err := own.tx.Commit().Error
		own.tx = nil
		return err
	}
	return nil
}
func (own *Mysql) Rollback() error {
	own.isTransaction = false
	if own.tx != nil {
		err := own.tx.Rollback().Error
		own.tx = nil
		return err
	}
	return nil
}

func (own *Mysql) GetRunDB() interface{} {
	return own.db
}
