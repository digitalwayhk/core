package oltp

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
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

// ============================================================
// 全局变量
// ============================================================

var (
	// AutoMigrate 重试锁（按表名锁定）
	migrateLocks sync.Map
)

// ============================================================
// Sqlite 结构体
// ============================================================

type Sqlite struct {
	Name         string
	Size         float64
	UpdateTime   int32
	Path         string
	db           *gorm.DB
	tx           *gorm.DB
	isTansaction bool
	tables       map[string]*TableMaster
	IsLog        bool
	writeLock    sync.Mutex // 🆕 全局写锁
}

func NewSqlite() *Sqlite {
	return &Sqlite{
		tables: make(map[string]*TableMaster),
	}
}

// ============================================================
// 核心方法：连接管理
// ============================================================

// init 初始化数据库连接
func (own *Sqlite) init(data interface{}) error {
	err := own.GetDBName(data)
	if err != nil {
		return err
	}

	dns, err := own.getPath()
	if err != nil {
		return err
	}

	// ✅ 如果数据库文件不存在，清除连接缓存
	if !utils.IsFile(dns) {
		connManager.SetConnection(dns, nil)
		own.db = nil
		own.tx = nil
	}

	// ✅ 确保连接有效
	if err := own.ensureValidConnection(); err != nil {
		return err
	}

	// ✅ 初始化事务
	if own.isTansaction {
		if own.tx == nil {
			own.tx = own.db.Begin()
		}
	}

	return nil
}

// ensureValidConnection 确保连接有效
func (own *Sqlite) ensureValidConnection() error {
	if own.db == nil {
		_, err := own.GetDB()
		return err
	}

	// ✅ 检查连接健康状态
	sqlDB, err := own.db.DB()
	if err != nil {
		logx.Errorf("获取底层数据库连接失败: %v", err)
		return own.recreateConnection()
	}

	// ✅ Ping 测试
	if err := sqlDB.Ping(); err != nil {
		logx.Errorf("数据库连接 ping 失败: %v", err)
		return own.recreateConnection()
	}

	return nil
}

// recreateConnection 重建连接
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

// cleanupCurrentConnection 清理当前连接
func (own *Sqlite) cleanupCurrentConnection() {
	if own.db != nil {
		if sqlDB, err := own.db.DB(); err == nil {
			sqlDB.Close()
		}
		own.db = nil
	}

	dns, _ := own.getPath()
	connManager.SetConnection(dns, nil)
}

// GetDB 获取数据库连接
func (own *Sqlite) GetDB() (*gorm.DB, error) {
	dns, err := own.getPath()
	if err != nil {
		return nil, err
	}

	// ✅ 检查文件是否存在
	if !utils.IsFile(dns) {
		if db, ok := connManager.GetConnection(dns); ok && db != nil {
			if sqlDB, err := db.DB(); err == nil {
				sqlDB.Close()
			}
		}
		connManager.SetConnection(dns, nil)
		own.db = nil
	}

	// ✅ 从连接池获取
	if db, ok := connManager.GetConnection(dns); ok && db != nil {
		// ✅ 检查连接健康状态
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

	// ✅ 创建新连接
	own.db, err = own.newDB()
	if err != nil {
		return nil, err
	}

	if !config.INITSERVER {
		connManager.SetConnection(dns, own.db)
	}
	return own.db, nil
}

// newDB 创建新的数据库连接
func (own *Sqlite) newDB() (*gorm.DB, error) {
	dia := sqlite.Open(own.Path)
	db, err := gorm.Open(dia, &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
			NoLowerCase:   true,
		},
		PrepareStmt:              false,
		DisableAutomaticPing:     false,
		DisableNestedTransaction: true,
		SkipDefaultTransaction:   true,
		Logger:                   logger.Default.LogMode(logger.Error),
	})

	if err != nil {
		return nil, err
	}

	// ✅ 配置连接池（🔧 修复：只允许 1 个写连接）
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetMaxOpenConns(2) // 🔧 修改：从 3 -> 2
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	sqlDB.SetConnMaxIdleTime(2 * time.Minute)

	// ✅ SQLite 优化（🔧 修复：增加 busy_timeout）
	db.Exec("PRAGMA journal_mode=WAL;")
	db.Exec("PRAGMA busy_timeout=30000;") // 🔧 5秒
	db.Exec("PRAGMA synchronous=NORMAL;")
	db.Exec("PRAGMA cache_size=2000;")
	db.Exec("PRAGMA temp_store=MEMORY;")     // 🆕 临时表存储在内存
	db.Exec("PRAGMA mmap_size=30000000000;") // 🆕 启用内存映射（30GB）

	// 🆕 验证 WAL 模式是否生效
	var journalMode string
	db.Raw("PRAGMA journal_mode;").Scan(&journalMode)
	if journalMode != "wal" {
		logx.Errorf("⚠️ WAL 模式未生效，当前模式: %s", journalMode)
	} else {
		logx.Infof("✅ WAL 模式已启用")
	}

	return db, nil
}

