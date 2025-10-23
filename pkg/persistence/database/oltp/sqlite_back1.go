package oltp

// import (
// 	"errors"
// 	"fmt"
// 	"reflect"
// 	"strings"
// 	"time"

// 	"github.com/digitalwayhk/core/pkg/persistence/local"
// 	"github.com/digitalwayhk/core/pkg/persistence/types"
// 	"github.com/digitalwayhk/core/pkg/server/config"
// 	"github.com/digitalwayhk/core/pkg/utils"
// 	"github.com/zeromicro/go-zero/core/logx"

// 	"gorm.io/driver/sqlite"
// 	"gorm.io/gorm"
// 	"gorm.io/gorm/logger"
// 	"gorm.io/gorm/schema"
// )

// // 数据库模式枚举
// type DatabaseMode int

// const (
// 	RegularMode DatabaseMode = iota
// 	WALMode
// 	AutoMode // 智能选择
// )

// // 负载级别
// type LoadLevel int

// const (
// 	LightLoad  LoadLevel = iota // 轻负载
// 	MediumLoad                  // 中等负载
// 	HeavyLoad                   // 重负载
// )

// // 数据重要性
// type DataImportance int

// const (
// 	LowImportanceData DataImportance = iota
// 	ImportantData
// 	CriticalData
// )

// // 环境类型
// type Environment int

// const (
// 	LocalDisk Environment = iota
// 	NetworkFS
// 	Container
// 	Mobile
// )

// // 数据安全配置
// type DataSafetyConfig struct {
// 	EnableWAL          bool
// 	SyncMode           string // OFF, NORMAL, FULL, EXTRA
// 	CheckpointInterval int
// 	BackupEnabled      bool
// 	IntegrityCheck     bool
// }

// // 数据库模式选择器
// type DatabaseModeSelector struct {
// 	dbPath         string
// 	expectedLoad   LoadLevel
// 	dataImportance DataImportance
// 	environment    Environment
// }

// type Sqlite struct {
// 	Name         string
// 	Size         float64 //库大小
// 	UpdateTime   int32   //数据最后更新时间
// 	Path         string  //库文件路径
// 	db           *gorm.DB
// 	tx           *gorm.DB
// 	isTansaction bool
// 	tables       map[string]*TableMaster
// 	IsLog        bool
// 	currentMode  DatabaseMode // 当前使用的模式
// }

// func NewSqlite() *Sqlite {
// 	sql := &Sqlite{
// 		tables:      make(map[string]*TableMaster),
// 		currentMode: AutoMode, // 默认使用自动模式
// 	}
// 	return sql
// }

// func (own *Sqlite) init(data interface{}) error {
// 	err := own.GetDBName(data)
// 	if err != nil {
// 		return err
// 	}

// 	// 检查数据库文件是否存在
// 	dns, err := own.getPath()
// 	if err != nil {
// 		return err
// 	}

// 	// 如果数据库文件不存在，清除连接缓存
// 	if !utils.IsFile(dns) {
// 		connManager.SetConnection(dns, nil)
// 		own.db = nil
// 		own.tx = nil
// 	}

// 	// 使用新的连接检查方法
// 	if err := own.ensureValidConnection(); err != nil {
// 		return err
// 	}

// 	if own.isTansaction {
// 		if own.tx == nil {
// 			own.tx = own.db.Begin()
// 		}
// 	}

// 	return nil
// }

// func (own *Sqlite) ensureValidConnection() error {
// 	if own.db == nil {
// 		_, err := own.GetDB()
// 		return err
// 	}

// 	// 检查连接是否有效
// 	sqlDB, err := own.db.DB()
// 	if err != nil {
// 		logx.Errorf("获取底层数据库连接失败: %v", err)
// 		return own.recreateConnection()
// 	}

// 	// 测试连接
// 	if err := sqlDB.Ping(); err != nil {
// 		logx.Errorf("数据库连接ping失败: %v", err)
// 		return own.recreateConnection()
// 	}

// 	return nil
// }

