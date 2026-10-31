// 本文件为不可变事件或流水记录提供同事务批量发布确认，不推断应用的业务状态。
package message

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// PublishedFields 显式指定应用已确认的发布状态及可选时间字段，使用 Go 字段名。
// 仅适用于每行对应一个不可变事件、布尔发布状态且无需逐行业务更新 Hook 的模型。
// 可变业务行的版本比较、目标子状态及完成度由应用另行适配，不能套用本工具。
type PublishedFields struct {
	Published   string
	PublishedAt string
	UpdatedAt   string
}

// ConfirmPublishedWith 在当前 SQL 事务中锁定并核对完整主键集合，再批量更新明确指定的字段。
// 空批次无需连接；零/缺失 ID 拒绝，已确认幂等成功。任何错误必须使调用方整事务回滚。
func ConfirmPublishedWith(ctx context.Context, action types.IDataAction, model interface{}, ids []uint, fields PublishedFields) error {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[uint]bool, len(ids))
	unique := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			return errors.New("Outbox 主键不能为空")
		}
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i] < unique[j] })
	db, err := transactionDB(ctx, action, model)
	if err != nil {
		return err
	}
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(model); err != nil {
		return err
	}
	s := statement.Schema
	if len(s.PrimaryFields) != 1 || s.PrioritizedPrimaryField == nil {
		return errors.New("发布确认需要单一主键")
	}
	primary := s.PrioritizedPrimaryField.DBName
	published := s.LookUpField(fields.Published)
	if published == nil || published.DataType != schema.Bool {
		return errors.New("发布确认需要显式布尔状态字段")
	}
	updates := map[string]interface{}{published.DBName: true}
	now := time.Now().UTC()
	for _, name := range []string{fields.PublishedAt, fields.UpdatedAt} {
		if name == "" {
			continue
		}
		field := s.LookUpField(name)
		if field == nil || field.DataType != schema.Time {
			return errors.New("发布确认时间字段无效")
		}
		updates[field.DBName] = now
	}
	const batchSize = 1000
	for offset := 0; offset < len(unique); offset += batchSize {
		batch := unique[offset:min(offset+batchSize, len(unique))]
		var found []uint
		err := db.Model(model).Clauses(clause.Locking{Strength: "UPDATE"}, clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: primary}}}}).
			Where(map[string]interface{}{primary: batch}).Pluck(primary, &found).Error
		if err != nil {
			return err
		}
		if len(found) != len(batch) {
			return errors.New("Outbox 事件不存在")
		}
	}
	for offset := 0; offset < len(unique); offset += batchSize {
		batch := unique[offset:min(offset+batchSize, len(unique))]
		if err := db.Model(model).Where(map[string]interface{}{primary: batch, published.DBName: false}).UpdateColumns(updates).Error; err != nil {
			return err
		}
	}
	return nil
}
