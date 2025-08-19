package oltp

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/local"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/zeromicro/go-zero/core/logx"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

type Sqlite struct {
	Name         string
	Size         float64 //库大小
	UpdateTime   int32   //数据最后更新时间
	Path         string  //库文件路径
	db           *gorm.DB
	tx           *gorm.DB
	isTansaction bool
	tables       map[string]*TableMaster
	IsLog        bool
}

func NewSqlite() *Sqlite {
	sql := &Sqlite{
		tables: make(map[string]*TableMaster),
	}
	return sql
}

func (own *Sqlite) init(data interface{}) error {
	err := own.GetDBName(data)
	if err != nil {
		return err
	}

	// 🔧 修复：检查数据库文件是否存在
	dns, err := own.getPath()
	if err != nil {
		return err
	}

	// 如果数据库文件不存在，清除连接缓存
	if !utils.IsFile(dns) {
		connManager.SetConnection(dns, nil)
		own.db = nil
		own.tx = nil
	}

	if own.db == nil {
		_, err := own.GetDB()
		if err != nil {
			return err
		}
	}

	if own.isTansaction {
		if own.tx == nil {
			own.tx = own.db.Begin()
		}
	}

	return nil
}

// 新增：延迟表检查方法
func (own *Sqlite) ensureTable(data interface{}) error {
	return own.HasTable(data)
}
func (own *Sqlite) GetDBName(data interface{}) error {
	if idb, ok := data.(types.IDBName); ok {
		own.Name = idb.GetLocalDBName()
		if own.Name == "" {
			return errors.New("db name is empty")
		}
		return nil
	}
	return errors.New("db name is empty")
}
func (own *Sqlite) GetModelDB(model interface{}) (interface{}, error) {
	err := own.init(model)
	return own.db, err
}
func (own *Sqlite) DeleteDB() error {
	dns, err := own.getPath()
	if err != nil {
		return err
	}

	// 🔧 修复：在删除文件前先关闭所有数据库连接
	if err := own.closeAllConnections(); err != nil {
		logx.Errorf("关闭数据库连接失败: %v", err)
		// 继续执行，不要因为关闭连接失败而阻止删除文件
	}

	// 🔧 修复：清除连接缓存（在删除文件前）
	connManager.SetConnection(dns, nil)

	// 🔧 修复：重置当前实例的连接
	own.db = nil
	own.tx = nil
	own.isTansaction = false

	// 删除数据库文件
	err = utils.DeleteFile(dns)
	if err != nil {
		logx.Errorf("删除数据库文件失败: %s, 错误: %v", dns, err)
		return err
	}

	// 🔧 修复：清除表缓存
	own.clearTableCache()

	logx.Infof("✅ 成功删除数据库文件: %s", dns)
	return nil
}

// 🔧 新增：关闭所有数据库连接
func (own *Sqlite) closeAllConnections() error {
	var lastError error

	// 关闭事务连接
	if own.tx != nil {
		if tx := own.tx.Rollback(); tx != nil {
			logx.Errorf("回滚事务失败: %v", tx.Error)
			lastError = tx.Error
		}
		own.tx = nil
		own.isTansaction = false
	}

	// 关闭主数据库连接
	if own.db != nil {
		if sqlDB, err := own.db.DB(); err == nil {
			if err := sqlDB.Close(); err != nil {
				logx.Errorf("关闭数据库连接失败: %v", err)
				lastError = err
			}
		}
		own.db = nil
	}

	return lastError
}