// // 重建连接的方法
// func (own *Sqlite) recreateConnection() error {
// 	// 清理当前连接
// 	own.cleanupCurrentConnection()

// 	// 重新获取连接
// 	newDB, err := own.GetDB()
// 	if err != nil {
// 		return fmt.Errorf("重建数据库连接失败: %v", err)
// 	}

// 	own.db = newDB
// 	logx.Infof("数据库连接已重建: %s", own.Path)
// 	return nil
// }

// // 清理当前连接
// func (own *Sqlite) cleanupCurrentConnection() {
// 	if own.db != nil {
// 		if sqlDB, err := own.db.DB(); err == nil {
// 			sqlDB.Close()
// 		}
// 		own.db = nil
// 	}

// 	// 从连接池中移除
// 	dns, _ := own.getPath()
// 	connManager.SetConnection(dns, nil)
// }

// // 延迟表检查方法
// func (own *Sqlite) ensureTable(data interface{}) error {
// 	return own.HasTable(data)
// }

// func (own *Sqlite) GetDBName(data interface{}) error {
// 	if idb, ok := data.(types.IDBName); ok {
// 		own.Name = idb.GetLocalDBName()
// 		if own.Name == "" {
// 			return errors.New("db name is empty")
// 		}
// 		return nil
// 	}
// 	return errors.New("db name is empty")
// }

// func (own *Sqlite) GetModelDB(model interface{}) (interface{}, error) {
// 	err := own.init(model)
// 	return own.db, err
// }

// func (own *Sqlite) DeleteDB() error {
// 	dns, err := own.getPath()
// 	if err != nil {
// 		return err
// 	}

// 	// 在删除文件前先关闭所有数据库连接
// 	if err := own.closeAllConnections(); err != nil {
// 		logx.Errorf("关闭数据库连接失败: %v", err)
// 		// 继续执行，不要因为关闭连接失败而阻止删除文件
// 	}

// 	// 清除连接缓存（在删除文件前）
// 	connManager.SetConnection(dns, nil)

// 	// 重置当前实例的连接
// 	own.db = nil
// 	own.tx = nil
// 	own.isTansaction = false

// 	// 删除数据库文件
// 	err = utils.DeleteFile(dns)
// 	if err != nil {
// 		logx.Errorf("删除数据库文件失败: %s, 错误: %v", dns, err)
// 		return err
// 	}

// 	// 删除WAL相关文件
// 	own.deleteWALFiles(dns)

// 	// 清除表缓存
// 	own.clearTableCache()

// 	return nil
// }

// // 删除WAL相关文件
// func (own *Sqlite) deleteWALFiles(dbPath string) {
// 	walFiles := []string{
// 		dbPath + "-wal",
// 		dbPath + "-shm",
// 	}

// 	for _, walFile := range walFiles {
// 		if utils.IsFile(walFile) {
// 			if err := utils.DeleteFile(walFile); err != nil {
// 				logx.Errorf("删除WAL文件失败: %s, 错误: %v", walFile, err)
// 			}
// 		}
// 	}
// }

// // 关闭所有数据库连接
// func (own *Sqlite) closeAllConnections() error {
// 	var lastError error

// 	// 关闭事务连接
// 	if own.tx != nil {
// 		if tx := own.tx.Rollback(); tx != nil {
// 			logx.Errorf("回滚事务失败: %v", tx.Error)
// 			lastError = tx.Error
// 		}
// 		own.tx = nil
// 		own.isTansaction = false
// 	}

// 	// 关闭主数据库连接
// 	if own.db != nil {
// 		if sqlDB, err := own.db.DB(); err == nil {
// 			if err := sqlDB.Close(); err != nil {
// 				logx.Errorf("关闭数据库连接失败: %v", err)
// 				lastError = err
// 			}
// 		}
// 		own.db = nil
// 	}

// 	return lastError
// }

