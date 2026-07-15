package models

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestOrderBatcherGroupsConcurrentOrders 演示可靠 Group Commit 的核心契约：
// 窗口内的订单只执行一次批量持久化，且持久化完成前任何请求都不得返回成功。
func TestOrderBatcherGroupsConcurrentOrders(t *testing.T) {
	const count = 32
	commitEntered := make(chan struct{}, 1)
	releaseCommit := make(chan struct{})
	var commits atomic.Int32
	var batchMu sync.Mutex
	batchSizes := make([]int, 0, count)
	batcher := newOrderBatcher(64, 20*time.Millisecond, func(items []*Order) error {
		commits.Add(1)
		batchMu.Lock()
		batchSizes = append(batchSizes, len(items))
		batchMu.Unlock()
		select {
		case commitEntered <- struct{}{}:
		default:
		}
		<-releaseCommit
		return nil
	})
	t.Cleanup(func() { require.NoError(t, batcher.Close()) })

	start := make(chan struct{})
	results := make(chan error, count)
	var workers sync.WaitGroup
	workers.Add(count)
	for index := 0; index < count; index++ {
		order := NewOrder()
		order.SetID(uint(index + 1))
		go func(item *Order) {
			defer workers.Done()
			<-start
			results <- batcher.Submit(item)
		}(order)
	}
	close(start)

	select {
	case <-commitEntered:
	case <-time.After(time.Second):
		t.Fatal("批量提交未进入持久化函数")
	}
	select {
	case err := <-results:
		t.Fatalf("批量持久化完成前 Submit 不得返回: %v", err)
	default:
	}

	close(releaseCommit)
	workers.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	batchMu.Lock()
	defer batchMu.Unlock()
	total := 0
	maxBatch := 0
	for _, size := range batchSizes {
		total += size
		if size > maxBatch {
			maxBatch = size
		}
	}
	require.Equal(t, count, total)
	require.Greater(t, maxBatch, 1, "并发订单必须至少产生一个真实合批")
	require.Less(t, commits.Load(), int32(count), "Group Commit 的提交数应少于订单数")
}

// TestOrderBatcherPropagatesBatchFailure 确保整批持久化失败会传递给每个等待者，
// 不会把未落盘的订单误报为成功。
func TestOrderBatcherPropagatesBatchFailure(t *testing.T) {
	want := errors.New("批量落盘失败")
	batcher := newOrderBatcher(8, time.Millisecond, func([]*Order) error { return want })

	order := NewOrder()
	order.SetID(1)
	require.ErrorIs(t, batcher.Submit(order), want)
	require.ErrorIs(t, batcher.Close(), want)
}

// TestOrderBatcherCommitsIsolatedOrderImmediately 确保固定的聚合窗口不会惩罚低流量。
// 只有队列已经出现积压时才应等待后续订单；孤立订单应立即持久化。
func TestOrderBatcherCommitsIsolatedOrderImmediately(t *testing.T) {
	const aggregationWindow = 200 * time.Millisecond
	batcher := newOrderBatcher(64, aggregationWindow, func([]*Order) error { return nil })
	t.Cleanup(func() { require.NoError(t, batcher.Close()) })
	order := NewOrder()
	order.SetID(1)

	started := time.Now()
	require.NoError(t, batcher.Submit(order))
	require.Less(t, time.Since(started), aggregationWindow/2,
		"孤立订单不应等待整个 Group Commit 窗口")
}

// TestOrderBatcherDoesNotDelayBelowBacklogThreshold 确保中低并发不会因少量排队就进入聚合等待。
// Group Commit 只用于已经形成明显积压的高流量，否则逐单立即提交更快。
func TestOrderBatcherDoesNotDelayBelowBacklogThreshold(t *testing.T) {
	const (
		count             = 8
		aggregationWindow = 200 * time.Millisecond
	)
	batcher := newOrderBatcher(64, aggregationWindow, func([]*Order) error { return nil })
	t.Cleanup(func() { require.NoError(t, batcher.Close()) })

	start := make(chan struct{})
	results := make(chan error, count)
	for index := 0; index < count; index++ {
		order := NewOrder()
		order.SetID(uint(index + 1))
		go func(item *Order) {
			<-start
			results <- batcher.Submit(item)
		}(order)
	}
	started := time.Now()
	close(start)
	for index := 0; index < count; index++ {
		require.NoError(t, <-results)
	}
	require.Less(t, time.Since(started), aggregationWindow/2,
		"低于积压阈值时不应等待整个 Group Commit 窗口")
}