// ============================================================
// 核心方法：表管理（方案 C：延迟创建）
// ============================================================

// HasTable 检查表是否存在（只创建主表，不递归处理嵌套表）
func (own *Sqlite) HasTable(model interface{}) error {
	if config.INITSERVER || (own.db != nil && own.db.DryRun) {
		return nil
	}

	// ✅ 确保连接有效
	if err := own.ensureValidConnection(); err != nil {
		return err
	}

	if _, ok := model.(types.IDBSQL); ok {
		return nil
	}

	// ✅ 处理指针层级
	modelType := reflect.TypeOf(model)
	if modelType == nil {
		return fmt.Errorf("model 不能为 nil")
	}

	pointerDepth := 0
	finalType := modelType
	for finalType.Kind() == reflect.Ptr {
		finalType = finalType.Elem()
		pointerDepth++
	}

	if finalType.Kind() != reflect.Struct {
		return fmt.Errorf("model 必须是结构体或结构体指针，当前类型: %v", modelType)
	}

	if pointerDepth > 1 {
		logx.Errorf("HasTable 检测到 %d 层指针: %v -> %v", pointerDepth, modelType, finalType)
	}

	// ✅ 获取表名
	tableName := own.db.NamingStrategy.TableName(finalType.Name())
	cacheKey := TableCacheKey{
		DBPath:    own.Path,
		TableName: tableName,
	}

	// ✅ 检查缓存
	if _, exists := tableCache.Load(cacheKey); exists {
		return nil
	}

	// ✅ 使用锁防止并发迁移（按表名锁定）
	lockKey := own.Path + ":" + tableName
	lock, _ := migrateLocks.LoadOrStore(lockKey, &sync.Mutex{})
	tableLock := lock.(*sync.Mutex)

	tableLock.Lock()
	defer tableLock.Unlock()

	// ✅ 双重检查
	if _, exists := tableCache.Load(cacheKey); exists {
		return nil
	}

	// ✅ 快速检查表是否存在
	var count int64
	err := own.db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", tableName).Scan(&count).Error
	if err == nil && count > 0 {
		tableCache.Store(cacheKey, true)
		return nil
	}

	// ✅ 创建表（使用安全的 AutoMigrate）
	modelForMigration := reflect.New(finalType).Interface()
	err = own.safeAutoMigrate(modelForMigration)
	if err != nil {
		logx.Errorf("创建表失败: %s, 错误: %v, 输入类型: %v", tableName, err, modelType)
		return err
	}

	// ✅ 缓存结果
	tableCache.Store(cacheKey, true)

	// ✅ 方案 C 关键：不再递归处理嵌套表
	// 嵌套表会在首次访问时自动创建（通过 ensureTable）
	return nil
}

