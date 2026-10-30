// Package nosql 提供实例隔离、可靠增删改、背压和有界同步的统一 store 门面。
package nosql

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/types"
)

var (
	// ErrWriteBehindNotBound 表示可靠 store 尚未绑定权威远端写回目标。
	ErrWriteBehindNotBound = errors.New("可靠写入尚未绑定 WriteBehind target")
)

// ReliableWriteStore 组合本地 Badger、Group Commit、准入控制和远端写回。
type ReliableWriteStore[T types.IModel] struct {
	db        *PrefixedBadgerDB[T]
	batcher   *BatchCommitter[T]
	admission *WriteAdmissionController
	config    ReliableWriteStoreConfig
	startedAt time.Time
	closeOnce sync.Once
	closeErr  error
	closing   atomic.Bool
	bound     atomic.Bool
}

// NewReliableWriteStore 创建按服务和实例身份隔离的可靠 store 与独立 Admin handle。
func NewReliableWriteStore[T types.IModel](
	identity ServiceIdentity,
	config ReliableWriteStoreConfig,
) (*ReliableWriteStore[T], *ReliableWriteStoreAdmin[T], error) {
	normalized, err := config.normalized(identity)
	if err != nil {
		return nil, nil, err
	}
	db, err := NewSharedBadgerDB[T](normalized.BasePath, normalized.Badger)
	if err != nil {
		return nil, nil, err
	}
	store := &ReliableWriteStore[T]{
		db:        db,
		admission: newWriteAdmissionController(normalized.Admission),
		config:    normalized,
		startedAt: time.Now().UTC(),
	}
	store.batcher = newBatchCommitter(normalized.Batch, db.ApplyWriteOperations)
	if normalized.Badger.WriteBehindFailure != nil {
		db.localSubmit = store.batcher.submitLocalCommit
		db.localConfirmationLimit = func() int {
			if store.batcher.pendingWrites.Load() > 0 {
				return 8
			}
			return dispositionConfirmationMaxItems
		}
	}
	return store, &ReliableWriteStoreAdmin[T]{db: db}, nil
}

// Save 可靠保存新模型或更新现有模型。
func (store *ReliableWriteStore[T]) Save(ctx context.Context, item *T) error {
	return store.submit(ctx, WriteOperation[T]{Type: WriteOperationSave, Item: item})
}

// SaveBatch 按切片顺序可靠保存模型，并返回已提交的连续前缀。
func (store *ReliableWriteStore[T]) SaveBatch(ctx context.Context, items []*T) (BatchWriteResult, error) {
	return store.submitBatch(ctx, writeOperations(WriteOperationSave, items))
}

// Delete 可靠写入模型的删除 tombstone。
func (store *ReliableWriteStore[T]) Delete(ctx context.Context, item *T) error {
	return store.submit(ctx, WriteOperation[T]{Type: WriteOperationDelete, Item: item})
}

// DeleteBatch 按切片顺序可靠写入删除 tombstone，并返回已提交的连续前缀。
func (store *ReliableWriteStore[T]) DeleteBatch(ctx context.Context, items []*T) (BatchWriteResult, error) {
	return store.submitBatch(ctx, writeOperations(WriteOperationDelete, items))
}

// Add 是 Save 的兼容别名。
func (store *ReliableWriteStore[T]) Add(ctx context.Context, item *T) error {
	return store.Save(ctx, item)
}

// AddBatch 是 SaveBatch 的兼容别名。
func (store *ReliableWriteStore[T]) AddBatch(ctx context.Context, items []*T) (BatchWriteResult, error) {
	return store.SaveBatch(ctx, items)
}

// GetLocal 按业务 key 读取本地可见模型，tombstone 不可见。
func (store *ReliableWriteStore[T]) GetLocal(ctx context.Context, key string) (*T, error) {
	if err := store.checkReadable(ctx); err != nil {
		return nil, err
	}
	return store.db.Get(key)
}

// ScanLocal 扫描本地可见模型，tombstone 不进入结果。
func (store *ReliableWriteStore[T]) ScanLocal(ctx context.Context, options LocalScanOptions) ([]*T, error) {
	if err := store.checkReadable(ctx); err != nil {
		return nil, err
	}
	return store.db.Scan(options.Prefix, options.Limit)
}

// UseWriteBehind 绑定唯一权威远端写回目标。
func (store *ReliableWriteStore[T]) UseWriteBehind(target WriteBehindTarget[T]) error {
	if store == nil || store.db == nil || store.closing.Load() {
		return ErrWriteStoreClosed
	}
	if err := store.db.UseWriteBehind(target); err != nil {
		return err
	}
	store.bound.Store(true)
	return nil
}

// UseWriteBehindWithFailureHandler 显式绑定有限预算及应用失败处置入口，保持原写入/生命周期门面。
func (store *ReliableWriteStore[T]) UseWriteBehindWithFailureHandler(target WriteBehindTarget[T], handler WriteBehindFailureHandler[T]) error {
	if store == nil || store.db == nil || store.closing.Load() {
		return ErrWriteStoreClosed
	}
	if err := store.db.UseWriteBehindWithFailureHandler(target, handler); err != nil {
		return err
	}
	store.bound.Store(true)
	return nil
}

// ListWriteBehindFailures 分页读取受应用权限保护的原失败记录，不提供管理页面或匿名API。
func (store *ReliableWriteStore[T]) ListWriteBehindFailures(after string, limit int) ([]WriteBehindFailureRecord[T], string, error) {
	if store == nil || store.db == nil {
		return nil, "", ErrWriteStoreClosed
	}
	return store.db.ListWriteBehindFailures(after, limit)
}

