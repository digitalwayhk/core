// Package models 是演示存储组合根，启动自动建库表；handler只执行事务，不承担DDL。
package models

import (
	"context"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/contract"
	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Store 保存实例注入的技术持久化访问，不使用包级全局store。
type Store struct {
	action *oltp.MySQL
	db     *gorm.DB
}

// NewStore 使用调用方显式提供的共享MySQL；不猜测本地或生产DSN。
func NewStore(cfg *oltp.Config) *Store {
	copy := *cfg
	copy.IsLog = false
	return &Store{action: oltp.NewMySQL(&copy)}
}

// EnsureStorage 仅由服务Initialize调用，通过Core数据访问触发自动建库表和补列。
func (s *Store) EnsureStorage() error {
	var owners []*ownerRecord
	var applied []*appliedEvent
	if err := s.action.Load(&pt.SearchItem{Page: 1, Size: 1, Model: &ownerRecord{}}, &owners); err != nil {
		return err
	}
	if err := s.action.Load(&pt.SearchItem{Page: 1, Size: 1, Model: &appliedEvent{}}, &applied); err != nil {
		return err
	}
	db, err := s.action.GetDB()
	if err != nil {
		return err
	}
	s.db = db
	for _, key := range contract.Manifest().Keys {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&ownerRecord{Scope: contract.Scope, KeyName: key}).Error; err != nil {
			return err
		}
	}
	return nil
}

// Activate 等待旧SQL真实退出后，在同一事务激活新epoch；Redis租约不能代替此锁。
func (s *Store) Activate(ctx context.Context, c ownership.Credential) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return ownership.ActivateFence(fenceTransaction{tx}, c) })
}

// Apply 同事务验证fence、幂等记录和更新计数；只有事务返回成功才允许上层ACK。
func (s *Store) Apply(ctx context.Context, c ownership.Credential, rows []*event.Envelope) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ownership.CheckFence(fenceTransaction{tx}, c); err != nil {
			return err
		}
		var n int64
		for _, e := range rows {
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&appliedEvent{ID: e.ID, Scope: c.Scope, KeyName: c.Key})
			if result.Error != nil {
				return result.Error
			}
			n += result.RowsAffected
		}
		return tx.Model(&ownerRecord{}).Where("scope = ? AND key_name = ?", c.Scope, c.Key).UpdateColumn("applied", gorm.Expr("applied + ?", n)).Error
	})
}

// Count 只读已提交快照；有效owner由正式Runtime.Status和路由再次校验。
func (s *Store) Count(ctx context.Context, key string) (int64, error) {
	var row ownerRecord
	err := s.db.WithContext(ctx).Where("scope = ? AND key_name = ?", contract.Scope, key).Take(&row).Error
	return row.Applied, err
}

// ReadFenced 将查询也绑定SQL权威epoch，Redis恢复回退不能让旧缓存冒充有效结果。
func (s *Store) ReadFenced(ctx context.Context, c ownership.Credential) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ownership.CheckFence(fenceTransaction{tx}, c); err != nil {
			return err
		}
		var row ownerRecord
		if err := tx.Where("scope = ? AND key_name = ?", c.Scope, c.Key).Take(&row).Error; err != nil {
			return err
		}
		count = row.Applied
		return nil
	})
	return count, err
}

// Close 由ServiceContext资源生命周期关闭本实例的SQL连接池。
func (s *Store) Close(context.Context) error {
	if s.db == nil {
		return nil
	}
	db, err := s.db.DB()
	if err != nil {
		return err
	}
	return db.Close()
}