// safeAutoMigrate 安全的 AutoMigrate，带重试机制（方案 A）
func (own *Sqlite) safeAutoMigrate(model interface{}) error {
	const maxRetries = 3
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		// ✅ 检查连接状态
		if err := own.ensureValidConnection(); err != nil {
			lastErr = err
			time.Sleep(time.Millisecond * 100 * time.Duration(i+1))
			continue
		}

		// ✅ 执行迁移
		err := own.db.AutoMigrate(model)
		if err == nil {
			return nil
		}

		// ✅ 检查是否是连接关闭错误
		if strings.Contains(err.Error(), "database is closed") ||
			strings.Contains(err.Error(), "bad connection") {
			logx.Errorf("AutoMigrate 连接错误 (尝试 %d/%d): %v", i+1, maxRetries, err)
			lastErr = err

			// ✅ 强制重建连接
			if recreateErr := own.recreateConnection(); recreateErr != nil {
				logx.Errorf("重建连接失败: %v", recreateErr)
			}

			time.Sleep(time.Millisecond * 100 * time.Duration(i+1))
			continue
		}

		// 其他错误，直接返回
		return err
	}

	return fmt.Errorf("AutoMigrate 失败，已重试 %d 次: %w", maxRetries, lastErr)
}

// ensureTable 确保表存在（延迟创建入口）
func (own *Sqlite) ensureTable(data interface{}) error {
	return own.HasTable(data)
}

// ============================================================
// CRUD 方法（添加 ensureTable 检查）
// ============================================================

