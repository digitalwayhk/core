// 本文件通过真实队列阻塞验证等待指标，不以提交耗时替代排队等待。
package nosql

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestBatchQueueWaitMetricsObserveBlockedCommit 验证首批阻塞期间后续操作的排队时间可观测。
func TestBatchQueueWaitMetricsObserveBlockedCommit(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var releaseOnce sync.Once
	b := newBatchCommitter[testFund](BatchCommitConfig{MaxBatch: 1, QueueCapacity: 4, CollectWindow: time.Millisecond}, func(ops []WriteOperation[testFund]) (BatchWriteResult, error) {
		once.Do(func() { close(started); <-release })
		return BatchWriteResult{Committed: len(ops)}, nil
	})
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }); require.NoError(t, b.Close(context.Background())) })
	errors := make(chan error, 2)
	go func() {
		errors <- b.Submit(context.Background(), WriteOperation[testFund]{Type: WriteOperationSave, Item: newFund("first", "spot", 1)})
	}()
	<-started
	go func() {
		errors <- b.Submit(context.Background(), WriteOperation[testFund]{Type: WriteOperationSave, Item: newFund("second", "spot", 1)})
	}()
	require.Eventually(t, func() bool { return len(b.requests) == 1 }, time.Second, time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	releaseOnce.Do(func() { close(release) })
	require.NoError(t, <-errors)
	require.NoError(t, <-errors)
	require.GreaterOrEqual(t, b.Metrics().TotalQueueWaitDuration, 10*time.Millisecond)
}
