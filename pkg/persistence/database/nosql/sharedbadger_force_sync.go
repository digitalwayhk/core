// Package nosql 提供 PrefixedBadgerDB 带 context 和 limit 的有界同步入口。
package nosql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

var (
	// ErrInvalidSyncLimit 表示有界同步 limit 不是正数。
	ErrInvalidSyncLimit = errors.New("有界同步 limit 无效")
	// ErrWriteBehindNoProgress 表示仍有 pending 但 target 未确认任何 key。
	ErrWriteBehindNoProgress = errors.New("WriteBehind 同步未取得进展")
)

// ForceSyncResult 描述一次或多次有界同步确认数和返回时剩余数。
type ForceSyncResult struct {
	Confirmed int
	Remaining int
}

// ForceSyncBatch 最多处理 limit 条 pending，但不会超过配置的 SyncBatchSize 硬上限。
func (p *PrefixedBadgerDB[T]) ForceSyncBatch(ctx context.Context, limit int) (ForceSyncResult, error) {
	return p.forceSyncBatch(ctx, limit, true)
}

func (p *PrefixedBadgerDB[T]) forceSyncBatch(ctx context.Context, limit int, rebuild bool) (ForceSyncResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ForceSyncResult{Remaining: p.GetCachedPendingSyncCount()}, err
	}
	if limit <= 0 {
		return ForceSyncResult{Remaining: p.GetCachedPendingSyncCount()}, ErrInvalidSyncLimit
	}
	if p == nil || p.IsClosed() {
		return ForceSyncResult{}, errors.New("BadgerDB 实例已关闭")
	}
	maxLimit := p.manager.config.SyncBatchSize
	if maxLimit <= 0 {
		return ForceSyncResult{Remaining: p.GetCachedPendingSyncCount()}, ErrInvalidSyncLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	p.syncLock.RLock()
	syncEnabled := p.syncDB && (p.syncList != nil || p.syncTarget != nil)
	p.syncLock.RUnlock()
	if !syncEnabled {
		return ForceSyncResult{Remaining: p.GetCachedPendingSyncCount()}, errors.New("同步功能未启用")
	}
	if rebuild {
		if _, err := p.rebuildSyncQueue(); err != nil {
			return ForceSyncResult{Remaining: p.GetCachedPendingSyncCount()}, err
		}
		if err := ctx.Err(); err != nil {
			return ForceSyncResult{Remaining: p.GetCachedPendingSyncCount()}, err
		}
	}
	confirmed, err := p.processSyncQueueContext(ctx, limit)
	return ForceSyncResult{
		Confirmed: confirmed,
		Remaining: p.GetCachedPendingSyncCount(),
	}, err
}

// ForceSyncAllContext 使用配置批次大小持续同步，遇到错误、取消或无进展立即停止。
func (p *PrefixedBadgerDB[T]) ForceSyncAllContext(ctx context.Context) (ForceSyncResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ForceSyncResult{Remaining: p.GetCachedPendingSyncCount()}, err
	}
	if p == nil || p.IsClosed() {
		return ForceSyncResult{}, errors.New("BadgerDB 实例已关闭")
	}
	if _, err := p.rebuildSyncQueue(); err != nil {
		return ForceSyncResult{Remaining: p.GetCachedPendingSyncCount()}, err
	}
	limit := p.manager.config.SyncBatchSize
	if limit <= 0 {
		return ForceSyncResult{Remaining: p.GetCachedPendingSyncCount()}, ErrInvalidSyncLimit
	}
	totalConfirmed := 0
	for {
		remaining := p.GetCachedPendingSyncCount()
		if remaining == 0 {
			return ForceSyncResult{Confirmed: totalConfirmed}, nil
		}
		result, err := p.forceSyncBatch(ctx, limit, false)
		totalConfirmed += result.Confirmed
		combined := ForceSyncResult{Confirmed: totalConfirmed, Remaining: result.Remaining}
		if err != nil {
			return combined, err
		}
		if result.Confirmed == 0 && result.Remaining > 0 {
			return combined, fmt.Errorf("%w [prefix=%s, remaining=%d]", ErrWriteBehindNoProgress, p.prefix, result.Remaining)
		}
	}
}

func (p *PrefixedBadgerDB[T]) processSyncQueueContext(ctx context.Context, limit int) (synced int, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if limit <= 0 {
		return 0, ErrInvalidSyncLimit
	}
	// 同一实例的 worker、手动同步和关闭同步必须串行，避免重复 target 事务。
	p.syncExecMu.Lock()
	defer p.syncExecMu.Unlock()
	started := time.Now()
	defer func() { p.recordSyncMetrics(synced, err, time.Since(started)) }()

	unsyncedItems, err := p.getUnsyncedBatch(limit)
	if err != nil {
		return 0, fmt.Errorf("获取未同步数据失败: %w", err)
	}
	if len(unsyncedItems) == 0 {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	logx.Infof("开始同步 [prefix=%s, 数量: %d]", p.prefix, len(unsyncedItems))
	confirmedKeys, err := p.syncBatchContext(ctx, unsyncedItems)
	successCount := len(confirmedKeys)
	if err != nil {
		logx.Errorf("批量同步失败 [prefix=%s, 已确认=%d]: %v", p.prefix, successCount, err)
		return successCount, err
	}
	if successCount == 0 {
		logx.Errorf("同步未确认任何数据 [prefix=%s, 待同步: %d]", p.prefix, len(unsyncedItems))
		return 0, nil
	}
	if successCount < len(unsyncedItems) {
		logx.Infof("同步部分完成 [prefix=%s, 成功: %d/%d]", p.prefix, successCount, len(unsyncedItems))
		return successCount, nil
	}
	logx.Infof("同步成功 [prefix=%s, 成功: %d/%d]", p.prefix, successCount, len(unsyncedItems))
	return successCount, nil
}
