package oltp

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/zeromicro/go-zero/core/logx"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func init() {
	// 确保全局管理器已初始化
	if connManager == nil {
		connManager = NewConnectionManager()
	}
}

// MySQL 配置
type Config struct {
	Host         string
	Port         int
	Username     string
	Password     string
	Database     string
	Charset      string
	ParseTime    bool
	Loc          string
	MaxIdleConns int
	MaxOpenConns int
	MaxLifetime  time.Duration
	IsLog        bool
}

// 默认配置
var DefaultConfig = &Config{
	Host:         "localhost",
	Port:         3306,
	Username:     "root",
	Password:     "",
	Database:     "test",
	Charset:      "utf8mb4",
	ParseTime:    true,
	Loc:          "Local",
	MaxIdleConns: 5,
	MaxOpenConns: 10,
	MaxLifetime:  30 * time.Minute,
	IsLog:        false,
}

// MySQL 连接管理
type MySQL struct {
	Name         string
	UpdateTime   int32
	db           *gorm.DB
	tx           *gorm.DB
	isTansaction bool
	tables       map[string]*TableMaster
	IsLog        bool
	config       *Config
}

func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{}
}

// NewMySQL 创建 MySQL 实例
func NewMySQL(config *Config) *MySQL {
	if config == nil {
		config = DefaultConfig
	}

	return &MySQL{
		tables: make(map[string]*TableMaster),
		IsLog:  config.IsLog,
		config: config,
	}
}
func (m *MySQL) GetConfig() *Config {
	return m.config
}

// ==================== 核心方法（与 SQLite 保持一致）====================

func (m *MySQL) ensureValidConnection() error {
	if m.db == nil {
		_, err := m.GetDB()
		return err
	}

	// 🔧 检查连接是否有效
	sqlDB, err := m.db.DB()
	if err != nil {
		logx.Errorf("获取底层数据库连接失败: %v", err)
		return m.recreateConnection()
	}

	// 🔧 测试连接
	if err := sqlDB.Ping(); err != nil {
		logx.Errorf("数据库连接ping失败: %v", err)
		return m.recreateConnection()
	}

	return nil
}

// 🔧 重建连接的方法
func (m *MySQL) recreateConnection() error {
	// 清理当前连接
	m.cleanupCurrentConnection()

	// 重新获取连接
	newDB, err := m.GetDB()
	if err != nil {
		return fmt.Errorf("重建数据库连接失败: %v", err)
	}

	m.db = newDB
	logx.Infof("数据库连接已重建: %s", m.Name)
	return nil
}

// 🔧 清理当前连接
func (m *MySQL) cleanupCurrentConnection() {
	if m.db != nil {
		if sqlDB, err := m.db.DB(); err == nil {
			sqlDB.Close()
		}
		m.db = nil
	}

	// 从连接池中移除
	connKey := m.getConnectionKey()
	connManager.SetConnection(connKey, nil)
}

// 延迟表检查方法
func (m *MySQL) ensureTable(data interface{}) error {
	return m.HasTable(data)
}

func (m *MySQL) GetDBName(data interface{}) error {
	// 1️⃣ 优先使用 config 中配置的数据库名
	if m.config.Database != "" {
		m.Name = m.config.Database
		return nil
	}

	// 2️⃣ 如果 m.Name 已设置，直接使用
	if m.Name != "" {
		return nil
	}

	// 3️⃣ 从模型获取数据库名
	if idb, ok := data.(types.IDBName); ok {
		// 优先使用 GetRemoteDBName（MySQL 场景）
		dbName := idb.GetRemoteDBName()
		if dbName == "" {
			// 如果 GetRemoteDBName 为空，尝试 GetLocalDBName
			dbName = idb.GetLocalDBName()
		}

		if dbName == "" {
			return errors.New("db name is empty")
		}

		m.Name = dbName
		return nil
	}

	return errors.New("db name is empty: config.Database, m.Name and model.GetRemoteDBName() are all empty")
}
func (m *MySQL) GetModelDB(model interface{}) (interface{}, error) {
	err := m.init(model)
	return m.db, err
}

