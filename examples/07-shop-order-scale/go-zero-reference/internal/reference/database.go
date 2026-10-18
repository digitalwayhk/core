// 本文件直接使用 GORM 访问原有 schema；不导入 IDataAction 或 Core 事务封装。
package reference

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func openDatabase(role, path string) (*gorm.DB, error) {
	c := &gorm.Config{NamingStrategy: schema.NamingStrategy{SingularTable: true}, SkipDefaultTransaction: true, DisableNestedTransaction: true, Logger: logger.Default.LogMode(logger.Silent)}
	var db *gorm.DB
	var err error
	if role == "supplier" {
		db, err = gorm.Open(sqlite.Open(path), c)
	} else {
		dsn := gomysql.NewConfig()
		dsn.User = os.Getenv("SHOP_ORDER_REMOTE_MYSQL_USER")
		dsn.Passwd = os.Getenv("SHOP_ORDER_REMOTE_MYSQL_PASSWORD")
		dsn.Net = "tcp"
		dsn.Addr = os.Getenv("SHOP_ORDER_REMOTE_MYSQL_HOST") + ":" + os.Getenv("SHOP_ORDER_REMOTE_MYSQL_PORT")
		dsn.DBName = os.Getenv("SHOP_ORDER_REMOTE_MYSQL_DATABASE")
		dsn.ParseTime = true
		dsn.Loc = time.Local
		dsn.Params = map[string]string{"charset": "utf8mb4"}
		db, err = gorm.Open(mysql.Open(dsn.FormatDSN()), c)
	}
	if err != nil {
		return nil, err
	}
	pool, err := db.DB()
	if err != nil {
		return nil, err
	}
	if role == "supplier" {
		pool.SetMaxOpenConns(2)
		pool.SetMaxIdleConns(1)
		pool.SetConnMaxLifetime(5 * time.Minute)
		pool.SetConnMaxIdleTime(2 * time.Minute)
		for _, pragma := range []string{"PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=30000", "PRAGMA synchronous=NORMAL", "PRAGMA cache_size=2000", "PRAGMA temp_store=MEMORY", "PRAGMA mmap_size=268435456"} {
			if e := db.Exec(pragma).Error; e != nil {
				_ = pool.Close()
				return nil, e
			}
		}
	} else {
		pool.SetMaxOpenConns(envInt("SHOP_ORDER_REMOTE_MYSQL_MAX_OPEN", 20))
		pool.SetMaxIdleConns(envInt("SHOP_ORDER_REMOTE_MYSQL_MAX_IDLE", 5))
		pool.SetConnMaxLifetime(30 * time.Minute)
	}
	tables := []string{"order", "outbox_record", "order_rule"}
	if role == "supplier" {
		tables = []string{"supplier", "product"}
	}
	for _, table := range tables {
		if !db.Migrator().HasTable(table) {
			_ = pool.Close()
			return nil, fmt.Errorf("计时外必须准备表 %s", table)
		}
	}
	return db, nil
}

func hashCodes(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "-") + "-"))
	return hex.EncodeToString(h[:16])
}

func (a *application) syncBatch(ctx context.Context, items []*Order) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		hashes := make([]string, 0, len(items))
		for _, o := range items {
			o.Hashcode = hashCodes(o.RequestID, fmt.Sprint(o.UserID))
			hashes = append(hashes, o.Hashcode)
		}
		var existing []*Order
		if e := tx.Where("hashcode IN ?", hashes).Find(&existing).Error; e != nil {
			return e
		}
		byHash := map[string]*Order{}
		for _, o := range existing {
			byHash[o.Hashcode] = o
		}
		missing := make([]*Order, 0, len(items))
		stored := make([]*Order, 0, len(items))
		seen := map[string]bool{}
		now := time.Now().UTC()
		for _, o := range items {
			if old := byHash[o.Hashcode]; old != nil {
				if old.RequestFingerprint != o.RequestFingerprint {
					return errors.New("幂等键已用于不同订单请求")
				}
				stored = append(stored, old)
				continue
			}
			if seen[o.Hashcode] {
				continue
			}
			seen[o.Hashcode] = true
			o.OrderStatus = "synced"
			o.SyncedAt = &now
			missing = append(missing, o)
			stored = append(stored, o)
		}
		if len(missing) > 0 {
			if e := tx.Create(&missing).Error; e != nil {
				return e
			}
		}
		outboxes := make([]*OutboxRecord, 0, len(stored))
		eventHashes := make([]string, 0, len(stored))
		for _, o := range stored {
			outbox, e := a.newOutbox(o)
			if e != nil {
				return e
			}
			outboxes = append(outboxes, outbox)
			eventHashes = append(eventHashes, outbox.Hashcode)
		}
		var old []*OutboxRecord
		if e := tx.Where("hashcode IN ?", eventHashes).Find(&old).Error; e != nil {
			return e
		}
		present := map[string]bool{}
		for _, o := range old {
			present[o.Hashcode] = true
		}
		fresh := make([]*OutboxRecord, 0, len(outboxes))
		for _, o := range outboxes {
			if !present[o.Hashcode] {
				fresh = append(fresh, o)
			}
		}
		if len(fresh) > 0 {
			return tx.Create(&fresh).Error
		}
		return nil
	})
}

func (a *application) markPublished(ctx context.Context, items []*OutboxRecord) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(items))
	for _, o := range items {
		ids = append(ids, o.ID)
	}
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var found []uint
		if e := tx.Model(&OutboxRecord{}).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Order("id").Pluck("id", &found).Error; e != nil {
			return e
		}
		if len(found) != len(ids) {
			return errors.New("Outbox 事件不存在")
		}
		now := time.Now().UTC()
		return tx.Model(&OutboxRecord{}).Where("id IN ? AND published = ?", ids, false).Updates(map[string]interface{}{"published": true, "published_at": now, "updated_at": now}).Error
	})
}
