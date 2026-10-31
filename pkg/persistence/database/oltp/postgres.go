// 本文件实现 PostgreSQL 数据操作器：共享连接池，Clone 独立持有事务状态。
package oltp

import (
	"crypto/sha256"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sync"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// Postgres 实现模型数据访问。非事务访问并发共池，事务必须使用 Clone。
type Postgres struct {
	config      types.StorageConfig
	mu          sync.Mutex
	db          *gorm.DB
	tx          *gorm.DB
	txRequested bool
	txDBName    string
	initialized *sync.Map
}

// NewPostgres 创建懒连接的 PostgreSQL action，并复制配置防止调用方修改。
func NewPostgres(c types.StorageConfig) *Postgres {
	c.Driver = "postgres"
	c.ApplyDefaults()
	return &Postgres{config: c, initialized: &sync.Map{}}
}

// Clone 只共享不可变配置与连接池，不继承活动事务。
func (p *Postgres) Clone() types.IDataAction {
	clone := NewPostgres(p.config)
	clone.initialized = p.initialized
	return clone
}

// GetMaxOpenConns 为可靠写提供实际连接并发预算。
func (p *Postgres) GetMaxOpenConns() int { return p.config.MaxOpenConns }

// GetSyncPoolKey 返回不包含明文凭证的真实存储身份。
func (p *Postgres) GetSyncPoolKey(model interface{}) string {
	name, err := p.resolveName(model)
	if err != nil {
		return ""
	}
	return storageIdentity(p.config, name)
}

func storageModel(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	v := reflect.ValueOf(value)
	for {
		switch v.Kind() {
		case reflect.Ptr:
			if v.IsNil() {
				return zeroStorageModel(v.Type())
			}
			v = v.Elem()
		case reflect.Slice, reflect.Array:
			if v.Len() == 0 {
				return zeroStorageModel(v.Type())
			}
			v = v.Index(0)
		case reflect.Struct:
			model := reflect.New(v.Type())
			model.Elem().Set(v)
			return model.Interface()
		default:
			return value
		}
	}
}
func zeroStorageModel(t reflect.Type) interface{} {
	for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	return reflect.New(t).Interface()
}

func (p *Postgres) resolveName(model interface{}) (string, error) {
	if p.config.Database != "" {
		return p.config.Database, nil
	}
	value := reflect.ValueOf(model)
	for value.IsValid() && value.Kind() == reflect.Ptr && !value.IsNil() {
		value = value.Elem()
	}
	if value.IsValid() && (value.Kind() == reflect.Slice || value.Kind() == reflect.Array) && value.Len() > 0 {
		name, err := p.resolveName(value.Index(0).Interface())
		if err != nil {
			return "", err
		}
		for i := 1; i < value.Len(); i++ {
			next, err := p.resolveName(value.Index(i).Interface())
			if err != nil {
				return "", err
			}
			if next != name {
				return "", fmt.Errorf("batch spans multiple databases")
			}
		}
		return name, nil
	}
	// 保留分库实例字段，同时将类型化nil转换为零值模型，缺少分库键时明确拒绝。
	model = storageModel(model)
	if named, ok := model.(types.IDBName); ok {
		if name := named.GetRemoteDBName(); name != "" {
			return name, nil
		}
		if name := named.GetLocalDBName(); name != "" {
			return name, nil
		}
	}
	return "", fmt.Errorf("PostgreSQL model database is empty")
}

func (p *Postgres) pool(name string) (*gorm.DB, error) {
	if err := p.config.Validate(false); err != nil {
		return nil, err
	}
	return connManager.GetOrCreate(storageIdentity(p.config, name), func() (*gorm.DB, error) { return p.connect(name) })
}

func (p *Postgres) connect(name string) (*gorm.DB, error) {
	open := func(database string) (*gorm.DB, error) {
		db, err := gorm.Open(postgres.Open(p.dsn(database)), &gorm.Config{NamingStrategy: schema.NamingStrategy{SingularTable: true}, DisableForeignKeyConstraintWhenMigrating: true, Logger: logger.Discard})
		if err != nil {
			if db != nil {
				closeConnectionPool(db)
			}
			return nil, err
		}
		sqlDB, err := db.DB()
		if err != nil {
			closeConnectionPool(db)
			return nil, err
		}
		sqlDB.SetMaxIdleConns(p.config.MaxIdleConns)
		sqlDB.SetMaxOpenConns(p.config.MaxOpenConns)
		sqlDB.SetConnMaxLifetime(p.config.MaxLifetime)
		return db, nil
	}
	db, err := open(name)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "3D000" {
		return db, err
	}
	// 建库只在缺库时进行，维护连接不会进入业务事务。
	maintenance, openErr := open(p.config.MaintenanceDatabase)
	if openErr != nil {
		return nil, openErr
	}
	defer closeConnectionPool(maintenance)
	createErr := maintenance.Connection(func(connection *gorm.DB) error {
		// 建库不能在事务中执行，故用独占会话持有advisory lock；同名库只允许一个创建者。
		digest := sha256.Sum256([]byte("core/database/" + name))
		lockKey := int64(binary.BigEndian.Uint64(digest[:8]))
		if err := connection.Exec("SELECT pg_advisory_lock(?)", lockKey).Error; err != nil {
			return err
		}
		defer connection.Exec("SELECT pg_advisory_unlock(?)", lockKey)
		var count int64
		if err := connection.Raw("SELECT COUNT(*) FROM pg_database WHERE datname = ?", name).Scan(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		return connection.Exec("CREATE DATABASE " + quoteIdent(DialectPostgres, name)).Error
	})
	if createErr != nil {
		return nil, createErr
	}

	return open(name)
}

func (p *Postgres) modelDB(model interface{}) (*gorm.DB, string, error) {
	name, err := p.resolveName(model)
	if err != nil {
		return nil, "", err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.txRequested && p.txDBName != "" && p.txDBName != name {
		return nil, "", fmt.Errorf("transaction is bound to another database")
	}
	db, err := p.pool(name)
	if err != nil {
		return nil, "", err
	}
	p.db = db
	if p.txRequested {
		if p.tx == nil {
			p.tx = db.Begin()
			if p.tx.Error != nil {
				err := p.tx.Error
				p.tx = nil
				return nil, "", err
			}
			p.txDBName = name
		}
		db = p.tx
	}
	return db, name, nil
}

func (p *Postgres) ready(model interface{}) (*gorm.DB, string, error) {
	db, name, err := p.modelDB(model)
	if err != nil {
		return nil, name, err
	}
	originalModel := model
	model = storageModel(model)
	if _, view := originalModel.(types.IDBSQL); view {
		return db, name, nil
	}
	if model == nil {
		return db, name, nil
	}
	key, err := p.tableKey(db, name, model)
	if err != nil {
		return nil, name, err
	}
	if _, ok := tableCache.Load(key); ok {
		return db, name, nil
	}
	p.mu.Lock()
	inTx := p.txRequested
	p.mu.Unlock()
	if inTx {
		if _, ok := p.initialized.Load(key); ok {
			return db, name, nil
		}
		return nil, name, fmt.Errorf("model storage is not initialized; call EnsureStorage before transaction")
	}
	if err := p.HasTable(originalModel); err != nil {
		return nil, name, err
	}
	return db, name, nil
}

// GetModelDB 返回已初始化的模型连接，事务期间返回事务连接。
func (p *Postgres) GetModelDB(model interface{}) (interface{}, error) {
	db, _, err := p.ready(model)
	return db, err
}

// GetDB 返回固定数据库连接，不执行结构初始化。
func (p *Postgres) GetDB() (*gorm.DB, error) { db, _, err := p.modelDB(nil); return db, err }

// GetDBName 校验模型分库，不修改配置。
func (p *Postgres) GetDBName(model interface{}) error { _, err := p.resolveName(model); return err }

// GetRunDB 保留兼容访问；正常查询优先使用 GetModelDB。
func (p *Postgres) GetRunDB() interface{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tx != nil {
		return p.tx
	}
	return p.db
}

// Transaction 登记事务，首次模型访问时固定目标库。
func (p *Postgres) Transaction() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.txRequested = true
	return nil
}

// Commit 提交一次，不在结果不确定时重放。
func (p *Postgres) Commit() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	defer p.clearTransaction()
	if p.tx == nil {
		return nil
	}
	return normalizePersistenceError(p.tx.Commit().Error)
}

// Rollback 始终清理本地事务状态。
func (p *Postgres) Rollback() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	defer p.clearTransaction()
	if p.tx == nil {
		return nil
	}
	return p.tx.Rollback().Error
}
func (p *Postgres) clearTransaction() { p.tx = nil; p.txRequested = false; p.txDBName = "" }

