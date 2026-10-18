// 本文件在调用方业务事务内复用已有消费依据，不创建独立 Inbox 表。
package message

import (
	"context"
	"errors"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/types"
)

// ConsumeInboxWith 在当前事务内查询消费依据、执行业务并插入依据；事务提交后才能 ACK。
// receipt 可以是独立 Inbox 或既有业务流水，identity 必须由应用提供完整幂等条件，
// 并由数据库唯一约束防止跨副本重复；冲突必须向外返回以回滚整个事务，再由 MQ 重投。
// apply 仅可包含该事务内的业务操作；HTTP/WebSocket 等外部副作用不在原子保证范围。
func ConsumeInboxWith[T any](ctx context.Context, action types.IDataAction, receipt *T, identity *types.SearchItem, apply func(types.IDataAction) error) error {
	if receipt == nil || identity == nil || len(identity.WhereList) == 0 || apply == nil {
		return errors.New("消费事务需要记录、完整幂等条件和业务操作")
	}
	for _, condition := range identity.WhereList {
		if condition == nil || strings.TrimSpace(condition.Column) == "" {
			return errors.New("消费幂等条件不能为空")
		}
	}
	if _, err := transactionDB(ctx, action, receipt); err != nil {
		return err
	}
	query := *identity
	query.Model, query.Page, query.Size, query.SkipCount = receipt, 1, 1, true
	var existing []*T
	if err := action.Load(&query, &existing); err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	if err := apply(action); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return action.Insert(receipt)
}
