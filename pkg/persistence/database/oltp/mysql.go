package oltp

import (
	"errors"
	"fmt"
	"os"
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

func (c *Config) MysqlDSN() string {
	if c.Charset == "" {
		c.Charset = "utf8mb4"
	}
	if c.ParseTime == false {
		c.ParseTime = true
	}
	if c.Loc == "" {
		c.Loc = "Local"
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=%t&loc=%s",
		c.Username, c.Password, c.Host, c.Port, c.Database, c.Charset, c.ParseTime, c.Loc)
}
func (c *Config) SetMysqlDSN(dsn string) error {
	// 例: user:pass@tcp(host:3306)/dbname?charset=utf8mb4&parseTime=True&loc=Local
	upAndRest := strings.SplitN(dsn, "@tcp(", 2)
	if len(upAndRest) != 2 {
		return errors.New("invalid DSN format")
	}
	userPass := upAndRest[0]
	rest := upAndRest[1]

	// 用户名和密码
	userParts := strings.SplitN(userPass, ":", 2)
	if len(userParts) != 2 {
		return errors.New("invalid DSN user:pass format")
	}
	c.Username = userParts[0]
	c.Password = userParts[1]

	// host:port)/dbname?params
	hostAndDb := strings.SplitN(rest, ")/", 2)
	if len(hostAndDb) != 2 {
		return errors.New("invalid DSN format")
	}

	hostPort := strings.TrimSuffix(hostAndDb[0], ")")
	hostPortParts := strings.SplitN(hostPort, ":", 2)
	if len(hostPortParts) != 2 {
		return errors.New("invalid DSN host:port format")
	}
	c.Host = hostPortParts[0]
	fmt.Sscanf(hostPortParts[1], "%d", &c.Port)

	// dbname?params
	dbAndParams := strings.SplitN(hostAndDb[1], "?", 2)
	c.Database = dbAndParams[0]
	// if c.Database == "" {
	// 	return errors.New("invalid DSN: database name is empty")
	// }
	// 解析参数
	if len(dbAndParams) == 2 {
		params := dbAndParams[1]
		for _, kv := range strings.Split(params, "&") {
			kvParts := strings.SplitN(kv, "=", 2)
			if len(kvParts) != 2 {
				continue
			}
			switch kvParts[0] {
			case "charset":
				c.Charset = kvParts[1]
			case "parseTime":
				c.ParseTime = kvParts[1] == "true" || kvParts[1] == "True"
			case "loc":
				c.Loc = kvParts[1]
			}
		}
	}
	return nil
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
	// m.db 为 nil 表示本实例尚未关联连接（懒加载），直接从连接池获取，不走重建流程
	if m.db == nil {
		db, err := m.GetDB()
		if err != nil {
			return err
		}
		m.db = db
		return nil
	}

	// 测试已有连接是否仍然可用
	sqlDB, err := m.db.DB()
	if err != nil {
		logx.Errorf("获取底层数据库连接失败: %v", err)
		return m.recreateConnection()
	}

	if err := sqlDB.Ping(); err != nil {
		logx.Infof("🔧 检测到连接异常，尝试重新连接: %v", err)
		return m.recreateConnection()
	}

	return nil
}

// 🔧 重建连接的方法（增强版）
func (m *MySQL) recreateConnection() error {
	// 清理当前连接
	m.cleanupCurrentConnection()

	// 🔧 添加重试机制
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		// 延迟重试
		if i > 0 {
			waitTime := time.Duration(i) * time.Second
			logx.Infof("⏳ 等待 %v 后重试连接...", waitTime)
			time.Sleep(waitTime)
		}

		// 重新获取连接
		db, err := m.GetDB()
		if err != nil {
			logx.Errorf("❌ 第 %d 次重建连接失败: %v", i+1, err)
			if i == maxRetries-1 {
				return fmt.Errorf("重建连接失败（已重试 %d 次）: %v", maxRetries, err)
			}
			continue
		}

		m.db = db
		logx.Info("✅ 数据库连接重建成功")
		return nil
	}

	return fmt.Errorf("重建连接失败: 已达到最大重试次数")
}

