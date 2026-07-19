// Package nosql 验证通用可靠 Group Commit 的聚合、结果路由、panic 和关闭并发。
package nosql

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBatchCommitterLowBacklogCommitsWithoutCollectWindowDelay(t *testing.T) {
	commitEntered := make(chan struct{})
	committer := newBatchCommitter[testFund](BatchCommitConfig{
		MaxBatch: 32, CollectWindow: 200 * time.Millisecond, QueueCapacity: 64,
	}, func(operations []WriteOperation[testFund]) (BatchWriteResult, error) {
		close(commitEntered)
		return BatchWriteResult{Committed: len(operations)}, nil
	})
	t.Cleanup(func() { _ = committer.Close(context.Background()) })

	result := make(chan error, 1)
	go func() {
		result <- committer.Submit(context.Background(), WriteOperation[testFund]{
			Type: WriteOperationSave,
			Item: newFund("immediate", "HK", 1),
		})
	}()

	select {
	case <-commitEntered:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("低积压提交等待了完整聚合窗口")
	}
	require.NoError(t, <-result)
}

func TestBatchCommitterAggregatesConcurrentRequests(t *testing.T) {
	var (
		mu         sync.Mutex
		batchSizes []int
	)
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	var commitCount int
	committer := newBatchCommitter[testFund](BatchCommitConfig{
		MaxBatch: 8, CollectWindow: 20 * time.Millisecond, CollectBacklog: 1, QueueCapacity: 32,
	}, func(operations []WriteOperation[testFund]) (BatchWriteResult, error) {
		mu.Lock()
		commitCount++
		currentCommit := commitCount
		batchSizes = append(batchSizes, len(operations))
		mu.Unlock()
		if currentCommit == 1 {
			close(firstEntered)
			<-releaseFirst
		}
		return BatchWriteResult{Committed: len(operations)}, nil
	})
	t.Cleanup(func() { _ = committer.Close(context.Background()) })

	results := make(chan error, 4)
	go func() {
		results <- committer.Submit(context.Background(), WriteOperation[testFund]{
			Type: WriteOperationSave,
			Item: newFund("warmup", "HK", 1),
		})
	}()
	<-firstEntered
	for index := range 3 {
		index := index
		go func() {
			results <- committer.Submit(context.Background(), WriteOperation[testFund]{
				Type: WriteOperationSave,
				Item: newFund("aggregate", string(rune('A'+index)), 1),
			})
		}()
	}
	require.Eventually(t, func() bool { return len(committer.requests) == 3 }, time.Second, time.Millisecond)
	close(releaseFirst)
	for range 4 {
		require.NoError(t, <-results)
	}

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []int{1, 3}, batchSizes)
}