// // 清除表缓存
// func (own *Sqlite) clearTableCache() {
// 	// 清除与此数据库相关的表缓存
// 	tableCache.Range(func(key, value interface{}) bool {
// 		if cacheKey, ok := key.(TableCacheKey); ok {
// 			if cacheKey.DBPath == own.Path {
// 				tableCache.Delete(key)
// 			}
// 		}
// 		return true
// 	})
// }

// func (own *Sqlite) getPath() (string, error) {
// 	key := own.Name
// 	if key == "" {
// 		key = "models"
// 	}

// 	path, err := local.GetDbPath(key)
// 	if err != nil {
// 		return "", err
// 	}

// 	dns := path + ".ldb"
// 	own.Path = dns
// 	return dns, nil
// }

// // 获取数据库连接 - 智能模式选择
// func (own *Sqlite) GetDB() (*gorm.DB, error) {
// 	dns, err := own.getPath()
// 	if err != nil {
// 		return nil, err
// 	}

// 	// 检查文件是否存在
// 	if !utils.IsFile(dns) {
// 		// 先关闭现有连接再清除缓存
// 		if db, ok := connManager.GetConnection(dns); ok && db != nil {
// 			if sqlDB, err := db.DB(); err == nil {
// 				sqlDB.Close()
// 			}
// 		}
// 		connManager.SetConnection(dns, nil)
// 		own.db = nil
// 	}

// 	if db, ok := connManager.GetConnection(dns); ok {
// 		if db != nil {
// 			// 检查连接健康状态
// 			if sqlDB, err := db.DB(); err == nil {
// 				if err := sqlDB.Ping(); err == nil {
// 					own.db = db
// 					return db, nil
// 				} else {
// 					// 连接不健康，关闭并清理
// 					sqlDB.Close()
// 					connManager.SetConnection(dns, nil)
// 				}
// 			}
// 		}
// 	}

// 	// 使用智能模式选择
// 	useWAL, reason := own.shouldUseWAL()
// 	logx.Infof("数据库 %s 模式选择: WAL=%v (%s)", own.Name, useWAL, reason)

// 	if useWAL {
// 		own.db, err = own.newDBWithWAL()
// 		if err != nil {
// 			logx.Errorf("%s:WAL模式失败，降级到常规模式: %v,file:%s", own.Name, err, own.Path)
// 			own.db, err = own.newDB()
// 			own.currentMode = RegularMode
// 		} else {
// 			own.currentMode = WALMode
// 		}
// 	} else {
// 		own.db, err = own.newDB()
// 		own.currentMode = RegularMode
// 	}

// 	if err != nil {
// 		return nil, err
// 	}

// 	if !config.INITSERVER {
// 		connManager.SetConnection(dns, own.db)
// 	}
// 	return own.db, nil
// }

// // 智能模式选择
// func (own *Sqlite) shouldUseWAL() (bool, string) {
// 	selector := &DatabaseModeSelector{
// 		dbPath:         own.Path,
// 		expectedLoad:   own.detectLoadLevel(),
// 		dataImportance: own.detectDataImportance(),
// 		environment:    own.detectEnvironment(),
// 	}

// 	return selector.recommend()
// }

// func (selector *DatabaseModeSelector) recommend() (bool, string) {
// 	// 强制使用非WAL的场景
// 	if selector.environment == NetworkFS {
// 		return false, "网络文件系统不建议使用WAL"
// 	}

// 	if selector.environment == Mobile {
// 		return false, "移动设备存储限制"
// 	}

// 	// 强制使用WAL的场景
// 	if selector.dataImportance == CriticalData && selector.expectedLoad >= MediumLoad {
// 		return true, "关键数据 + 中高负载必须使用WAL"
// 	}

// 	if selector.expectedLoad == HeavyLoad {
// 		return true, "高并发负载必须使用WAL"
// 	}

// 	// 基于数据库特性决定
// 	if strings.Contains(selector.dbPath, "websocket") ||
// 		strings.Contains(selector.dbPath, "realtime") ||
// 		strings.Contains(selector.dbPath, "cache") {
// 		return true, "实时数据建议使用WAL"
// 	}

