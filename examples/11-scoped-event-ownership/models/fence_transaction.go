// 本文件将Core公共fence合同适配到当前业务SQL事务，行锁持有到真实提交或回滚。
package models

import (
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type fenceTransaction struct{ db *gorm.DB }

func (t fenceTransaction) LockFence(scope, key string) (ownership.FenceState, error) {
	var row ownerRecord
	err := t.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("scope = ? AND key_name = ?", scope, key).Take(&row).Error
	return ownership.FenceState{Scope: row.Scope, Key: row.KeyName, Epoch: row.Epoch, Token: row.Token}, err
}
func (t fenceTransaction) SaveFence(s ownership.FenceState) error {
	return t.db.Model(&ownerRecord{}).Where("scope = ? AND key_name = ?", s.Scope, s.Key).Updates(map[string]any{"epoch": s.Epoch, "token": s.Token}).Error
}