// 🔧 新增：清除表缓存
func (own *Sqlite) clearTableCache() {
	// 清除与此数据库相关的表缓存
	tableCache.Range(func(key, value interface{}) bool {
		if cacheKey, ok := key.(TableCacheKey); ok {
			if cacheKey.DBPath == own.Path {
				tableCache.Delete(key)
			}
		}
		return true
	})
}
func (own *Sqlite) getPath() (string, error) {
	key := own.Name
	if key == "" {
		key = "models"
	}

	path, err := local.GetDbPath(key)
	if err != nil {
		return "", err
	}

	dns := path + ".ldb"
	own.Path = dns
	return dns, nil
}
func (own *Sqlite) newDB() (*gorm.DB, error) {
	logx.Infof("🆕 创建新的数据库连接: %s", own.Path)
	dia := sqlite.Open(own.Path)
	db, err := gorm.Open(dia, &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
			NoLowerCase:   true,
		},
		PrepareStmt:              false, // 暂时禁用预编译语句减少内存
		DisableAutomaticPing:     true,
		DisableNestedTransaction: true,
		SkipDefaultTransaction:   true,                                  // 跳过默认事务
		Logger:                   logger.Default.LogMode(logger.Silent), // 静默模式
	})

	if err != nil {
		return nil, err
	}

	// 最小连接池配置
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxIdleConns(1)                   // 最小空闲连接
	sqlDB.SetMaxOpenConns(2)                   // 最小打开连接
	sqlDB.SetConnMaxLifetime(10 * time.Minute) // 短生存时间
	return db, nil
}

// 🔧 修复：GetDB 方法添加文件存在性检查
func (own *Sqlite) GetDB() (*gorm.DB, error) {
	dns, err := own.getPath()
	if err != nil {
		return nil, err
	}

	// 🔧 修复：检查文件是否存在，如果不存在则清除缓存
	if !utils.IsFile(dns) {
		connManager.SetConnection(dns, nil)
		own.db = nil
	}

	if db, ok := connManager.GetConnection(dns); ok {
		if db != nil && db.Error == nil {
			// 🔧 修复：验证连接是否仍然有效
			if err := db.Exec("SELECT 1").Error; err == nil {
				own.db = db
				logx.Infof("🔄 复用现有数据库连接: %s", dns)
				return db, nil
			} else {
				// 连接无效，清除缓存
				connManager.SetConnection(dns, nil)
			}
		}
	}

	own.db, err = own.newDB()
	if err != nil {
		return nil, err
	}

	if !config.INITSERVER {
		connManager.SetConnection(dns, own.db)
	}
	return own.db, nil
}

func (own *Sqlite) HasTable(model interface{}) error {
	if config.INITSERVER || (own.db != nil && own.db.DryRun) {
		return nil
	}

	if own.db == nil {
		db, err := own.GetDB()
		if err != nil {
			return err
		}
		own.db = db
	}

	if _, ok := model.(types.IDBSQL); ok {
		return nil
	}

	// 获取表名
	tableName := own.db.NamingStrategy.TableName(reflect.TypeOf(model).Elem().Name())
	cacheKey := TableCacheKey{
		DBPath:    own.Path,
		TableName: tableName,
	}

	// 检查缓存
	if _, exists := tableCache.Load(cacheKey); exists {
		return nil // 已处理过，直接返回
	}

	// 使用锁防止并发迁移
	migrationLock.Lock()
	defer migrationLock.Unlock()

	// 双重检查
	if _, exists := tableCache.Load(cacheKey); exists {
		return nil
	}

	logx.Infof("检查表: %s", tableName)

	// 快速检查表是否存在，避免调用复杂的 Migrator
	var count int64
	err := own.db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", tableName).Scan(&count).Error
	if err == nil && count > 0 {
		tableCache.Store(cacheKey, true)
		return nil // 表已存在
	}

	logx.Infof("开始创建表: %s", tableName)

	// 只在表不存在时才执行迁移
	err = own.db.AutoMigrate(model)
	if err != nil {
		logx.Errorf("创建表失败: %s, 错误: %v", tableName, err)
		return err
	}

	// 缓存结果
	tableCache.Store(cacheKey, true)
	logx.Infof("表创建完成: %s", tableName)

	// 处理嵌套表，但限制深度
	return own.processNestedTablesOptimized(model, make(map[string]bool), 0, 2)
}