// 	// 日志和临时数据可以不用WAL
// 	if strings.Contains(selector.dbPath, "log") ||
// 		strings.Contains(selector.dbPath, "temp") {
// 		return false, "临时数据无需WAL保护"
// 	}

// 	// 默认推荐WAL
// 	return true, "默认推荐WAL模式"
// }

// func (own *Sqlite) detectLoadLevel() LoadLevel {
// 	// 基于数据库名称和预期用途判断负载
// 	if strings.Contains(own.Name, "user") ||
// 		strings.Contains(own.Name, "order") ||
// 		strings.Contains(own.Name, "realtime") {
// 		return HeavyLoad
// 	}

// 	if strings.Contains(own.Name, "config") ||
// 		strings.Contains(own.Name, "system") {
// 		return LightLoad
// 	}

// 	return MediumLoad
// }

// func (own *Sqlite) detectDataImportance() DataImportance {
// 	// 基于数据库名称判断重要性
// 	if strings.Contains(own.Name, "user") ||
// 		strings.Contains(own.Name, "order") ||
// 		strings.Contains(own.Name, "payment") ||
// 		strings.Contains(own.Name, "account") {
// 		return CriticalData
// 	}

// 	if strings.Contains(own.Name, "log") ||
// 		strings.Contains(own.Name, "cache") ||
// 		strings.Contains(own.Name, "temp") {
// 		return LowImportanceData
// 	}

// 	return ImportantData
// }

// func (own *Sqlite) detectEnvironment() Environment {
// 	dns, _ := own.getPath()

// 	// 检查是否在容器中
// 	if utils.IsFile("/.dockerenv") {
// 		return Container
// 	}

// 	// 检查是否在网络文件系统
// 	if strings.HasPrefix(dns, "/mnt/") ||
// 		strings.Contains(dns, "nfs") {
// 		return NetworkFS
// 	}

// 	return LocalDisk
// }

// // 检查WAL模式兼容性
// func (own *Sqlite) checkWALCompatibility() error {
// 	if own.db == nil {
// 		return errors.New("database connection not initialized")
// 	}

// 	sqlDB, err := own.db.DB()
// 	if err != nil {
// 		return err
// 	}

// 	var walMode string
// 	row := sqlDB.QueryRow("PRAGMA journal_mode=WAL;")
// 	err = row.Scan(&walMode)

// 	if err != nil || strings.ToLower(walMode) != "wal" {
// 		return fmt.Errorf("WAL模式不兼容当前文件系统")
// 	}

// 	return nil
// }

// // 验证WAL模式是否启用
// func (own *Sqlite) verifyWALMode() bool {
// 	if own.db == nil {
// 		return false
// 	}

// 	sqlDB, err := own.db.DB()
// 	if err != nil {
// 		return false
// 	}

// 	var journalMode string
// 	row := sqlDB.QueryRow("PRAGMA journal_mode;")
// 	err = row.Scan(&journalMode)

// 	return err == nil && strings.ToLower(journalMode) == "wal"
// }

// // 常规模式数据库连接
// func (own *Sqlite) newDB() (*gorm.DB, error) {
// 	dia := sqlite.Open(own.Path)
// 	db, err := gorm.Open(dia, &gorm.Config{
// 		DisableForeignKeyConstraintWhenMigrating: true,
// 		NamingStrategy: schema.NamingStrategy{
// 			SingularTable: true,
// 			NoLowerCase:   true,
// 		},
// 		PrepareStmt:              true,  // 启用预编译语句
// 		DisableAutomaticPing:     false, // 启用ping检测
// 		DisableNestedTransaction: true,  // 禁用嵌套事务
// 		SkipDefaultTransaction:   true,  // 跳过默认事务以提高性能
// 		Logger:                   logger.Default.LogMode(logger.Silent),
// 	})

// 	if err != nil {
// 		return nil, err
// 	}

// 	// 更严格的连接池配置
// 	sqlDB, err := db.DB()
// 	if err != nil {
// 		return nil, err
// 	}