// Load 查询数据（✅ 关键：Load 时也要确保表存在）
func (own *Sqlite) Load(item *types.SearchItem, result interface{}) error {
	err := own.init(item.Model)
	if err != nil {
		return err
	}

	// ✅ 关键修复：Load 时确保表存在
	// 场景：可能先查询再插入，此时表还不存在
	err = own.ensureTable(item.Model)
	if err != nil {
		return err
	}

	// ✅ 再次检查连接（防止在 ensureTable 中连接被关闭）
	if err := own.ensureValidConnection(); err != nil {
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

// Insert 插入数据
func (own *Sqlite) Insert(data interface{}) error {
	err := own.init(data)
	if err != nil {
		return err
	}

	// ✅ 确保表存在
	err = own.ensureTable(data)
	if err != nil {
		return err
	}

	// ✅ 再次检查连接
	if err := own.ensureValidConnection(); err != nil {
		return err
	}

	if own.isTansaction {
		err := createData(own.tx, data)
		if err != nil {
			return err
		}
		return nil
	}
	// 🆕 非事务操作加写锁
	own.writeLock.Lock()
	defer own.writeLock.Unlock()
	err = createData(own.db, data)
	if err != nil {
		err = own.errorHandler(err, data, createData)
	}
	return err
}

// Update 更新数据
func (own *Sqlite) Update(data interface{}) error {
	err := own.init(data)
	if err != nil {
		return err
	}

	// ✅ 确保表存在
	err = own.ensureTable(data)
	if err != nil {
		return err
	}

	// ✅ 再次检查连接
	if err := own.ensureValidConnection(); err != nil {
		return err
	}

	if own.isTansaction {
		err := updateData(own.tx, data)
		if err != nil {
			return err
		}
		return nil
	}
	// 🆕 非事务操作加写锁
	own.writeLock.Lock()
	defer own.writeLock.Unlock()
	err = updateData(own.db, data)
	if err != nil {
		err = own.errorHandler(err, data, updateData)
	}
	return err
}

// Delete 删除数据
func (own *Sqlite) Delete(data interface{}) error {
	err := own.init(data)
	if err != nil {
		return err
	}

	// ✅ 确保表存在
	err = own.ensureTable(data)
	if err != nil {
		return err
	}

	// ✅ 再次检查连接
	if err := own.ensureValidConnection(); err != nil {
		return err
	}

	if own.isTansaction {
		err := deleteData(own.tx, data)
		if err != nil {
			return err
		}
		return nil
	}
	// 🆕 非事务操作加写锁
	own.writeLock.Lock()
	defer own.writeLock.Unlock()
	err = deleteData(own.db, data)
	if err != nil {
		err = own.errorHandler(err, data, deleteData)
	}
	return err
}

// errorHandler 错误处理（自动迁移）
func (own *Sqlite) errorHandler(err error, data interface{}, fn func(db *gorm.DB, data interface{}) error) error {
	if err == nil {
		return nil
	}

	// ✅ 检查是否是列不存在的错误
	if strings.Contains(err.Error(), "no such column") ||
		strings.Contains(err.Error(), "has no column named") ||
		strings.Contains(err.Error(), "ambiguous column name") ||
		strings.Contains(err.Error(), "no such table") ||
		strings.Contains(err.Error(), "datatype mismatch") {

		// ✅ 使用安全的 AutoMigrate
		err := own.safeAutoMigrate(data)
		if err == nil {
			// ✅ 迁移成功后检查连接
			if connErr := own.ensureValidConnection(); connErr != nil {
				return connErr
			}
			return fn(own.db, data)
		}
	}

	return err
}

// ============================================================
// 其他方法
// ============================================================

// Raw 执行原生 SQL
func (own *Sqlite) Raw(sql string, data interface{}) error {
	obj := utils.NewArrayItem(data)
	err := own.init(obj)
	if err != nil {
		return err
	}

	// ✅ 确保连接有效
	if err := own.ensureValidConnection(); err != nil {
		return err
	}

	own.db.Raw(sql).Scan(data)
	return own.db.Error
}

// Exec 执行 SQL
func (own *Sqlite) Exec(sql string, data interface{}) error {
	err := own.init(data)
	if err != nil {
		return err
	}

	// ✅ 确保连接有效
	if err := own.ensureValidConnection(); err != nil {
		return err
	}

	own.db.Exec(sql, data)
	return own.db.Error
}

// Transaction 开启事务
func (own *Sqlite) Transaction() error {
	own.isTansaction = true
	return nil
}

// Commit 提交事务
func (own *Sqlite) Commit() error {
	own.isTansaction = false
	if own.tx != nil {
		err := own.tx.Commit().Error
		own.tx = nil
		return err
	}
	return nil
}

// Rollback 回滚事务
func (own *Sqlite) Rollback() error {
	if own.tx != nil {
		err := own.tx.Rollback().Error
		own.tx = nil
		own.isTansaction = false
		return err
	}
	return nil
}

// DeleteDB 删除数据库
func (own *Sqlite) DeleteDB() error {
	dns, err := own.getPath()
	if err != nil {
		return err
	}

	// ✅ 关闭所有连接
	if err := own.closeAllConnections(); err != nil {
		logx.Errorf("关闭数据库连接失败: %v", err)
	}

	// ✅ 清除连接缓存
	connManager.SetConnection(dns, nil)

	// ✅ 重置当前实例
	own.db = nil
	own.tx = nil
	own.isTansaction = false

	// ✅ 删除文件
	err = utils.DeleteFile(dns)
	if err != nil {
		logx.Errorf("删除数据库文件失败: %s, 错误: %v", dns, err)
		return err
	}

	// ✅ 清除表缓存
	own.clearTableCache()

	return nil
}

// closeAllConnections 关闭所有连接
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

	// 关闭主连接
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

// clearTableCache 清除表缓存
func (own *Sqlite) clearTableCache() {
	tableCache.Range(func(key, value interface{}) bool {
		if cacheKey, ok := key.(TableCacheKey); ok {
			if cacheKey.DBPath == own.Path {
				tableCache.Delete(key)
			}
		}
		return true
	})
}

// GetDBName 获取数据库名称
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

// getPath 获取数据库路径
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

// GetModelDB 获取模型的数据库连接
func (own *Sqlite) GetModelDB(model interface{}) (interface{}, error) {
	err := own.init(model)
	return own.db, err
}

// GetRunDB 获取运行中的数据库连接
func (own *Sqlite) GetRunDB() interface{} {
	return own.db
}

// RecreateConnection 重建连接（对外接口）
func (own *Sqlite) RecreateConnection() error {
	return own.recreateConnection()
}

// ============================================================
// 跨库事务支持
// ============================================================

// AttachDatabase 附加数据库
func (own *Sqlite) AttachDatabase(aliasName, dbPath string) error {
	if own.db == nil {
		if _, err := own.GetDB(); err != nil {
			return err
		}
	}

	sql := fmt.Sprintf("ATTACH DATABASE '%s' AS %s", dbPath, aliasName)
	return own.db.Exec(sql).Error
}

// DetachDatabase 分离数据库
func (own *Sqlite) DetachDatabase(aliasName string) error {
	if own.db == nil {
		return errors.New("database connection not initialized")
	}

	sql := fmt.Sprintf("DETACH DATABASE %s", aliasName)
	return own.db.Exec(sql).Error
}
