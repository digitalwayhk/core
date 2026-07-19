// Package nosql 提供 ReliableWriteStore 跨请求可靠 Group Commit 能力。
package nosql

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/types"
)

var (
	// ErrWriteStoreClosed 表示可靠 store 或其 batcher 已停止接收新写入。
	ErrWriteStoreClosed = errors.New("可靠写入存储已关闭")
	// ErrInvalidBatchCommitResult 表示 commit 返回的前缀数量不符合批次契约。
	ErrInvalidBatchCommitResult = errors.New("可靠批提交结果无效")
)

type batchCommitRequest[T types.IModel] struct {
	operation WriteOperation[T]
	result    chan error
}

// BatchCommitMetrics 描述 Group Commit 的提交、失败、批量和排队指标。
type BatchCommitMetrics struct {
	Submitted             uint64
	Committed             uint64
	Batches               uint64
	FailedBatches         uint64
	MaxBatchSize          int
	MaxQueueDepth         int
	TotalCommitDuration   time.Duration
	AverageCommitDuration time.Duration
}

type batchCommitMetricState struct {
	submitted     atomic.Uint64
	committed     atomic.Uint64
	batches       atomic.Uint64
	failedBatches atomic.Uint64
	maxBatchSize  atomic.Int64
	maxQueueDepth atomic.Int64
	totalNanos    atomic.Int64
}

// BatchCommitter 把短窗口内的并发 Save/Delete 请求合并为有序本地事务。
type BatchCommitter[T types.IModel] struct {
	config     BatchCommitConfig
	commit     func([]WriteOperation[T]) (BatchWriteResult, error)
	requests   chan batchCommitRequest[T]
	closing    chan struct{}
	done       chan struct{}
	stateMu    sync.RWMutex
	closed     bool
	submitters sync.WaitGroup
	closeOnce  sync.Once
	errMu      sync.Mutex
	firstErr   error
	metrics    batchCommitMetricState
}

func newBatchCommitter[T types.IModel](
	config BatchCommitConfig,
	commit func([]WriteOperation[T]) (BatchWriteResult, error),
) *BatchCommitter[T] {
	if config.MaxBatch <= 0 {
		config.MaxBatch = 1
	}
	if config.CollectWindow <= 0 {
		config.CollectWindow = time.Millisecond
	}
	if config.QueueCapacity < config.MaxBatch {
		config.QueueCapacity = config.MaxBatch
	}
	batcher := &BatchCommitter[T]{
		config:   config,
		commit:   commit,
		requests: make(chan batchCommitRequest[T], config.QueueCapacity),
		closing:  make(chan struct{}),
		done:     make(chan struct{}),
	}
	go batcher.run()
	return batcher
}

// Submit 接受一个可靠操作，并等待其所在本地事务得到明确结果。
func (b *BatchCommitter[T]) Submit(ctx context.Context, operation WriteOperation[T]) error {
	if b == nil {
		return ErrWriteStoreClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	request := batchCommitRequest[T]{operation: operation, result: make(chan error, 1)}
	b.stateMu.RLock()
	if b.closed {
		b.stateMu.RUnlock()
		return ErrWriteStoreClosed
	}
	b.submitters.Add(1)
	b.stateMu.RUnlock()
	select {
	case b.requests <- request:
		b.metrics.submitted.Add(1)
		updateReliableAtomicMax(&b.metrics.maxQueueDepth, int64(len(b.requests)))
		b.submitters.Done()
	case <-ctx.Done():
		b.submitters.Done()
		return ctx.Err()
	case <-b.closing:
		b.submitters.Done()
		return ErrWriteStoreClosed
	}
	// 请求进入 channel 后必须等待提交结果，不能把已接受写入伪装为取消。
	return <-request.result
}

// Close 停止接收新请求，排空所有已接受请求，并返回首个 commit 错误。
func (b *BatchCommitter[T]) Close(ctx context.Context) error {
	if b == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	b.closeOnce.Do(func() {
		b.stateMu.Lock()
		b.closed = true
		close(b.closing)
		b.stateMu.Unlock()
		b.submitters.Wait()
		close(b.requests)
	})
	select {
	case <-b.done:
		b.errMu.Lock()
		defer b.errMu.Unlock()
		return b.firstErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Metrics 返回 Group Commit 的无锁指标快照。
func (b *BatchCommitter[T]) Metrics() BatchCommitMetrics {
	if b == nil {
		return BatchCommitMetrics{}
	}
	batches := b.metrics.batches.Load()
	total := time.Duration(b.metrics.totalNanos.Load())
	average := time.Duration(0)
	if batches > 0 {
		average = total / time.Duration(batches)
	}
	return BatchCommitMetrics{
		Submitted:             b.metrics.submitted.Load(),
		Committed:             b.metrics.committed.Load(),
		Batches:               batches,
		FailedBatches:         b.metrics.failedBatches.Load(),
		MaxBatchSize:          int(b.metrics.maxBatchSize.Load()),
		MaxQueueDepth:         int(b.metrics.maxQueueDepth.Load()),
		TotalCommitDuration:   total,
		AverageCommitDuration: average,
	}
}

func (b *BatchCommitter[T]) run() {
	defer close(b.done)
	for first := range b.requests {
		batch := []batchCommitRequest[T]{first}
		channelClosed := false
		if b.config.MaxBatch > 1 {
			timer := time.NewTimer(b.config.CollectWindow)
		collect:
			for len(batch) < b.config.MaxBatch {
				select {
				case request, ok := <-b.requests:
					if !ok {
						channelClosed = true
						break collect
					}
					batch = append(batch, request)
				case <-timer.C:
					break collect
				}
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		b.finishBatch(batch)
		if channelClosed {
			return
		}
	}
}

func (b *BatchCommitter[T]) finishBatch(batch []batchCommitRequest[T]) {
	started := time.Now()
	result, err := b.commitSafely(batch)
	duration := time.Since(started)
	b.metrics.batches.Add(1)
	b.metrics.totalNanos.Add(duration.Nanoseconds())
	updateReliableAtomicMax(&b.metrics.maxBatchSize, int64(len(batch)))
	if result.Committed < 0 || result.Committed > len(batch) || (err == nil && result.Committed != len(batch)) {
		err = fmt.Errorf("%w: committed=%d batch=%d", ErrInvalidBatchCommitResult, result.Committed, len(batch))
		result.Committed = 0
	}
	if err != nil {
		b.metrics.failedBatches.Add(1)
		b.errMu.Lock()
		if b.firstErr == nil {
			b.firstErr = err
		}
		b.errMu.Unlock()
	}
	b.metrics.committed.Add(uint64(result.Committed))
	for index, request := range batch {
		if index < result.Committed {
			request.result <- nil
			continue
		}
		request.result <- err
	}
}

func (b *BatchCommitter[T]) commitSafely(batch []batchCommitRequest[T]) (result BatchWriteResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = BatchWriteResult{}
			err = fmt.Errorf("可靠批提交 panic: %v", recovered)
		}
	}()
	if b.commit == nil {
		return BatchWriteResult{}, errors.New("可靠批提交函数未配置")
	}
	operations := make([]WriteOperation[T], 0, len(batch))
	for _, request := range batch {
		operations = append(operations, request.operation)
	}
	return b.commit(operations)
}

func updateReliableAtomicMax(value *atomic.Int64, candidate int64) {
	for current := value.Load(); candidate > current; current = value.Load() {
		if value.CompareAndSwap(current, candidate) {
			return
		}
	}
}