// 	sqlDB.SetMaxIdleConns(1)                  // 最小空闲连接
// 	sqlDB.SetMaxOpenConns(3)                  // 稍微增加但保持较小
// 	sqlDB.SetConnMaxLifetime(5 * time.Minute) // 缩短生存时间
// 	sqlDB.SetConnMaxIdleTime(2 * time.Minute) // 空闲超时

// 	return db, nil
// }

// // WAL模式优化数据库连接
// func (own *Sqlite) newDBWithWAL() (*gorm.DB, error) {
// 	// WAL模式连接字符串
// 	connStr := own.Path + "?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=1000&_temp_store=memory"

// 	dia := sqlite.Open(connStr)
// 	db, err := gorm.Open(dia, &gorm.Config{
// 		DisableForeignKeyConstraintWhenMigrating: true,
// 		NamingStrategy: schema.NamingStrategy{
// 			SingularTable: true,
// 			NoLowerCase:   true,
// 		},
// 		PrepareStmt:              true, // 启用预编译语句
// 		DisableAutomaticPing:     false,
// 		DisableNestedTransaction: false, // 支持嵌套事务
// 		SkipDefaultTransaction:   false, // 保持事务安全
// 		Logger:                   logger.Default.LogMode(logger.Silent),
// 	})

// 	if err != nil {
// 		return nil, err
// 	}

// 	sqlDB, err := db.DB()
// 	if err != nil {
// 		return nil, err
// 	}

// 	// 优化的连接池配置
// 	sqlDB.SetMaxIdleConns(5)
// 	sqlDB.SetMaxOpenConns(10)
// 	sqlDB.SetConnMaxLifetime(30 * time.Minute)
// 	sqlDB.SetConnMaxIdleTime(10 * time.Minute)

// 	// WAL模式配置
// 	pragmas := []string{
// 		"PRAGMA journal_mode=WAL;",
// 		"PRAGMA synchronous=NORMAL;",
// 		"PRAGMA cache_size=1000;",
// 		"PRAGMA temp_store=memory;",
// 		"PRAGMA mmap_size=268435456;", // 256MB mmap
// 		"PRAGMA wal_autocheckpoint=1000;",
// 		"PRAGMA optimize;",
// 	}

// 	for _, pragma := range pragmas {
// 		if _, err := sqlDB.Exec(pragma); err != nil {
// 			logx.Errorf("执行PRAGMA失败: %s, 错误: %v", pragma, err)
// 		}
// 	}

// 	// 验证WAL模式是否成功启用
// 	if !own.verifyWALMode() {
// 		return nil, errors.New("WAL模式启用失败")
// 	}

// 	return db, nil
// }

// // 数据完整性检查
// func (own *Sqlite) performIntegrityCheck() error {
// 	if own.db == nil {
// 		return errors.New("database connection not initialized")
// 	}

// 	var result string
// 	err := own.db.Raw("PRAGMA integrity_check").Scan(&result).Error
// 	if err != nil {
// 		return err
// 	}

// 	if result != "ok" {
// 		logx.Errorf("数据完整性检查失败: %s", result)
// 		return fmt.Errorf("数据库完整性检查失败: %s", result)
// 	}

// 	logx.Infof("✅ 数据库完整性检查通过: %s", own.Name)
// 	return nil
// }

// // 获取当前模式
// func (own *Sqlite) GetCurrentMode() DatabaseMode {
// 	return own.currentMode
// }

// // 强制切换到WAL模式
// func (own *Sqlite) ForceWALMode() error {
// 	if own.currentMode == WALMode {
// 		return nil // 已经是WAL模式
// 	}

// 	// 关闭当前连接
// 	own.cleanupCurrentConnection()

// 	// 使用WAL模式重新连接
// 	var err error
// 	own.db, err = own.newDBWithWAL()
// 	if err != nil {
// 		return fmt.Errorf("强制切换到WAL模式失败: %v", err)
// 	}

// 	own.currentMode = WALMode
// 	logx.Infof("已强制切换到WAL模式: %s", own.Name)
// 	return nil
// }

