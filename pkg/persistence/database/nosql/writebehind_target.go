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
// SyncBatch 返回错误时仍可携带 ConfirmedKeys，框架会先 ACK 已成功 key 再传播错误。
// 两种模式均不直接按RetryKeys/DeadKeys重试或删除；显式失败处置模式将实际未确认项交给应用handler。
type WriteBehindResult struct {
	ConfirmedKeys []string
	RetryKeys     []string
	DeadKeys      []string
}
