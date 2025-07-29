package oltp

import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
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

	// 不在 init 中检查表，延迟到真正需要时
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

func (own *Sqlite) GetDB() (*gorm.DB, error) {
	key := own.Name
	if key == "" {
		key = "models"
	}

	path, err := local.GetDbPath(key)
	if err != nil {
		return nil, err
	}

	dns := path + ".ldb"
	own.Path = dns

	if db, ok := connManager.GetConnection(dns); ok {
		own.db = db
		return db, nil
	}

	dia := sqlite.Open(dns)
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

	own.db = db
	if !config.INITSERVER {
		connManager.SetConnection(dns, db)
	}

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
	return createData(own.db, data)
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
	return updateData(own.db, data)
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
	return deleteData(own.db, data)
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

// 添加清理方法
func (own *Sqlite) cleanup() {
	// 定期清理缓存
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			runtime.GC()
			runtime.GC() // 双重GC
		}
	}()
}