// // 跨库事务支持
// func (own *Sqlite) AttachDatabase(aliasName, dbPath string) error {
// 	if own.db == nil {
// 		if _, err := own.GetDB(); err != nil {
// 			return err
// 		}
// 	}

// 	sql := fmt.Sprintf("ATTACH DATABASE '%s' AS %s", dbPath, aliasName)
// 	return own.db.Exec(sql).Error
// }

// func (own *Sqlite) DetachDatabase(aliasName string) error {
// 	if own.db == nil {
// 		return errors.New("database connection not initialized")
// 	}

// 	sql := fmt.Sprintf("DETACH DATABASE %s", aliasName)
// 	return own.db.Exec(sql).Error
// }

// func (own *Sqlite) HasTable(model interface{}) error {
// 	if config.INITSERVER || (own.db != nil && own.db.DryRun) {
// 		return nil
// 	}

// 	if own.db == nil {
// 		db, err := own.GetDB()
// 		if err != nil {
// 			return err
// 		}
// 		own.db = db
// 	}

// 	if _, ok := model.(types.IDBSQL); ok {
// 		return nil
// 	}

// 	// 获取表名
// 	tableName := own.db.NamingStrategy.TableName(reflect.TypeOf(model).Elem().Name())
// 	cacheKey := TableCacheKey{
// 		DBPath:    own.Path,
// 		TableName: tableName,
// 	}

// 	// 检查缓存
// 	if _, exists := tableCache.Load(cacheKey); exists {
// 		return nil // 已处理过，直接返回
// 	}

// 	// 使用锁防止并发迁移
// 	migrationLock.Lock()
// 	defer migrationLock.Unlock()

// 	// 双重检查
// 	if _, exists := tableCache.Load(cacheKey); exists {
// 		return nil
// 	}

// 	// 快速检查表是否存在，避免调用复杂的 Migrator
// 	var count int64
// 	err := own.db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", tableName).Scan(&count).Error
// 	if err == nil && count > 0 {
// 		tableCache.Store(cacheKey, true)
// 		return nil
// 	}

// 	// 只在表不存在时才执行迁移
// 	err = own.db.AutoMigrate(model)
// 	if err != nil {
// 		logx.Errorf("创建表失败: %s, 错误: %v", tableName, err)
// 		return err
// 	}

// 	// 缓存结果
// 	tableCache.Store(cacheKey, true)

// 	// 处理嵌套表，但限制深度
// 	return own.processNestedTablesOptimized(model, make(map[string]bool), 0, 2)
// }

// // 优化嵌套表处理，添加深度限制
// func (own *Sqlite) processNestedTablesOptimized(model interface{}, processed map[string]bool, depth, maxDepth int) error {
// 	if depth >= maxDepth {
// 		return nil // 超过最大深度，停止递归
// 	}

// 	typeName := utils.GetTypeName(model)
// 	if processed[typeName] {
// 		return nil // 已处理过，避免循环
// 	}
// 	processed[typeName] = true

// 	utils.DeepForItem(model, func(field, parent reflect.StructField, kind utils.TypeKind) {
// 		if kind == utils.Array {
// 			t := field.Type.Elem()
// 			if t.Kind() == reflect.Ptr {
// 				t = t.Elem()
// 			}

// 			name1 := t.Name()
// 			pname := utils.GetTypeName(model)
// 			if name1 == pname {
// 				return // 避免自引用
// 			}
// 			obj := reflect.New(t).Interface()
// 			err := own.db.AutoMigrate(obj)
// 			if err != nil {
// 				logx.Errorf("处理嵌套表失败: %s -> %s, 错误: %v", pname, name1, err)
// 			}
// 			// 递归处理嵌套表
// 			own.processNestedTablesOptimized(obj, processed, depth+1, maxDepth)
// 		}
// 	})

// 	return nil
// }

