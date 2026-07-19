// Package nosql 验证通用可靠 Group Commit 的聚合、结果路由、panic 和关闭并发。
package nosql

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBatchCommitterAggregatesConcurrentRequests(t *testing.T) {
	var (
		mu         sync.Mutex
		batchSizes []int
	)
	committer := newBatchCommitter[testFund](BatchCommitConfig{
		MaxBatch: 8, CollectWindow: 20 * time.Millisecond, QueueCapacity: 32,
	}, func(operations []WriteOperation[testFund]) (BatchWriteResult, error) {
		mu.Lock()
		batchSizes = append(batchSizes, len(operations))
		mu.Unlock()
		return BatchWriteResult{Committed: len(operations)}, nil
	})
	t.Cleanup(func() { _ = committer.Close(context.Background()) })

	start := make(chan struct{})
	results := make(chan error, 3)
	for index := range 3 {
		index := index
		go func() {
			<-start
			results <- committer.Submit(context.Background(), WriteOperation[testFund]{
				Type: WriteOperationSave,
				Item: newFund("aggregate", string(rune('A'+index)), 1),
			})
		}()
	}
	close(start)
	for range 3 {
		require.NoError(t, <-results)
	}

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []int{3}, batchSizes)
}

func TestBatchCommitterRoutesPartialPrefixResult(t *testing.T) {
	commitErr := errors.New("second operation failed")
	committer := newBatchCommitter[testFund](BatchCommitConfig{
		MaxBatch: 3, CollectWindow: 20 * time.Millisecond, QueueCapacity: 8,
	}, func([]WriteOperation[testFund]) (BatchWriteResult, error) {
		return BatchWriteResult{Committed: 1}, commitErr
	})
	t.Cleanup(func() { _ = committer.Close(context.Background()) })

	start := make(chan struct{})
	results := make(chan error, 3)
	for index := range 3 {
		index := index
		go func() {
			<-start
			results <- committer.Submit(context.Background(), WriteOperation[testFund]{
				Type: WriteOperationSave,
				Item: newFund("partial", string(rune('A'+index)), 1),
			})
		}()
	}
	close(start)
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
