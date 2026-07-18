// Package nosql 提供 PrefixedBadgerDB 本地可靠写后的可插拔远端同步目标。
package nosql

import (
	"context"

	"github.com/digitalwayhk/core/pkg/persistence/types"
)

// WriteBehindTarget 定义 PrefixedBadgerDB pending 批次的远端汇合目标。
// 实现方只表达业务同步语义，Badger pending、ACK、重试触发和关闭恢复由 PrefixedBadgerDB 负责。
type WriteBehindTarget[T types.IModel] interface {
	SyncBatch(ctx context.Context, items []*SyncQueueItem[T]) (*WriteBehindResult, error)
}

// WriteBehindResult 描述一次远端同步后每类 key 的处理结果。
// 第一阶段只对 ConfirmedKeys 执行 ACK；RetryKeys 和 DeadKeys 保留给生产级增强阶段。
type WriteBehindResult struct {
	ConfirmedKeys []string
	RetryKeys     []string
	DeadKeys      []string
}
