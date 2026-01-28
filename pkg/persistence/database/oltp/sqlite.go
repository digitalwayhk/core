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

	// 🔧 修复：使用新的连接检查方法
	if err := own.ensureValidConnection(); err != nil {
		return err
	}

	if own.isTansaction {
		if own.tx == nil {
			own.tx = own.db.Begin()
		}
	}

	return nil
}
func (own *Sqlite) ensureValidConnection() error {
	if own.db == nil {
		_, err := own.GetDB()
		return err
	}

	// 🔧 检查连接是否有效
	sqlDB, err := own.db.DB()
	if err != nil {
		logx.Errorf("获取底层数据库连接失败: %v", err)
		return own.recreateConnection()
	}

	// 🔧 测试连接
	if err := sqlDB.Ping(); err != nil {
		logx.Errorf("数据库连接ping失败: %v", err)
		return own.recreateConnection()
	}

	return nil
}

// 🔧 新增：重建连接的方法
func (own *Sqlite) recreateConnection() error {
	// 清理当前连接
	own.cleanupCurrentConnection()

	// 重新获取连接
	newDB, err := own.GetDB()
	if err != nil {
		return fmt.Errorf("重建数据库连接失败: %v", err)
	}

	own.db = newDB
	logx.Infof("数据库连接已重建: %s", own.Path)
	return nil
}

