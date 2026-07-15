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
	batcher := &orderBatcher{
		maxBatch: 64,
		wait:     20 * time.Millisecond,
		requests: make(chan orderBatchRequest, 64*8),
		done:     make(chan struct{}),
		closing:  make(chan struct{}),
		commit: func(items []*Order) error {
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
		},
	}
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
	require.Eventually(t, func() bool { return len(batcher.requests) == count }, time.Second, time.Millisecond,
		"必须先确定性地把并发请求放入队列，再启动 worker 验证真实合批")
	go batcher.run()

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
	snapshot := batcher.Snapshot()
	require.Zero(t, snapshot.ThresholdImmediateBatches)
	require.Greater(t, snapshot.AggregatedBatches, uint64(0))
	require.Zero(t, snapshot.SingletonAggregatedBatches)
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

// TestOrderBatcherCloseReleasesBlockedSubmitters 验证队列已满时，Close 仍能先标记关闭，
// 并唤醒还没有进入队列的 Submit，不会因其长时间持有读锁而无法开始优雅停机。
func TestOrderBatcherCloseReleasesBlockedSubmitters(t *testing.T) {
	commitEntered := make(chan struct{}, 1)
	releaseCommit := make(chan struct{})
	batcher := newOrderBatcher(1, time.Millisecond, func([]*Order) error {
		select {
		case commitEntered <- struct{}{}:
		default:
		}
		<-releaseCommit
		return nil
	})

	const queued = 8
	results := make(chan error, queued+2)
	for index := 0; index < queued+2; index++ {
		order := NewOrder()
		order.SetID(uint(index + 1))
		go func(item *Order) { results <- batcher.Submit(item) }(order)
	}
	select {
	case <-commitEntered:
	case <-time.After(time.Second):
		t.Fatal("批量提交未进入阻塞点")
	}

	closeResult := make(chan error, 1)
	go func() { closeResult <- batcher.Close() }()

	select {
	case err := <-results:
		require.ErrorIs(t, err, errOrderBatcherClosed,
			"尚未入队的 Submit 应在 Close 后立即返回关闭错误")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Close 未唤醒被满队列阻塞的 Submit")
	}

	close(releaseCommit)
	require.NoError(t, <-closeResult)
}

func TestOrderBatcherSnapshotRecordsReliableCommits(t *testing.T) {
	batcher := newOrderBatcher(8, time.Millisecond, func([]*Order) error { return nil })
	order := NewOrder()
	order.SetID(1)
	require.NoError(t, batcher.Submit(order))
	require.NoError(t, batcher.Close())

	snapshot := batcher.Snapshot()
	require.Equal(t, uint64(1), snapshot.SubmittedOrders)
	require.Equal(t, uint64(1), snapshot.CommittedOrders)
	require.Equal(t, uint64(1), snapshot.CommitBatches)
	require.Equal(t, uint64(1), snapshot.ThresholdImmediateBatches)
	require.Zero(t, snapshot.SingletonAggregatedBatches)
	require.Equal(t, uint64(0), snapshot.FailedBatches)
	require.Equal(t, 1, snapshot.MaxBatchSize)
	require.GreaterOrEqual(t, snapshot.TotalCommitDuration, time.Duration(0))
}

func TestOrderBatcherSnapshotSeparatesSingletonAggregationPath(t *testing.T) {
	batcher := &orderBatcher{commit: func([]*Order) error { return nil }}
	order := NewOrder()
	order.SetID(1)
	result := make(chan error, 1)

	batcher.finishBatch([]orderBatchRequest{{order: order, result: result}}, false)
	require.NoError(t, <-result)

	snapshot := batcher.Snapshot()
	require.Zero(t, snapshot.ThresholdImmediateBatches)
	require.Equal(t, uint64(1), snapshot.SingletonAggregatedBatches)
	require.Zero(t, snapshot.AggregatedBatches)
}
