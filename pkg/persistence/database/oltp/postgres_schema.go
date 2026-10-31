// 本文件只在非业务事务中执行 PostgreSQL 加性结构初始化。
package oltp

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"gorm.io/gorm"
	"os"
	"reflect"
)

func (p *Postgres) tableKey(db *gorm.DB, name string, model interface{}) (TableCacheKey, error) {
	t := reflect.TypeOf(model)
	if t == nil {
		return TableCacheKey{}, fmt.Errorf("model is required")
	}
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return TableCacheKey{}, fmt.Errorf("model must be a struct")
	}
	return TableCacheKey{DBPath: storageIdentity(p.config, name), TableName: modelTableName(db.NamingStrategy, t)}, nil
}

// HasTable 按跨进程锁初始化表、缺失列与索引；任一失败均阻止启动。
func (p *Postgres) HasTable(model interface{}) error {
	p.mu.Lock()
	inTx := p.txRequested
	p.mu.Unlock()
	if inTx {
		return fmt.Errorf("schema initialization is forbidden in business transaction")
	}
	name, err := p.resolveName(model)
	if err != nil {
		return err
	}
	model = storageModel(model)
	db, err := p.pool(name)
	if err != nil {
		return err
	}
	key, err := p.tableKey(db, name, model)
	if err != nil {
		return err
	}
	if os.Getenv("DB_FORCE_MIGRATE") != "true" {
		if cached, ok := tableCache.Load(key); ok {
			p.initialized.Store(key, true)
			if keys, ok := cached.([]TableCacheKey); ok {
				for _, related := range keys {
					p.initialized.Store(related, true)
				}
			}
			return nil
		}
	}
	migrationLock.Lock()
	defer migrationLock.Unlock()
	// 同库不同根模型可能共享关联表，锁必须覆盖整库结构初始化。
	digest := sha256.Sum256([]byte(name + "/core/schema"))
	lockKey := int64(binary.BigEndian.Uint64(digest[:8]))
	initialized := make(map[reflect.Type]bool)
	err = db.Transaction(func(tx *gorm.DB) error {
		// 使用事务级 advisory lock，进程崩溃也不会残留会话锁。
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", lockKey).Error; err != nil {
			return err
		}
		return ensurePostgresSchema(tx, model, initialized)
	})
	if err == nil {
		keys := []TableCacheKey{key}
		p.initialized.Store(key, true)
		for modelType := range initialized {
			if nestedKey, keyErr := p.tableKey(db, name, reflect.New(modelType).Interface()); keyErr == nil {
				keys = append(keys, nestedKey)
				p.initialized.Store(nestedKey, true)
			}
		}
		for _, initializedKey := range keys {
			tableCache.Store(initializedKey, keys)
		}
	}
	return err
}

// ensurePostgresSchema 在同一启动事务内初始化根模型和关联模型，visited 防止循环。
func ensurePostgresSchema(tx *gorm.DB, model interface{}, visited map[reflect.Type]bool) error {
	stmt := &gorm.Statement{DB: tx}
	if err := stmt.Parse(model); err != nil {
		return err
	}
	if visited[stmt.Schema.ModelType] {
		return nil
	}
	visited[stmt.Schema.ModelType] = true
	configurePostgresModelTypes(stmt)
	migrator := tx.Migrator()
	if !migrator.HasTable(model) {
		if err := migrator.CreateTable(model); err != nil {
			return err
		}
	}
	for _, field := range stmt.Schema.Fields {
		if field.DBName == "" || field.IgnoreMigration || field.DataType == "" {
			continue
		}
		if !migrator.HasColumn(model, field.DBName) {
			if err := migrator.AddColumn(model, field.Name); err != nil {
				return err
			}
		}
	}
	for _, index := range stmt.Schema.ParseIndexes() {
		if !migrator.HasIndex(model, index.Name) {
			if err := migrator.CreateIndex(model, index.Name); err != nil {
				return err
			}
		}
	}
	for _, relation := range stmt.Schema.Relationships.Relations {
		if relation.Field != nil && relation.Field.IgnoreMigration {
			continue
		}
		if err := ensurePostgresSchema(tx, reflect.New(relation.FieldSchema.ModelType).Interface(), visited); err != nil {
			return err
		}
		if relation.JoinTable != nil {
			if err := ensurePostgresSchema(tx, reflect.New(relation.JoinTable.ModelType).Interface(), visited); err != nil {
				return err
			}
		}
	}
	return nil
}

var _ types.IDataBase = (*Postgres)(nil)