// GetDB 获取或创建数据库连接
func (m *MySQL) GetDB() (*gorm.DB, error) {
	// 确保数据库名已设置（但允许为空，用于管理操作）
	connKey := m.getConnectionKey()

	// 尝试从连接池获取
	if db, ok := connManager.GetConnection(connKey); ok {
		if db != nil {
			// 检查连接健康状态
			if sqlDB, err := db.DB(); err == nil {
				if err := sqlDB.Ping(); err == nil {
					m.db = db
					return db, nil
				} else {
					// 连接不健康，关闭并清理
					sqlDB.Close()
					connManager.SetConnection(connKey, nil)
				}
			}
		}
	}

	// 创建新连接
	db, err := m.newDB()
	if err != nil {
		return nil, err
	}

	// 缓存连接
	m.db = db
	connManager.SetConnection(connKey, db)
	return db, nil
}

// 🔧 修复 init 方法 - 确保调用顺序正确
func (m *MySQL) init(data interface{}) error {
	err := m.GetDBName(data)
	if err != nil {
		return err
	}

	// 🔧 确保有效连接（此时 m.Name 已设置）
	if err := m.ensureValidConnection(); err != nil {
		return err
	}

	if m.isTansaction {
		if m.tx == nil {
			m.tx = m.db.Begin()
		}
	}

	return nil
}

// newDB 创建新的数据库连接（完全对标 SQLite 配置）
func (m *MySQL) newDB() (*gorm.DB, error) {
	var dsn string
	var db *gorm.DB
	var err error

	// 🔧 根据数据库名情况选择连接策略
	if m.Name != "" {
		// 有数据库名：先检查数据库是否存在
		tempDB, err := m.createTempConnection()
		if err != nil {
			return nil, fmt.Errorf("创建临时连接失败: %v", err)
		}

		dbExists := m.checkDatabaseExists(tempDB, m.Name)
		m.closeTempConnection(tempDB)

		if !dbExists {
			// 🔧 数据库不存在，先连接到 MySQL 服务器创建数据库
			dsn = m.buildDSN()
			db, err = gorm.Open(mysql.Open(dsn), m.getGormConfig())
			if err != nil {
				return nil, fmt.Errorf("创建数据库连接失败: %v", err)
			}

			// 创建数据库
			if err := m.ensureDatabase(db); err != nil {
				if sqlDB, e := db.DB(); e == nil {
					sqlDB.Close()
				}
				return nil, err
			}

			// 🔧 关键修复：创建数据库后，关闭连接，重新使用带数据库名的 DSN 连接
			if sqlDB, e := db.DB(); e == nil {
				sqlDB.Close()
			}
		}

		// 🔧 使用带数据库名的 DSN 连接（无论数据库是否已存在）
		dsn = m.buildDSNWithDB(m.Name)
		db, err = gorm.Open(mysql.Open(dsn), m.getGormConfig())
		if err != nil {
			return nil, fmt.Errorf("连接数据库失败: %v", err)
		}
	} else {
		// 无数据库名，连接到 MySQL 服务器（用于管理操作）
		dsn = m.buildDSN()
		db, err = gorm.Open(mysql.Open(dsn), m.getGormConfig())
		if err != nil {
			return nil, fmt.Errorf("创建 MySQL 连接失败: %v", err)
		}
	}

	// 配置连接池
	if err := m.configureConnectionPool(db); err != nil {
		if sqlDB, e := db.DB(); e == nil {
			sqlDB.Close()
		}
		return nil, err
	}

	return db, nil
}

// ==================== DSN 构建 ====================

// buildDSN 构建不带数据库名的 DSN（用于管理操作或创建数据库）
func (m *MySQL) buildDSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/?charset=%s&parseTime=true&loc=%s",
		m.config.Username,
		m.config.Password,
		m.config.Host,
		m.config.Port,
		m.config.Charset,
		m.config.Loc,
	)
}

// buildDSNWithDB 构建带数据库名的 DSN（直接连接到指定数据库）
func (m *MySQL) buildDSNWithDB(dbName string) string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=true&loc=%s",
		m.config.Username,
		m.config.Password,
		m.config.Host,
		m.config.Port,
		dbName,
		m.config.Charset,
		m.config.Loc,
	)
}

// ==================== 辅助方法 ====================