func TestBatchCommitterRoutesPartialPrefixResult(t *testing.T) {
	commitErr := errors.New("second operation failed")
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	var commitCount atomic.Int32
	committer := newBatchCommitter[testFund](BatchCommitConfig{
		MaxBatch: 3, CollectWindow: 20 * time.Millisecond, CollectBacklog: 1, QueueCapacity: 8,
	}, func(operations []WriteOperation[testFund]) (BatchWriteResult, error) {
		if commitCount.Add(1) == 1 {
			close(firstEntered)
			<-releaseFirst
			return BatchWriteResult{Committed: len(operations)}, nil
		}
		return BatchWriteResult{Committed: 1}, commitErr
	})
	t.Cleanup(func() { _ = committer.Close(context.Background()) })

	warmup := make(chan error, 1)
	go func() {
		warmup <- committer.Submit(context.Background(), WriteOperation[testFund]{
			Type: WriteOperationSave,
			Item: newFund("warmup", "HK", 1),
		})
	}()
	<-firstEntered
	results := make(chan error, 3)
	for index := range 3 {
		index := index
		go func() {
			results <- committer.Submit(context.Background(), WriteOperation[testFund]{
				Type: WriteOperationSave,
				Item: newFund("partial", string(rune('A'+index)), 1),
			})
		}()
	}
	require.Eventually(t, func() bool { return len(committer.requests) == 3 }, time.Second, time.Millisecond)
	close(releaseFirst)
	require.NoError(t, <-warmup)
	var success, failed int
	for range 3 {
		if err := <-results; err == nil {
			success++
		} else {
			require.ErrorIs(t, err, commitErr)
			failed++
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 2, failed)
}

func TestBatchCommitterSubmitBatchReturnsCommittedPrefix(t *testing.T) {
	commitErr := errors.New("third operation failed")
	committer := newBatchCommitter[testFund](BatchCommitConfig{
		MaxBatch: 1, CollectWindow: time.Millisecond, QueueCapacity: 1,
	}, func(operations []WriteOperation[testFund]) (BatchWriteResult, error) {
		require.Len(t, operations, 3)
		return BatchWriteResult{Committed: 2}, commitErr
	})
	t.Cleanup(func() { _ = committer.Close(context.Background()) })

	result, err := committer.SubmitBatch(context.Background(), []WriteOperation[testFund]{
		{Type: WriteOperationSave, Item: newFund("batch", "A", 1)},
		{Type: WriteOperationSave, Item: newFund("batch", "B", 1)},
		{Type: WriteOperationSave, Item: newFund("batch", "C", 1)},
	})
	require.Equal(t, BatchWriteResult{Committed: 2}, result)
	require.ErrorIs(t, err, commitErr)
}

func TestBatchCommitterConvertsCommitPanic(t *testing.T) {
	committer := newBatchCommitter[testFund](BatchCommitConfig{
		MaxBatch: 1, CollectWindow: time.Millisecond, QueueCapacity: 1,
	}, func([]WriteOperation[testFund]) (BatchWriteResult, error) {
		panic("commit exploded")
	})
	t.Cleanup(func() { _ = committer.Close(context.Background()) })

	err := committer.Submit(context.Background(), WriteOperation[testFund]{
		Type: WriteOperationSave,
		Item: newFund("panic", "HK", 1),
	})
	require.ErrorContains(t, err, "commit exploded")
}

func TestBatchCommitterCloseDrainsAcceptedAndRejectsNewRequests(t *testing.T) {
	commitEntered := make(chan struct{})
	releaseCommit := make(chan struct{})
	committer := newBatchCommitter[testFund](BatchCommitConfig{
		MaxBatch: 1, CollectWindow: time.Millisecond, QueueCapacity: 1,
	}, func(operations []WriteOperation[testFund]) (BatchWriteResult, error) {
		close(commitEntered)
		<-releaseCommit
		return BatchWriteResult{Committed: len(operations)}, nil
	})
	accepted := make(chan error, 1)
	go func() {
		accepted <- committer.Submit(context.Background(), WriteOperation[testFund]{
			Type: WriteOperationSave,
			Item: newFund("close", "HK", 1),
		})
	}()
	<-commitEntered
	closed := make(chan error, 1)
	go func() { closed <- committer.Close(context.Background()) }()
	<-committer.closing
	err := committer.Submit(context.Background(), WriteOperation[testFund]{
		Type: WriteOperationSave,
		Item: newFund("late", "HK", 1),
	})
	require.ErrorIs(t, err, ErrWriteStoreClosed)
	close(releaseCommit)
	require.NoError(t, <-accepted)
	require.NoError(t, <-closed)
	require.NoError(t, committer.Close(context.Background()))
}

func TestBatchCommitterSubmitHonorsCanceledContextBeforeAcceptance(t *testing.T) {
	committer := newBatchCommitter[testFund](BatchCommitConfig{
		MaxBatch: 1, CollectWindow: time.Second, QueueCapacity: 1,
	}, func(operations []WriteOperation[testFund]) (BatchWriteResult, error) {
		return BatchWriteResult{Committed: len(operations)}, nil
	})
	t.Cleanup(func() { _ = committer.Close(context.Background()) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := committer.Submit(ctx, WriteOperation[testFund]{Type: WriteOperationSave, Item: newFund("cancel", "HK", 1)})
	require.ErrorIs(t, err, context.Canceled)
}