// 优化嵌套表处理，添加深度限制
func (own *Sqlite) processNestedTablesOptimized(model interface{}, processed map[string]bool, depth, maxDepth int) error {
	if depth >= maxDepth {
		return nil // 超过最大深度，停止递归
	}

	typeName := utils.GetTypeName(model)
	if processed[typeName] {
		return nil // 已处理过，避免循环
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
				return // 避免自引用
			}

			obj := reflect.New(t).Interface()
			if err := own.processNestedTablesOptimized(obj, processed, depth+1, maxDepth); err != nil {
				logx.Error("处理嵌套表失败:", err)
			}
		}
	})

	return nil
}

func (own *Sqlite) Load(item *types.SearchItem, result interface{}) error {
	err := own.init(item.Model)
	if err != nil {
		return err
	}
	// 确保表存在
	err = own.ensureTable(item.Model)
	if err != nil {
		return err
	}
	if item.IsStatistical {
		return sum(own.db, item, result)
	}
	if own.isTansaction {
		return load(own.tx, item, result)
	}
	return load(own.db, item, result)
}
func (own *Sqlite) Raw(sql string, data interface{}) error {
	obj := utils.NewArrayItem(data)
	err := own.init(obj)
	if err != nil {
		return err
	}
	own.db.Raw(sql).Scan(data)
	return own.db.Error
}
func (own *Sqlite) Exec(sql string, data interface{}) error {
	err := own.init(data)
	if err != nil {
		return err
	}
	own.db.Exec(sql, data)
	return own.db.Error
}

func (own *Sqlite) Transaction() {
	own.isTansaction = true
}
func (own *Sqlite) Insert(data interface{}) error {
	err := own.init(data)
	if err != nil {
		return err
	}
	// 确保表存在
	err = own.ensureTable(data)
	if err != nil {
		return err
	}
	if own.isTansaction {
		err := createData(own.tx, data)
		if err != nil {
			fmt.Println(own.Path)
			own.tx.Rollback()
			return err
		}
		return nil
	}
	err = createData(own.db, data)
	if err != nil {
		err = own.errorHandler(err, data, createData)
	}
	return err
}
func (own *Sqlite) errorHandler(err error, data interface{}, fn func(db *gorm.DB, data interface{}) error) error {
	if err == nil {
		return nil
	}
	// 检查是否是列不存在的错误
	if strings.Contains(err.Error(), "no such column") ||
		strings.Contains(err.Error(), "has no column named") ||
		strings.Contains(err.Error(), "ambiguous column name") ||
		strings.Contains(err.Error(), "no such table") ||
		strings.Contains(err.Error(), "datatype mismatch") {
		err := own.db.AutoMigrate(data)
		if err == nil {
			return fn(own.db, data)
		}
	}
	return err
}
func (own *Sqlite) Update(data interface{}) error {
	err := own.init(data)
	if err != nil {
		return err
	}
	// 确保表存在
	err = own.ensureTable(data)
	if err != nil {
		return err
	}
	if own.isTansaction {
		err := updateData(own.tx, data)
		if err != nil {
			fmt.Println(own.Path)
			own.tx.Rollback()
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
func (own *Sqlite) Delete(data interface{}) error {
	err := own.init(data)
	if err != nil {
		return err
	}
	// 确保表存在
	err = own.ensureTable(data)
	if err != nil {
		return err
	}
	if own.isTansaction {
		err := deleteData(own.tx, data)
		if err != nil {
			fmt.Println(own.Path)
			own.tx.Rollback()
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
func (own *Sqlite) Commit() error {
	own.isTansaction = false
	if own.tx != nil {
		own.tx.Commit()
		err := own.tx.Error
		own.tx = nil
		return err
	}
	return nil
}
func (own *Sqlite) GetRunDB() interface{} {
	return own.db
}