// createTempConnection 创建临时连接（用于检查数据库是否存在）
func (m *MySQL) createTempConnection() (*gorm.DB, error) {
	return gorm.Open(mysql.Open(m.buildDSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
}

// closeTempConnection 关闭临时连接
func (m *MySQL) closeTempConnection(db *gorm.DB) {
	if db != nil {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}
}

// checkDatabaseExists 检查数据库是否存在
func (m *MySQL) checkDatabaseExists(db *gorm.DB, dbName string) bool {
	var count int64
	err := db.Raw("SELECT COUNT(*) FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME = ?", dbName).Scan(&count).Error
	return err == nil && count > 0
}

// getGormConfig 获取 GORM 配置
func (m *MySQL) getGormConfig() *gorm.Config {
	return &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
			//NoLowerCase:   true,
		},
		PrepareStmt:              false,
		DisableAutomaticPing:     false,
		DisableNestedTransaction: true,
		SkipDefaultTransaction:   true,
		Logger:                   m.getLogger(),
	}
}

// configureConnectionPool 配置连接池
func (m *MySQL) configureConnectionPool(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("获取底层数据库连接失败: %v", err)
	}

	sqlDB.SetMaxIdleConns(m.config.MaxIdleConns)
	sqlDB.SetMaxOpenConns(m.config.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(m.config.MaxLifetime)
	sqlDB.SetConnMaxIdleTime(10 * time.Minute)

	return nil
}

// getConnectionKey 获取连接键
func (m *MySQL) getConnectionKey() string {
	// 使用 Name 而不是 config.Database，因为 Name 是最终确定的数据库名
	return fmt.Sprintf("%s:%d/%s", m.config.Host, m.config.Port, m.Name)
}

// ensureDatabase 确保数据库存在
func (m *MySQL) ensureDatabase(db *gorm.DB) error {
	// 🔧 验证数据库名不为空
	if m.Name == "" {
		return errors.New("database name is empty, cannot create database")
	}

	// 检查数据库是否存在
	var count int64
	err := db.Raw("SELECT COUNT(*) FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME = ?", m.Name).Scan(&count).Error
	if err != nil {
		return fmt.Errorf("检查数据库失败: %v", err)
	}

	// 数据库不存在，创建它
	if count == 0 {
		createSQL := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET %s COLLATE %s_general_ci",
			m.Name, m.config.Charset, m.config.Charset)

		if err := db.Exec(createSQL).Error; err != nil {
			return fmt.Errorf("创建数据库失败: %v", err)
		}
		logx.Infof("✅ 创建数据库成功: %s", m.Name)
	}

	// 切换到目标数据库
	if err := db.Exec(fmt.Sprintf("USE `%s`", m.Name)).Error; err != nil {
		return fmt.Errorf("切换数据库失败: %v", err)
	}

	return nil
}

// getLogger 获取日志配置
func (m *MySQL) getLogger() logger.Interface {
	if m.IsLog {
		return logger.Default.LogMode(logger.Info)
	}
	return logger.Default.LogMode(logger.Error)
}

// HasTable 检查并创建表（与 SQLite 逻辑完全一致）
func (m *MySQL) HasTable(model interface{}) error {
	// 🔧 先获取数据库名
	if err := m.GetDBName(model); err != nil {
		return err
	}

	if m.db == nil {
		db, err := m.GetDB()
		if err != nil {
			return err
		}
		m.db = db
	}

	if _, ok := model.(types.IDBSQL); ok {
		return nil
	}

	// ...existing code... (后续逻辑保持不变)
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

	tableName := m.db.NamingStrategy.TableName(finalType.Name())
	cacheKey := TableCacheKey{
		DBPath:    m.Name,
		TableName: tableName,
	}

	if _, exists := tableCache.Load(cacheKey); exists {
		return nil
	}

	migrationLock.Lock()
	defer migrationLock.Unlock()

	if _, exists := tableCache.Load(cacheKey); exists {
		return nil
	}

	var count int64
	err := m.db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?",
		m.Name, tableName).Scan(&count).Error
	if err == nil && count > 0 {
		tableCache.Store(cacheKey, true)
		return nil
	}

	modelForMigration := reflect.New(finalType).Interface()

	err = m.db.AutoMigrate(modelForMigration)
	if err != nil {
		logx.Errorf("创建表失败: %s, 错误: %v, 输入类型: %v, 迁移类型: %v",
			tableName, err, modelType, reflect.TypeOf(modelForMigration))
		return err
	}

	tableCache.Store(cacheKey, true)

	return m.processNestedTablesOptimized(modelForMigration, make(map[string]bool), 0, 2)
}

