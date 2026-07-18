// Package transaction 提供 07 订单本地可靠写入的 Group Commit 能力。
package transaction

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

var errOrderBatcherClosed = errors.New("订单批量提交器已关闭")

const orderCommitBacklogThreshold = 16

type orderBatchRequest struct {
	order  *Order
	result chan error
}

// orderBatcher 将短时间窗口内的订单合并成一次 Badger 事务。
type orderBatcher struct {
	maxBatch int
	wait     time.Duration
	commit   func([]*Order) error
	requests chan orderBatchRequest
	done     chan struct{}
	closing  chan struct{}

	stateMu    sync.RWMutex
	closed     bool
	submitters sync.WaitGroup
	once       sync.Once
	errMu      sync.Mutex
	firstErr   error
	metrics    orderBatcherMetrics
}

type orderBatcherMetrics struct {
	submittedOrders            atomic.Uint64
	committedOrders            atomic.Uint64
	commitBatches              atomic.Uint64
	thresholdImmediateBatches  atomic.Uint64
	singletonAggregatedBatches atomic.Uint64
	aggregatedBatches          atomic.Uint64
	failedBatches              atomic.Uint64
	maxBatchSize               atomic.Int64
	maxQueueDepth              atomic.Int64
	totalCommitNanos           atomic.Int64
}

// OrderBatcherSnapshot 是 Group Commit 的运行指标快照。
type OrderBatcherSnapshot struct {
	SubmittedOrders            uint64
	CommittedOrders            uint64
	CommitBatches              uint64
	ThresholdImmediateBatches  uint64
	SingletonAggregatedBatches uint64
	AggregatedBatches          uint64
	FailedBatches              uint64
	MaxBatchSize               int
	MaxQueueDepth              int
	TotalCommitDuration        time.Duration
	AverageCommitDuration      time.Duration
}

func newOrderBatcher(maxBatch int, wait time.Duration, commit func([]*Order) error) *orderBatcher {
	if maxBatch <= 0 {
		maxBatch = 1
	}
	if wait <= 0 {
		wait = time.Millisecond
	}
	batcher := &orderBatcher{
		maxBatch: maxBatch,
		wait:     wait,
		commit:   commit,
		requests: make(chan orderBatchRequest, maxBatch*8),
		done:     make(chan struct{}),
		closing:  make(chan struct{}),
	}
	go batcher.run()
	return batcher
}

func (b *orderBatcher) Submit(order *Order) error {
	if b == nil || order == nil {
		return errors.New("订单不能为空")
	}
	request := orderBatchRequest{order: order, result: make(chan error, 1)}
	b.stateMu.RLock()
	if b.closed {
		b.stateMu.RUnlock()
		return errOrderBatcherClosed
	}
	b.submitters.Add(1)
	b.stateMu.RUnlock()
	select {
	case b.requests <- request:
		b.metrics.submittedOrders.Add(1)
		updateAtomicMax(&b.metrics.maxQueueDepth, int64(len(b.requests)))
		b.submitters.Done()
	case <-b.closing:
		b.submitters.Done()
		return errOrderBatcherClosed
	}
	return <-request.result
}

func (b *orderBatcher) Close() error {
	if b == nil {
		return nil
	}
	b.once.Do(func() {
		b.stateMu.Lock()
		b.closed = true
		close(b.closing)
		b.stateMu.Unlock()
		b.submitters.Wait()
		close(b.requests)
	})
	<-b.done
	b.errMu.Lock()
	defer b.errMu.Unlock()
	return b.firstErr
}

func (b *orderBatcher) run() {
	defer close(b.done)
	for first := range b.requests {
		batch := []orderBatchRequest{first}
		runtime.Gosched()
		if len(b.requests) < orderCommitBacklogThreshold {
			b.finishBatch(batch, true)
			continue
		}
		timer := time.NewTimer(b.wait)
		channelClosed := false
	collect:
		for len(batch) < b.maxBatch {
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
		b.finishBatch(batch, false)
		if channelClosed {
			return
		}
	}
}

func (b *orderBatcher) finishBatch(batch []orderBatchRequest, thresholdImmediate bool) {
	started := time.Now()
	err := b.commitSafely(batch)
	duration := time.Since(started)
	b.metrics.commitBatches.Add(1)
	b.metrics.totalCommitNanos.Add(duration.Nanoseconds())
	updateAtomicMax(&b.metrics.maxBatchSize, int64(len(batch)))
	if thresholdImmediate {
		b.metrics.thresholdImmediateBatches.Add(1)
	} else if len(batch) == 1 {
		b.metrics.singletonAggregatedBatches.Add(1)
	} else {
		b.metrics.aggregatedBatches.Add(1)
	}
	if err != nil {
		b.metrics.failedBatches.Add(1)
		b.errMu.Lock()
		if b.firstErr == nil {
			b.firstErr = err
		}
		b.errMu.Unlock()
	} else {
		b.metrics.committedOrders.Add(uint64(len(batch)))
	}
	for _, request := range batch {
		request.result <- err
	}
}

func (b *orderBatcher) commitSafely(batch []orderBatchRequest) (err error) {
	orders := make([]*Order, 0, len(batch))
	for _, request := range batch {
		orders = append(orders, request.order)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("订单批量提交 panic: %v", recovered)
		}
	}()
	if b.commit == nil {
		return errors.New("订单批量持久化函数未配置")
	}
	return b.commit(orders)
}

// Snapshot 返回当前批量提交指标。
func (b *orderBatcher) Snapshot() OrderBatcherSnapshot {
	if b == nil {
		return OrderBatcherSnapshot{}
	}
	batches := b.metrics.commitBatches.Load()
	total := time.Duration(b.metrics.totalCommitNanos.Load())
	average := time.Duration(0)
	if batches > 0 {
		average = total / time.Duration(batches)
	}
	return OrderBatcherSnapshot{
		SubmittedOrders:            b.metrics.submittedOrders.Load(),
		CommittedOrders:            b.metrics.committedOrders.Load(),
		CommitBatches:              batches,
		ThresholdImmediateBatches:  b.metrics.thresholdImmediateBatches.Load(),
		SingletonAggregatedBatches: b.metrics.singletonAggregatedBatches.Load(),
		AggregatedBatches:          b.metrics.aggregatedBatches.Load(),
		FailedBatches:              b.metrics.failedBatches.Load(),
		MaxBatchSize:               int(b.metrics.maxBatchSize.Load()),
		MaxQueueDepth:              int(b.metrics.maxQueueDepth.Load()),
		TotalCommitDuration:        total,
		AverageCommitDuration:      average,
	}
}

func updateAtomicMax(value *atomic.Int64, candidate int64) {
	for current := value.Load(); candidate > current; current = value.Load() {
		if value.CompareAndSwap(current, candidate) {
			return
		}
	}
}