func postgresConnectionError(err error) bool {
	if errors.Is(err, driver.ErrBadConn) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var network *net.OpError
	if errors.As(err, &network) {
		return true
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return len(pg.Code) >= 2 && (pg.Code[:2] == "08" || pg.Code == "57P01" || pg.Code == "57P02" || pg.Code == "57P03")
	}
	// database/sql 没有导出的关闭池哨兵；此处只识别标准库固定错误。
	return err != nil && err.Error() == "sql: database is closed"
}

func (p *Postgres) invalidate(name string, db *gorm.DB) {
	p.mu.Lock()
	inTx := p.txRequested
	p.mu.Unlock()
	if !inTx {
		connManager.RemoveIfSame(storageIdentity(p.config, name), db)
	}
}

// Load 执行标准查询；非事务连接故障只重试一次。
func (p *Postgres) Load(item *types.SearchItem, result interface{}) error {
	if item == nil {
		return fmt.Errorf("search is required")
	}
	for attempt := 0; attempt < 2; attempt++ {
		db, name, err := p.ready(item.Model)
		if err != nil {
			return normalizePersistenceError(err)
		}
		if item.IsStatistical {
			err = sum(db, item, result)
		} else {
			err = load(db, item, result)
		}
		if !postgresConnectionError(err) {
			return normalizePersistenceError(err)
		}
		p.mu.Lock()
		inTx := p.txRequested
		p.mu.Unlock()
		if inTx || attempt == 1 {
			return normalizePersistenceError(err)
		}
		p.invalidate(name, db)
	}
	return nil
}