// processNestedTablesOptimized 优化嵌套表处理（与 SQLite 完全一致）
func (m *MySQL) processNestedTablesOptimized(model interface{}, processed map[string]bool, depth, maxDepth int) error {
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

			// 🔧 关键修复：先检查嵌套表是否已存在
			nestedTableName := m.db.NamingStrategy.TableName(name1)
			var tableExists int64
			err := m.db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?",
				m.Name, nestedTableName).Scan(&tableExists).Error

			if err != nil {
				logx.Errorf("检查嵌套表失败: %s, 错误: %v", nestedTableName, err)
				return
			}

			// 🔧 只有在表不存在时才创建
			if tableExists == 0 {
				obj := reflect.New(t).Interface()

				// 🔧 使用 Migrator.CreateTable 而不是 AutoMigrate
				// CreateTable 只创建表结构，不会尝试同步外键约束
				migrator := m.db.Migrator()
				if err := migrator.CreateTable(obj); err != nil {
					logx.Errorf("创建嵌套表失败: %s -> %s, 错误: %v", pname, name1, err)
					return
				}
				logx.Infof("✅ 创建嵌套表成功: %s", nestedTableName)

				// 递归处理更深层的嵌套（只在新创建的表上）
				m.processNestedTablesOptimized(obj, processed, depth+1, maxDepth)
			} else {
				// 表已存在，跳过迁移和递归
				logx.Infof("嵌套表已存在，跳过迁移: %s", nestedTableName)
			}
		}
	})

	return nil
}

// ==================== 数据操作方法（与 SQLite 完全一致）====================

func (m *MySQL) Load(item *types.SearchItem, result interface{}) error {
	err := m.init(item.Model)
	if err != nil {
		return err
	}
	err = m.ensureTable(item.Model)
	if err != nil {
		return err
	}
	if item.IsStatistical {
		return sum(m.db, item, result)
	}
	if m.isTansaction {
		return load(m.tx, item, result)
	}
	return load(m.db, item, result)
}

func (m *MySQL) Raw(sql string, data interface{}) error {
	obj := utils.NewArrayItem(data)
	err := m.init(obj)
	if err != nil {
		return err
	}
	m.db.Raw(sql).Scan(data)
	return m.db.Error
}

func (m *MySQL) Exec(sql string, data interface{}) error {
	err := m.init(data)
	if err != nil {
		return err
	}
	m.db.Exec(sql, data)
	return m.db.Error
}

func (m *MySQL) Transaction() error {
	// 🔧 确保数据库连接已建立
	if m.db == nil {
		return errors.New("database connection not established, call GetDBName() and GetDB() first")
	}

	m.isTansaction = true
	return nil
}

// errorHandler 错误处理（MySQL 版本）
func (m *MySQL) errorHandler(err error, data interface{}, fn func(db *gorm.DB, data interface{}) error) error {
	if err == nil {
		return nil
	}

	// MySQL 特定的错误检查
	errStr := err.Error()
	if strings.Contains(errStr, "Unknown column") ||
		strings.Contains(errStr, "doesn't exist") ||
		strings.Contains(errStr, "Table") && strings.Contains(errStr, "doesn't exist") ||
		strings.Contains(errStr, "Column") && strings.Contains(errStr, "cannot be null") {

		err := m.db.AutoMigrate(data)
		if err == nil {
			return fn(m.db, data)
		}
	}
	return err
}

// ==================== 插入方法优化 ====================

// Insert 插入数据（延迟表检查优化）
func (m *MySQL) Insert(data interface{}) error {
	err := m.init(data)
	if err != nil {
		return err
	}

	// 🔧 优化：先尝试插入，失败时再检查表
	if rowcode, ok := data.(types.IRowCode); ok {
		rowcode.SetHashcode(rowcode.GetHash())
	}

	var insertErr error
	if m.isTansaction {
		insertErr = createData(m.tx, data)
	} else {
		insertErr = createData(m.db, data)
	}

	// 🔧 只有在插入失败时才检查表
	if insertErr != nil {
		// 检查是否是"表不存在"错误
		if m.isTableNotExistsError(insertErr) {
			// 创建表
			if err := m.ensureTable(data); err != nil {
				return err
			}

			// 重试插入
			if m.isTansaction {
				return createData(m.tx, data)
			}
			return createData(m.db, data)
		}

		// 其他类型的错误，尝试自动修复
		return m.errorHandler(insertErr, data, createData)
	}

	return nil
}