// func (own *Sqlite) Load(item *types.SearchItem, result interface{}) error {
// 	err := own.init(item.Model)
// 	if err != nil {
// 		return err
// 	}
// 	// 确保表存在
// 	err = own.ensureTable(item.Model)
// 	if err != nil {
// 		return err
// 	}
// 	if item.IsStatistical {
// 		return sum(own.db, item, result)
// 	}
// 	if own.isTansaction {
// 		return load(own.tx, item, result)
// 	}
// 	return load(own.db, item, result)
// }

// func (own *Sqlite) Raw(sql string, data interface{}) error {
// 	obj := utils.NewArrayItem(data)
// 	err := own.init(obj)
// 	if err != nil {
// 		return err
// 	}
// 	own.db.Raw(sql).Scan(data)
// 	return own.db.Error
// }

// func (own *Sqlite) Exec(sql string, data interface{}) error {
// 	err := own.init(data)
// 	if err != nil {
// 		return err
// 	}
// 	own.db.Exec(sql, data)
// 	return own.db.Error
// }

// func (own *Sqlite) Transaction() {
// 	own.isTansaction = true
// }

// func (own *Sqlite) Insert(data interface{}) error {
// 	err := own.init(data)
// 	if err != nil {
// 		return err
// 	}
// 	// 确保表存在
// 	err = own.ensureTable(data)
// 	if err != nil {
// 		return err
// 	}
// 	if own.isTansaction {
// 		err := createData(own.tx, data)
// 		if err != nil {
// 			fmt.Println(own.Path)
// 			own.tx.Rollback()
// 			return err
// 		}
// 		return nil
// 	}
// 	err = createData(own.db, data)
// 	if err != nil {
// 		err = own.errorHandler(err, data, createData)
// 	}
// 	return err
// }

// func (own *Sqlite) errorHandler(err error, data interface{}, fn func(db *gorm.DB, data interface{}) error) error {
// 	if err == nil {
// 		return nil
// 	}
// 	// 检查是否是列不存在的错误
// 	if strings.Contains(err.Error(), "no such column") ||
// 		strings.Contains(err.Error(), "has no column named") ||
// 		strings.Contains(err.Error(), "ambiguous column name") ||
// 		strings.Contains(err.Error(), "no such table") ||
// 		strings.Contains(err.Error(), "datatype mismatch") {
// 		err := own.db.AutoMigrate(data)
// 		if err == nil {
// 			return fn(own.db, data)
// 		}
// 	}
// 	return err
// }

// func (own *Sqlite) Update(data interface{}) error {
// 	err := own.init(data)
// 	if err != nil {
// 		return err
// 	}
// 	// 确保表存在
// 	err = own.ensureTable(data)
// 	if err != nil {
// 		return err
// 	}
// 	if own.isTansaction {
// 		err := updateData(own.tx, data)
// 		if err != nil {
// 			fmt.Println(own.Path)
// 			own.tx.Rollback()
// 			return err
// 		}
// 		return nil
// 	}
// 	err = updateData(own.db, data)
// 	if err != nil {
// 		err = own.errorHandler(err, data, updateData)
// 	}
// 	return err
// }

// func (own *Sqlite) Delete(data interface{}) error {
// 	err := own.init(data)
// 	if err != nil {
// 		return err
// 	}
// 	// 确保表存在
// 	err = own.ensureTable(data)
// 	if err != nil {
// 		return err
// 	}
// 	if own.isTansaction {
// 		err := deleteData(own.tx, data)
// 		if err != nil {
// 			fmt.Println(own.Path)
// 			own.tx.Rollback()
// 			return err
// 		}
// 		return nil
// 	}
// 	err = deleteData(own.db, data)
// 	if err != nil {
// 		err = own.errorHandler(err, data, deleteData)
// 	}
// 	return err
// }

// func (own *Sqlite) Commit() error {
// 	own.isTansaction = false
// 	if own.tx != nil {
// 		own.tx.Commit()
// 		err := own.tx.Error
// 		own.tx = nil
// 		return err
// 	}
// 	return nil
// }

// func (own *Sqlite) GetRunDB() interface{} {
// 	return own.db
// }