// ApplyWriteBehindDecision 按原身份和版本委托应用恢复决策，绝不重置预算。
func (store *ReliableWriteStore[T]) ApplyWriteBehindDecision(ctx context.Context, d WriteBehindDecision) error {
	if store == nil || store.db == nil {
		return ErrWriteStoreClosed
	}
	return store.db.ApplyWriteBehindDecision(ctx, d)
}

// WriteBehindDispositionMetrics 返回失败处置状态，不将交接计入业务成功。
func (store *ReliableWriteStore[T]) WriteBehindDispositionMetrics() (WriteBehindDispositionMetrics, error) {
	if store == nil || store.db == nil {
		return WriteBehindDispositionMetrics{}, ErrWriteStoreClosed
	}
	return store.db.GetWriteBehindDispositionMetrics()
}

// ForceSyncBatch 最多同步 limit 条 pending，实际单轮数量不会超过 Badger.SyncBatchSize。
func (store *ReliableWriteStore[T]) ForceSyncBatch(ctx context.Context, limit int) (ForceSyncResult, error) {
	if err := store.checkSyncable(ctx); err != nil {
		return ForceSyncResult{}, err
	}
	return store.db.ForceSyncBatch(ctx, limit)
}

// ForceSyncAll 持续执行有界同步，直到无 pending 或出现错误。
func (store *ReliableWriteStore[T]) ForceSyncAll(ctx context.Context) (ForceSyncResult, error) {
	if err := store.checkSyncable(ctx); err != nil {
		return ForceSyncResult{}, err
	}
	return store.db.ForceSyncAllContext(ctx)
}

// Metrics 返回可靠 store 的无锁或只读指标快照。
func (store *ReliableWriteStore[T]) Metrics() ReliableWriteMetrics {
	if store == nil || store.db == nil {
		return ReliableWriteMetrics{}
	}
	size := store.db.StorageSize()
	batch := store.batcher.Metrics()
	syncMetrics := store.db.GetSyncMetrics()
	// 前台排队直接复用已采集的逐操作累计，不把未采集字段伪装为零。
	syncMetrics.ForegroundLocal.QueueWait = batch.TotalQueueWaitDuration
	return ReliableWriteMetrics{
		StartedAt:       store.startedAt,
		Pending:         store.db.GetCachedPendingSyncCount(),
		BadgerLSMBytes:  size.LSMBytes,
		BadgerVLogBytes: size.VLogBytes,
		Batch:           batch,
		Admission:       store.admission.Metrics(),
		Sync:            syncMetrics,
	}
}

// Close 停止接收写入、排空已接收本地提交并关闭当前 prefix；不会强制访问远端。
func (store *ReliableWriteStore[T]) Close(ctx context.Context) error {
	if store == nil {
		return nil
	}
	store.closeOnce.Do(func() {
		store.closing.Store(true)
		if ctx == nil {
			ctx = context.Background()
		}
		var batchErr error
		if store.db.localSubmit != nil {
			batchErr = store.batcher.drainAcceptedWrites(ctx)
		} else {
			batchErr = store.batcher.Close(ctx)
		}
		timeout := store.closeTimeout(ctx)
		if batchErr != nil && ctx.Err() != nil {
			if store.db.localSubmit != nil {
				// 超时仍终止后台与提交面准入，已接受结果由原worker完成；不新增遗留队列。
				dbErr := store.db.CloseWithTimeout(time.Nanosecond, time.Nanosecond)
				store.closeErr = errors.Join(batchErr, dbErr, store.batcher.Close(ctx))
			} else {
				store.closeErr = batchErr
			}
			return
		}
		dbErr := store.db.CloseWithTimeout(timeout, timeout)
		if store.db.localSubmit != nil {
			batchErr = errors.Join(batchErr, store.batcher.Close(ctx))
		}
		store.closeErr = errors.Join(batchErr, dbErr)
	})
	return store.closeErr
}

func (store *ReliableWriteStore[T]) submit(ctx context.Context, operation WriteOperation[T]) error {
	_, err := store.submitBatch(ctx, []WriteOperation[T]{operation})
	return err
}

func (store *ReliableWriteStore[T]) submitBatch(
	ctx context.Context,
	operations []WriteOperation[T],
) (BatchWriteResult, error) {
	if store == nil || store.db == nil || store.closing.Load() {
		return BatchWriteResult{}, ErrWriteStoreClosed
	}
	if !store.bound.Load() {
		return BatchWriteResult{}, ErrWriteBehindNotBound
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return BatchWriteResult{}, err
	}
	size := store.db.StorageSize()
	release, err := store.admission.Acquire(
		ctx,
		store.db.GetCachedPendingSyncCount(),
		size.LSMBytes+size.VLogBytes,
		time.Now(),
	)
	if err != nil {
		return BatchWriteResult{}, err
	}
	defer release()
	return store.batcher.SubmitBatch(ctx, operations)
}

func (store *ReliableWriteStore[T]) checkReadable(ctx context.Context) error {
	if store == nil || store.db == nil || store.closing.Load() {
		return ErrWriteStoreClosed
	}
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func (store *ReliableWriteStore[T]) checkSyncable(ctx context.Context) error {
	if err := store.checkReadable(ctx); err != nil {
		return err
	}
	if !store.bound.Load() {
		return ErrWriteBehindNotBound
	}
	return nil
}

func (store *ReliableWriteStore[T]) closeTimeout(ctx context.Context) time.Duration {
	timeout := store.config.CloseTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < timeout {
			timeout = remaining
		}
	}
	if timeout <= 0 {
		return time.Nanosecond
	}
	return timeout
}

func writeOperations[T types.IModel](operationType WriteOperationType, items []*T) []WriteOperation[T] {
	operations := make([]WriteOperation[T], len(items))
	for index, item := range items {
		operations[index] = WriteOperation[T]{Type: operationType, Item: item}
	}
	return operations
}