func (p *Postgres) write(data interface{}, operation func(*gorm.DB, interface{}) error) error {
	db, name, err := p.ready(data)
	if err != nil {
		return normalizePersistenceError(err)
	}
	p.mu.Lock()
	inTx := p.txRequested
	p.mu.Unlock()
	if inTx {
		if err := db.SavePoint("core_write").Error; err != nil {
			return normalizePersistenceError(err)
		}
	}
	err = operation(db, data)
	if inTx {
		if err != nil {
			if rollbackErr := db.RollbackTo("core_write").Error; rollbackErr != nil {
				return normalizePersistenceError(errors.Join(err, rollbackErr))
			}
		}
		releaseErr := db.Exec("RELEASE SAVEPOINT core_write").Error
		if err == nil {
			err = releaseErr
		}
	}
	if postgresConnectionError(err) {
		p.invalidate(name, db)
	}
	return normalizePersistenceError(err)
}

// Insert 严格新增，唯一冲突仍返回409，不把普通新增改为幂等忽略。
func (p *Postgres) Insert(data interface{}) error {
	prepareRowHashes(data)
	return p.write(data, func(db *gorm.DB, value interface{}) error {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(value); err != nil {
			return err
		}
		// 批次按列数保守限制绑定参数；GORM负责将多个批次包在同一事务中。
		columns := postgresBatchColumns(stmt.Schema, make(map[*schema.Schema]bool))
		if columns < 1 {
			return fmt.Errorf("model has no persistent columns")
		}
		batchSize := 65535 / columns
		if batchSize < 1 {
			return fmt.Errorf("model has too many persistent columns")
		}
		return createData(db.Session(&gorm.Session{CreateBatchSize: batchSize}), value)
	})
}

// postgresBatchColumns 包含关联模型，避免根模型的较大批次溢出子表参数限制。
func postgresBatchColumns(model *schema.Schema, visited map[*schema.Schema]bool) int {
	if model == nil || visited[model] {
		return 0
	}
	visited[model] = true
	columns := len(model.DBNames)
	for _, relation := range model.Relationships.Relations {
		if relation.Field != nil && relation.Field.IgnoreMigration {
			continue
		}
		if count := postgresBatchColumns(relation.FieldSchema, visited); count > columns {
			columns = count
		}
		if count := postgresBatchColumns(relation.JoinTable, visited); count > columns {
			columns = count
		}
	}
	return columns
}

// Update 更新所有持久化字段，包含零值。
func (p *Postgres) Update(data interface{}) error { return p.write(data, updateData) }

// Delete 按已有 ID/hash 契约删除。
func (p *Postgres) Delete(data interface{}) error { return p.write(data, deleteData) }

// Raw 保留旧原生查询入口，不自动重放任意 SQL。
func (p *Postgres) Raw(query string, data interface{}) error {
	db, _, err := p.ready(data)
	if err != nil {
		return err
	}
	return normalizePersistenceError(db.Raw(query).Scan(data).Error)
}

// Exec 保留旧原生执行入口，遵守活动事务连接。
func (p *Postgres) Exec(query string, data interface{}) error {
	db, name, err := p.modelDB(data)
	if err != nil {
		return err
	}
	err = db.Exec(query, data).Error
	if postgresConnectionError(err) {
		p.invalidate(name, db)
	}
	return normalizePersistenceError(err)
}

var _ types.IDataBase = (*Postgres)(nil)

func prepareRowHashes(data interface{}) {
	if data == nil {
		return
	}
	if row, ok := data.(types.IRowCode); ok {
		row.SetHashcode(row.GetHash())
		return
	}
	v := reflect.ValueOf(data)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
		for i := 0; i < v.Len(); i++ {
			value := v.Index(i)
			if value.CanAddr() && value.Kind() != reflect.Ptr {
				prepareRowHashes(value.Addr().Interface())
			} else {
				prepareRowHashes(value.Interface())
			}
		}
	}
}