// cleanupCurrentConnection 清空本实例持有的连接引用
func (m *MySQL) cleanupCurrentConnection() {
	// 只清空本实例的引用，不关闭也不从共享连接池中移除连接。
	// 原因：contManager 是全局共享的，多个 MySQL 实例可能持有同一个 *gorm.DB 引用。
	// 如果将共享连接 Close() 掉，其他实例执行查询时会得到 "database is closed"，
	// 而它们再尝试重建时又会把刚创建的新连接关掉，形成无限循环。
	m.db = nil
	m.tx = nil
	m.isTansaction = false
}

// 延迟表检查方法
func (m *MySQL) ensureTable(data interface{}) error {
	return m.HasTable(data)
}

func (m *MySQL) GetDBName(data interface{}) error {
	// 1️⃣ config 中硬编码的数据库名优先级最高（静态配置场景）
	if m.config.Database != "" {
		m.Name = m.config.Database
		return nil
	}

	// 2️⃣ 从模型动态获取数据库名（多库路由场景：每次调用都取，不缓存）
	// 先于静态缓存 m.Name 检查，确保 TradeRecordModel/OrderDoneRecordModel
	// 等多交易对模型能按各自的 GetRemoteDBName() 路由到正确的库，
	// 而不会被第一次调用时的结果固化。
	if idb, ok := data.(types.IDBName); ok {
		dbName := idb.GetRemoteDBName()
		if dbName == "" {
			dbName = idb.GetLocalDBName()
		}
		if dbName != "" {
			if m.Name != dbName {
				// 目标库发生变化，丢弃旧连接以便 ensureValidConnection 重新建立
				m.Name = dbName
				if !m.isTansaction {
					// 事务外切换库：清空连接，下一步 ensureValidConnection 重建
					m.db = nil
				}
			}
			return nil
		}
	}

	// 3️⃣ 使用缓存的 m.Name（模型未提供 GetRemoteDBName 时的兜底）
	if m.Name != "" {
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
					// 连接不健康，仅从缓存中清除引用，不调用 Close()
					// 调用 Close() 会影响所有持有同一 *gorm.DB 的实例
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
			// 兜底懒加载（Transaction() 已 eager Begin，正常不走这里）
			m.tx = m.db.Begin()
			if m.tx.Error != nil {
				err := m.tx.Error
				m.tx = nil
				m.isTansaction = false
				return fmt.Errorf("懒加载事务失败: %v", err)
			}
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
	if m.config.Charset == "" {
		m.config.Charset = "utf8mb4"
	}
	if m.config.ParseTime == false {
		m.config.ParseTime = true
	}
	if m.config.Loc == "" {
		m.config.Loc = "Local"
	}
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
	if m.config.Charset == "" {
		m.config.Charset = "utf8mb4"
	}
	if m.config.ParseTime == false {
		m.config.ParseTime = true
	}
	if m.config.Loc == "" {
		m.config.Loc = "Local"
	}
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

// HasTable 检查并创建表（增强版）
func (m *MySQL) HasTable(model interface{}) error {
	// 🔧 先获取数据库名
	err := m.GetDBName(model)
	if err != nil {
		return err
	}

	if m.db == nil {
		_, err := m.GetDB()
		if err != nil {
			return err
		}
	}

	modelType := reflect.TypeOf(model)
	if modelType == nil {
		return errors.New("模型为 nil")
	}

	pointerDepth := 0
	finalType := modelType
	for finalType.Kind() == reflect.Ptr {
		finalType = finalType.Elem()
		pointerDepth++
	}

	if finalType.Kind() != reflect.Struct {
		return errors.New("模型必须是结构体类型")
	}

	if pointerDepth > 1 {
		return errors.New("模型不能是多级指针")
	}

	tableName := m.db.NamingStrategy.TableName(finalType.Name())
	cacheKey := TableCacheKey{
		DBPath:    m.Name,
		TableName: tableName,
	}

	// DB_FORCE_MIGRATE=true 时跳过缓存，强制检查并重建/修改表（用于开发调试或上线前结构同步）
	forceMigrate := os.Getenv("DB_FORCE_MIGRATE") == "true"

	if !forceMigrate {
		// 命中缓存：表已确认存在且迁移完毕，直接返回
		if _, cached := tableCache.Load(cacheKey); cached {
			return nil
		}
	}

	migrationLock.Lock()
	defer migrationLock.Unlock()

	if !forceMigrate {
		// 双检锁：加锁后再判断一次缓存，防止并发重复迁移
		if _, cached := tableCache.Load(cacheKey); cached {
			return nil
		}
	}

	// 查询数据库确认表是否真实存在
	var count int64
	err = m.db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?",
		m.Name, tableName).Scan(&count).Error

	if err != nil {
		logx.Errorf("检查表是否存在失败: %s.%s, 错误: %v", m.Name, tableName, err)
		return err
	}

	// 表已存在，检查并补全字段/索引
	if count > 0 {
		modelForMigration := reflect.New(finalType).Interface()
		if err := m.safeAutoMigrate(modelForMigration, tableName); err != nil {
			logx.Errorf("更新表结构失败: %s, 错误: %v", tableName, err)
			// 不返回错误，允许继续使用现有表
		}

		tableCache.Store(cacheKey, true)
		return m.processNestedTablesOptimized(modelForMigration, make(map[string]bool), 0, 2)
	}

	// 🔧 表不存在，创建表
	logx.Infof("🔧 表不存在，开始创建: %s.%s", m.Name, tableName)

	modelForMigration := reflect.New(finalType).Interface()

	// 🔧 使用更安全的创建方式
	migrator := m.db.Migrator()

	// 方式1：先尝试 CreateTable
	if err := migrator.CreateTable(modelForMigration); err != nil {
		errStr := err.Error()

		// 如果是"表已存在"错误，说明并发创建了
		if strings.Contains(errStr, "already exists") || strings.Contains(errStr, "Error 1050") {
			logx.Infof("⚠️ 表已被其他进程创建: %s", tableName)
			tableCache.Store(cacheKey, true)
			return m.processNestedTablesOptimized(modelForMigration, make(map[string]bool), 0, 2)
		}

		// 其他错误，尝试 AutoMigrate
		logx.Errorf("CreateTable 失败，尝试 AutoMigrate: %s, 错误: %v", tableName, err)
		if err := m.safeAutoMigrate(modelForMigration, tableName); err != nil {
			logx.Errorf("AutoMigrate 也失败: %s, 错误: %v", tableName, err)
			return err
		}
	}

	// 🔧 再次验证表是否创建成功
	var verifyCount int64
	err = m.db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?",
		m.Name, tableName).Scan(&verifyCount).Error

	if err != nil || verifyCount == 0 {
		tableCache.Delete(cacheKey)
		return fmt.Errorf("表创建失败或验证失败: %s.%s", m.Name, tableName)
	}

	logx.Infof("✅ 表创建成功: %s.%s", m.Name, tableName)
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

			nestedTableName := m.db.NamingStrategy.TableName(name1)
			var tableExists int64
			err := m.db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?",
				m.Name, nestedTableName).Scan(&tableExists).Error

			if err != nil {
				logx.Errorf("检查嵌套表失败: %s, 错误: %v", nestedTableName, err)
				return
			}

			obj := reflect.New(t).Interface()

			if tableExists == 0 {
				// 🔧 表不存在，使用 CreateTable 创建
				migrator := m.db.Migrator()
				if err := migrator.CreateTable(obj); err != nil {
					logx.Errorf("创建嵌套表失败: %s -> %s, 错误: %v", pname, name1, err)
					return
				}
				logx.Infof("✅ 创建嵌套表成功: %s", nestedTableName)
			} else {
				// 🔧 表已存在，使用安全的迁移方式
				if err := m.safeAutoMigrate(obj, nestedTableName); err != nil {
					logx.Errorf("迁移嵌套表失败: %s, 错误: %v", nestedTableName, err)
					return
				}
			}

			// 递归处理更深层的嵌套
			m.processNestedTablesOptimized(obj, processed, depth+1, maxDepth)
		}
	})

	return nil
}