// 🔧 新增：清理当前连接
func (own *Sqlite) cleanupCurrentConnection() {
	if own.db != nil {
		if sqlDB, err := own.db.DB(); err == nil {
			sqlDB.Close()
		}
		own.db = nil
	}

	// 从连接池中移除
	dns, _ := own.getPath()
	connManager.SetConnection(dns, nil)
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

	//logx.Infof("✅ 成功删除数据库文件: %s", dns)
	return nil
}
func (own *Sqlite) RecreateConnection() error {
	return own.recreateConnection()
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

// sqlite.go - 修复连接管理
func (own *Sqlite) GetDB() (*gorm.DB, error) {
	dns, err := own.getPath()
	if err != nil {
		return nil, err
	}

	// 🔧 修复：检查文件是否存在
	if !utils.IsFile(dns) {
		// 先关闭现有连接再清除缓存
		if db, ok := connManager.GetConnection(dns); ok && db != nil {
			if sqlDB, err := db.DB(); err == nil {
				sqlDB.Close()
			}
		}
		connManager.SetConnection(dns, nil)
		own.db = nil
	}

	if db, ok := connManager.GetConnection(dns); ok {
		if db != nil {
			// 🔧 修复：检查连接健康状态
			if sqlDB, err := db.DB(); err == nil {
				if err := sqlDB.Ping(); err == nil {
					own.db = db
					return db, nil
				} else {
					// 连接不健康，关闭并清理
					sqlDB.Close()
					connManager.SetConnection(dns, nil)
				}
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

// 🔧 修复：改进newDB配置
func (own *Sqlite) newDB() (*gorm.DB, error) {
	dia := sqlite.Open(own.Path)
	db, err := gorm.Open(dia, &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
			//NoLowerCase:   true,
		},
		PrepareStmt:              false,
		DisableAutomaticPing:     false, // 🔧 启用ping检测
		DisableNestedTransaction: true,
		SkipDefaultTransaction:   true,
		Logger:                   logger.Default.LogMode(logger.Error),
	})

	if err != nil {
		return nil, err
	}

	// 🔧 修复：更严格的连接池配置
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxIdleConns(1)                  // 最小空闲连接
	sqlDB.SetMaxOpenConns(3)                  // 稍微增加但保持较小
	sqlDB.SetConnMaxLifetime(5 * time.Minute) // 缩短生存时间
	sqlDB.SetConnMaxIdleTime(2 * time.Minute) // 🔧 新增：空闲超时
	db.Exec("PRAGMA journal_mode=WAL;")
	db.Exec("PRAGMA busy_timeout=5000;")  // 5秒超时
	db.Exec("PRAGMA synchronous=NORMAL;") // 提升性能
	db.Exec("PRAGMA cache_size=2000;")    // 增加缓存
	return db, nil
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

	// 🔧 修复：先检查并处理指针层级
	modelType := reflect.TypeOf(model)
	if modelType == nil {
		return fmt.Errorf("model 不能为 nil")
	}

	// 🔧 统计指针层级并解引用到最终类型
	pointerDepth := 0
	finalType := modelType
	for finalType.Kind() == reflect.Ptr {
		finalType = finalType.Elem()
		pointerDepth++
	}

	// 🔧 验证最终类型必须是结构体
	if finalType.Kind() != reflect.Struct {
		return fmt.Errorf("model 必须是结构体或结构体指针，当前类型: %v", modelType)
	}

	// 🔧 如果是双指针或更多层，记录警告
	if pointerDepth > 1 {
		logx.Errorf("HasTable 检测到 %d 层指针: %v -> %v", pointerDepth, modelType, finalType)
	}

	// 获取表名（使用解引用后的类型名）
	tableName := own.db.NamingStrategy.TableName(finalType.Name())
	cacheKey := TableCacheKey{
		DBPath:    own.Path,
		TableName: tableName,
	}

	// 检查缓存
	if _, exists := tableCache.Load(cacheKey); exists {
		return nil
	}

	// 使用锁防止并发迁移
	migrationLock.Lock()
	defer migrationLock.Unlock()

	// 双重检查
	if _, exists := tableCache.Load(cacheKey); exists {
		return nil
	}

	// 快速检查表是否存在
	var count int64
	err := own.db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", tableName).Scan(&count).Error
	if err == nil && count > 0 {
		tableCache.Store(cacheKey, true)
		return nil
	}

	// 🔧 修复：创建标准的单层指针实例用于迁移
	// reflect.New(finalType) 返回 *finalType
	modelForMigration := reflect.New(finalType).Interface()

	// 只在表不存在时才执行迁移
	err = own.db.AutoMigrate(modelForMigration)
	if err != nil {
		logx.Errorf("创建表失败: %s, 错误: %v, 输入类型: %v, 迁移类型: %v",
			tableName, err, modelType, reflect.TypeOf(modelForMigration))
		return err
	}

	// 缓存结果
	tableCache.Store(cacheKey, true)

	// 处理嵌套表（使用规范化后的实例）
	return own.processNestedTablesOptimized(modelForMigration, make(map[string]bool), 0, 2)
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
			err := own.db.AutoMigrate(obj)
			if err != nil {
				logx.Errorf("处理嵌套表失败: %s -> %s, 错误: %v", pname, name1, err)
			}
			// 递归处理嵌套表
			own.processNestedTablesOptimized(obj, processed, depth+1, maxDepth)
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

func (own *Sqlite) Transaction() error {
	own.isTansaction = true
	return nil
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
			// 不在这里回滚，让调用者决定是否回滚
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
func (own *Sqlite) Commit() error {
	own.isTansaction = false
	if own.tx != nil {
		err := own.tx.Commit().Error
		own.tx = nil
		return err
	}
	return nil
}
func (own *Sqlite) GetRunDB() interface{} {
	return own.db
}
func (own *Sqlite) Rollback() error {
	if own.tx != nil {
		err := own.tx.Rollback().Error
		own.tx = nil
		own.isTansaction = false
		return err
	}
	return nil
}

// 在您的sqlite.go中添加跨库事务支持
func (own *Sqlite) AttachDatabase(aliasName, dbPath string) error {
	if own.db == nil {
		if _, err := own.GetDB(); err != nil {
			return err
		}
	}

	sql := fmt.Sprintf("ATTACH DATABASE '%s' AS %s", dbPath, aliasName)
	return own.db.Exec(sql).Error
}

func (own *Sqlite) DetachDatabase(aliasName string) error {
	if own.db == nil {
		return errors.New("database connection not initialized")
	}

	sql := fmt.Sprintf("DETACH DATABASE %s", aliasName)
	return own.db.Exec(sql).Error
}

// 在您的sqlite.go基础上进行WAL模式优化
func (own *Sqlite) newDBWithWAL() (*gorm.DB, error) {
	dia := sqlite.Open(own.Path + "?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=1000&_temp_store=memory")
	db, err := gorm.Open(dia, &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
			NoLowerCase:   true,
		},
		PrepareStmt:              true, // 启用预编译语句
		DisableAutomaticPing:     false,
		DisableNestedTransaction: false, // 支持嵌套事务
		SkipDefaultTransaction:   false, // 保持事务安全
		Logger:                   logger.Default.LogMode(logger.Silent),
	})

	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	// 优化的连接池配置
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	sqlDB.SetConnMaxIdleTime(10 * time.Minute)

	// WAL模式配置
	sqlDB.Exec("PRAGMA journal_mode=WAL;")
	sqlDB.Exec("PRAGMA synchronous=NORMAL;")
	sqlDB.Exec("PRAGMA cache_size=1000;")
	sqlDB.Exec("PRAGMA temp_store=memory;")
	sqlDB.Exec("PRAGMA mmap_size=268435456;") // 256MB mmap

	return db, nil
}
