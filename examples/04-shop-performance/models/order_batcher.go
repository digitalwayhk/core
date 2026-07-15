package models

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"
)

var errOrderBatcherClosed = errors.New("订单批量提交器已关闭")

// orderCommitBacklogThreshold 是启用聚合窗口的最小排队数。
// 少于该值时逐单立即提交，避免低/中并发为了凑批额外等待 1ms。
const orderCommitBacklogThreshold = 16

// orderBatchRequest 保存一个等待可靠持久化的订单。
// result 必须为有缓冲通道，使批量 worker 不会因请求取消而阻塞。
type orderBatchRequest struct {
	order  *Order
	result chan error
}

// orderBatcher 将很短时间窗口内的并发订单合并为一次 Badger 事务。
//
// 这是可靠 Group Commit，而不是“内存入队即成功”：Submit 只有在 commit
// 返回成功后才返回。因此服务突然退出时，已返回成功的订单仍然在 Badger
// WAL 中可恢复。maxBatch 控制单事务上限，wait 控制为了合并写入最多增加的等待。
type orderBatcher struct {
	maxBatch int
	wait     time.Duration
	commit   func([]*Order) error
	requests chan orderBatchRequest
	done     chan struct{}

	stateMu  sync.RWMutex
	closed   bool
	once     sync.Once
	errMu    sync.Mutex
	firstErr error
}

// newOrderBatcher 创建并立即启动一个单 worker 批量提交器。
// 单 worker 是有意的：它保证批次提交顺序，并避免多个 fsync 互相竞争。
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
	}
	go batcher.run()
	return batcher
}

// Submit 把订单加入当前批次，并等待该批持久化结果。
// 读锁覆盖发送动作，确保 Close 关闭 requests 时不会与并发发送产生 panic。
func (b *orderBatcher) Submit(order *Order) error {
	if b == nil || order == nil {
		return NewValidationError("订单不能为空")
	}
	request := orderBatchRequest{order: order, result: make(chan error, 1)}
	b.stateMu.RLock()
	if b.closed {
		b.stateMu.RUnlock()
		return errOrderBatcherClosed
	}
	b.requests <- request
	b.stateMu.RUnlock()
	return <-request.result
}

// Close 停止接收新订单，排空通道中已接收的订单，再返回首个批量错误。
// 重复调用是幂等的。
func (b *orderBatcher) Close() error {
	if b == nil {
		return nil
	}
	b.once.Do(func() {
		b.stateMu.Lock()
		b.closed = true
		close(b.requests)
		b.stateMu.Unlock()
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
		// 先让出一次调度机会，使同时到达的请求有机会进入通道。
		// 让出后仍无积压表示当前是低流量，立即提交以避免固定 1ms 延迟。
		runtime.Gosched()
		if len(b.requests) < orderCommitBacklogThreshold {
			b.finishBatch(batch)
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

		b.finishBatch(batch)
		if channelClosed {
			return
		}
	}
}

func (b *orderBatcher) finishBatch(batch []orderBatchRequest) {
	err := b.commitSafely(batch)
	if err != nil {
		b.errMu.Lock()
		if b.firstErr == nil {
			b.firstErr = err
		}
		b.errMu.Unlock()
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