// safeAutoMigrate 安全的自动迁移方法（完全重写）
func (m *MySQL) safeAutoMigrate(model interface{}, tableName string) error {
	migrator := m.db.Migrator()

	// 🔧 获取模型的 schema 信息
	stmt := &gorm.Statement{DB: m.db}
	if err := stmt.Parse(model); err != nil {
		return err
	}

	// 🔧 手动添加缺失的列（不使用 AutoMigrate）
	for _, field := range stmt.Schema.Fields {
		if field.DBName == "" {
			continue
		}

		// 检查列是否存在
		hasColumn := migrator.HasColumn(model, field.DBName)
		if !hasColumn {
			// 只添加新列
			if err := migrator.AddColumn(model, field.DBName); err != nil {
				errStr := err.Error()
				// 忽略列已存在的错误
				if !strings.Contains(errStr, "Duplicate column") &&
					!strings.Contains(errStr, "Error 1060") {
					logx.Errorf("添加列失败: %s.%s - %v", tableName, field.DBName, err)
				}
			} else {
				logx.Infof("✅ 添加新列: %s.%s", tableName, field.DBName)
			}
		}
	}

	// 🔧 手动创建索引（跳过已存在的）
	for _, idx := range stmt.Schema.ParseIndexes() {
		if !migrator.HasIndex(model, idx.Name) {
			if err := migrator.CreateIndex(model, idx.Name); err != nil {
				errStr := err.Error()
				// 忽略：索引已存在、重复键、重复值（表中已有重复数据无法建唯一索引）
				if !strings.Contains(errStr, "Duplicate key") &&
					!strings.Contains(errStr, "Error 1061") &&
					!strings.Contains(errStr, "already exists") &&
					!strings.Contains(errStr, "Error 1062") &&
					!strings.Contains(errStr, "Duplicate entry") {
					logx.Infof("⚠️ 创建索引失败（忽略）: %s.%s - %v", tableName, idx.Name, err)
				}
			} else {
				logx.Infof("✅ 创建索引: %s.%s", tableName, idx.Name)
			}
		}
	}

	logx.Infof("✅ 表迁移完成: %s", tableName)
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
	// eager Begin：在 isTansaction=false 期间立即验证连接并开启真实事务。
	// 若此时连接已死，立即返回错误，调用方降级为逐条操作，避免
	// "一批数据全部失败再 Rollback 失败" 的连锁场景。
	if err := m.ensureValidConnection(); err != nil {
		return fmt.Errorf("事务开启前连接检查失败: %v", err)
	}
	tx := m.db.Begin()
	if tx.Error != nil {
		return fmt.Errorf("开启事务失败: %v", tx.Error)
	}
	m.tx = tx
	m.isTansaction = true
	return nil
}

