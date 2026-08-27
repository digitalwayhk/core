package mq

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunReliableKeyedBatchKeepsSameKeySerialAndRunsDifferentKeysInParallel(t *testing.T) {
	release := make(chan struct{})
	started := make(chan string, 3)
	var (
		mu       sync.Mutex
		inflight = map[string]int{}
		peak     = map[string]int{}
	)
	work := func(key, id string) reliableKeyedWork {
		return reliableKeyedWork{key: key, id: id, run: func() error {
			mu.Lock()
			inflight[key]++
			if inflight[key] > peak[key] {
				peak[key] = inflight[key]
			}
			mu.Unlock()
			started <- id
			<-release
			mu.Lock()
			inflight[key]--
			mu.Unlock()
			return nil
		}}
	}
	finished := make(chan []reliableKeyedFailure, 1)
	go func() {
		finished <- runReliableKeyedBatch(context.Background(), 2, []reliableKeyedWork{
			work("a", "a1"), work("a", "a2"), work("b", "b1"),
		})
	}()

	first := map[string]bool{}
	for len(first) < 2 {
		select {
		case id := <-started:
			first[id] = true
		case <-time.After(time.Second):
			t.Fatalf("different keys did not overlap: %v", first)
		}
	}
	require.True(t, first["a1"])
	require.True(t, first["b1"])
	require.False(t, first["a2"])
	close(release)
	require.Empty(t, <-finished)
	mu.Lock()
	require.Equal(t, 1, peak["a"])
	mu.Unlock()
}

func TestRunReliableKeyedBatchFailureBlocksOnlySameKey(t *testing.T) {
	var a2Calls atomic.Int32
	var bCalls atomic.Int32
	want := errors.New("poison")
	failures := runReliableKeyedBatch(context.Background(), 2, []reliableKeyedWork{
		{key: "a", id: "a1", run: func() error { return want }},
		{key: "a", id: "a2", run: func() error { a2Calls.Add(1); return nil }},
		{key: "b", id: "b1", run: func() error { bCalls.Add(1); return nil }},
	})
	require.Equal(t, int32(0), a2Calls.Load())
	require.Equal(t, int32(1), bCalls.Load())
	require.Len(t, failures, 1)
	require.Equal(t, "a", failures[0].work.key)
	require.Equal(t, "a1", failures[0].work.id)
	require.ErrorIs(t, failures[0].err, want)
}

func TestAdmitReliableKeyedWorkLimitsHotKeyAndSkipsBlocked(t *testing.T) {
	items := []reliableKeyedWork{
		{key: "hot", id: "h1"}, {key: "hot", id: "h2"}, {key: "hot", id: "h3"},
		{key: "b", id: "b1"}, {key: "poison", id: "p2"}, {key: "c", id: "c1"},
	}
	admitted := admitReliableKeyedWork(items, map[string]reliableKeyedWork{
		"poison": {key: "poison", id: "p1"},
	}, 1)
	require.Equal(t, []string{"h1", "b1", "c1"}, reliableWorkIDs(admitted))
}

func reliableWorkIDs(items []reliableKeyedWork) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.id)
	}
	return ids
}