// isTableNotExistsError 判断是否是"表不存在"错误
func (m *MySQL) isTableNotExistsError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	return strings.Contains(errStr, "Table") && strings.Contains(errStr, "doesn't exist") ||
		strings.Contains(errStr, "Error 1146") // MySQL 错误码：表不存在
}

// Update 更新数据（同样优化）
func (m *MySQL) Update(data interface{}) error {
	err := m.init(data)
	if err != nil {
		return err
	}

	if rowcode, ok := data.(types.IRowCode); ok {
		rowcode.SetHashcode(rowcode.GetHash())
	}

	var updateErr error
	if m.isTansaction {
		updateErr = updateData(m.tx, data)
	} else {
		updateErr = updateData(m.db, data)
	}

	if updateErr != nil {
		if m.isTableNotExistsError(updateErr) {
			if err := m.ensureTable(data); err != nil {
				return err
			}

			if m.isTansaction {
				return updateData(m.tx, data)
			}
			return updateData(m.db, data)
		}
		return m.errorHandler(updateErr, data, updateData)
	}

	return nil
}

// Delete 删除数据（同样优化）
func (m *MySQL) Delete(data interface{}) error {
	err := m.init(data)
	if err != nil {
		return err
	}

	var deleteErr error
	if m.isTansaction {
		deleteErr = deleteData(m.tx, data)
	} else {
		deleteErr = deleteData(m.db, data)
	}

	if deleteErr != nil {
		if m.isTableNotExistsError(deleteErr) {
			// 删除操作遇到表不存在，直接返回成功（表都不存在了）
			return nil
		}
		return m.errorHandler(deleteErr, data, deleteData)
	}

	return nil
}

func (m *MySQL) Commit() error {
	m.isTansaction = false
	if m.tx != nil {
		err := m.tx.Commit().Error
		m.tx = nil
		return err
	}
	return nil
}

func (m *MySQL) GetRunDB() interface{} {
	return m.db
}

func (m *MySQL) Rollback() error {
	if m.tx != nil {
		err := m.tx.Rollback().Error
		m.tx = nil
		m.isTansaction = false
		return err
	}
	return nil
}

// ==================== 数据库管理方法 ====================

// DeleteDB 删除数据库
func (m *MySQL) DeleteDB() error {
	// 关闭所有连接
	if err := m.closeAllConnections(); err != nil {
		logx.Errorf("关闭数据库连接失败: %v", err)
	}

	// 清除连接缓存
	connKey := m.getConnectionKey()
	connManager.SetConnection(connKey, nil)

	// 重置当前实例的连接
	m.db = nil
	m.tx = nil
	m.isTansaction = false

	// 创建临时连接用于删除数据库
	tempDB, err := gorm.Open(mysql.Open(m.buildDSN()), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("创建临时连接失败: %v", err)
	}
	defer func() {
		if sqlDB, err := tempDB.DB(); err == nil {
			sqlDB.Close()
		}
	}()

	// 删除数据库
	dropSQL := fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", m.Name)
	if err := tempDB.Exec(dropSQL).Error; err != nil {
		return fmt.Errorf("删除数据库失败: %v", err)
	}

	// 清除表缓存
	m.clearTableCache()

	logx.Infof("✅ 成功删除数据库: %s", m.Name)
	return nil
}

// RecreateConnection 重建连接
func (m *MySQL) RecreateConnection() error {
	return m.recreateConnection()
}

// closeAllConnections 关闭所有数据库连接
func (m *MySQL) closeAllConnections() error {
	var lastError error

	// 关闭事务连接
	if m.tx != nil {
		if tx := m.tx.Rollback(); tx != nil {
			logx.Errorf("回滚事务失败: %v", tx.Error)
			lastError = tx.Error
		}
		m.tx = nil
		m.isTansaction = false
	}

	// 关闭主数据库连接
	if m.db != nil {
		if sqlDB, err := m.db.DB(); err == nil {
			if err := sqlDB.Close(); err != nil {
				logx.Errorf("关闭数据库连接失败: %v", err)
				lastError = err
			}
		}
		m.db = nil
	}

	return lastError
}

// clearTableCache 清除表缓存
func (m *MySQL) clearTableCache() {
	tableCache.Range(func(key, value interface{}) bool {
		if cacheKey, ok := key.(TableCacheKey); ok {
			if cacheKey.DBPath == m.Name {
				tableCache.Delete(key)
			}
		}
		return true
	})
}