// errorHandler 错误处理（MySQL 版本 - 增强版）
func (m *MySQL) errorHandler(err error, data interface{}, fn func(db *gorm.DB, data interface{}) error) error {
	if err == nil {
		return fn(m.db, data)
	}

	// MySQL 特定的错误检查
	errStr := err.Error()

	// 🔧 表不存在错误
	if strings.Contains(errStr, "doesn't exist") || strings.Contains(errStr, "Error 1146") {
		logx.Infof("🔧 检测到表不存在错误，尝试创建表: %v", err)

		// 清除表缓存
		if m.Name != "" {
			modelType := reflect.TypeOf(data)
			if modelType.Kind() == reflect.Ptr {
				modelType = modelType.Elem()
			}
			tableName := m.db.NamingStrategy.TableName(modelType.Name())
			cacheKey := TableCacheKey{
				DBPath:    m.Name,
				TableName: tableName,
			}
			tableCache.Delete(cacheKey)
			logx.Infof("🔧 清除表缓存: %s.%s", m.Name, tableName)
		}

		// 强制创建表
		if err := m.HasTable(data); err != nil {
			logx.Errorf("❌ 自动创建表失败: %v", err)
			return err
		}

		// 重试操作
		logx.Infof("🔄 表创建成功，重试操作...")
		return fn(m.db, data)
	}

	// 🔧 字段不存在或其他结构问题
	if strings.Contains(errStr, "Unknown column") ||
		strings.Contains(errStr, "Column") && strings.Contains(errStr, "cannot be null") {
		logx.Infof("🔧 检测到字段错误，尝试更新表结构: %v", err)

		if err := m.HasTable(data); err != nil {
			return err
		}

		return fn(m.db, data)
	}

	// 🔧 连接级错误（bad connection / commands out of sync）：刷新连接池空闲连接
	// （SetMaxIdleConns(0) 立即关闭全部空闲连接，清除协议失步的坏连接），然后重试一次。
	// 仅在非事务场景下重试；事务场景由调用方的 Rollback+逐条降级处理。
	if !m.isTansaction && isConnectionError(err) {
		logx.Infof("🔧 检测到连接错误，刷新连接池后重试: %v", err)
		if sqlDB, e := m.db.DB(); e == nil {
			sqlDB.SetMaxIdleConns(0)                     // 驱逐全部空闲坏连接
			sqlDB.SetMaxIdleConns(m.config.MaxIdleConns) // 恢复连接池大小
		}
		retryErr := fn(m.db, data)
		if retryErr != nil {
			logx.Errorf("连接错误重试仍失败: %v", retryErr)
			return retryErr
		}
		return nil
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

// isConnectionError 判断是否是连接级错误（坏连接、MySQL 协议失步等）。
// go-sql-driver 的 "commands out of sync" / "invalid connection" 不返回 driver.ErrBadConn，
// 所以 database/sql 不会自动重试，需要在业务层主动处理。
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "bad connection") ||
		strings.Contains(s, "invalid connection") ||
		strings.Contains(s, "commands out of sync") ||
		strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "connection reset by peer")
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
	// 始终重置事务状态，防止回滚失败（如连接已断）时 isTansaction 残留，
	// 导致后续逐条降级操作误入事务分支开启未提交的新事务。
	defer func() {
		m.tx = nil
		m.isTansaction = false
	}()
	if m.tx != nil {
		return m.tx.Rollback().Error
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

// Exists 判断数据行是否存在
func (m *MySQL) Exists(data interface{}) (bool, error) {
	err := m.init(data)
	if err != nil {
		return false, err
	}

	// 确保表存在
	if err := m.ensureTable(data); err != nil {
		return false, err
	}

	var db *gorm.DB
	if m.isTansaction {
		db = m.tx
	} else {
		db = m.db
	}

	// 🔧 调用通用方法
	return existsData(db, data)
}

// ExistsByCondition 根据自定义条件判断数据行是否存在
func (m *MySQL) ExistsByCondition(model interface{}, condition string, args ...interface{}) (bool, error) {
	err := m.init(model)
	if err != nil {
		return false, err
	}

	// 确保表存在
	if err := m.ensureTable(model); err != nil {
		return false, err
	}

	var db *gorm.DB
	if m.isTansaction {
		db = m.tx
	} else {
		db = m.db
	}

	// 🔧 调用通用方法
	return existsByCondition(db, model, condition, args...)
}

// ExistsByHashcode 根据 hashcode 判断数据行是否存在（快速方法）
func (m *MySQL) ExistsByHashcode(model interface{}, hashcode string) (bool, error) {
	err := m.init(model)
	if err != nil {
		return false, err
	}

	// 确保表存在
	if err := m.ensureTable(model); err != nil {
		return false, err
	}

	var db *gorm.DB
	if m.isTansaction {
		db = m.tx
	} else {
		db = m.db
	}

	// 🔧 调用通用方法
	return existsByHashcode(db, model, hashcode)
}

// ExistsByID 根据 ID 判断数据行是否存在（快速方法）
func (m *MySQL) ExistsByID(model interface{}, id int64) (bool, error) {
	err := m.init(model)
	if err != nil {
		return false, err
	}

	// 确保表存在
	if err := m.ensureTable(model); err != nil {
		return false, err
	}

	var db *gorm.DB
	if m.isTansaction {
		db = m.tx
	} else {
		db = m.db
	}

	// 🔧 调用通用方法
	return existsByID(db, model, id)
}
