// 本文件验证有界任务执行的结果顺序、panic 恢复、取消和并发上限。
package utils

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrencyTasksRunRecoversStringPanic(t *testing.T) {
	tasks := &ConcurrencyTasks[int]{
		Params:      []int{1},
		Concurrency: 2,
		Func: func(param int) (interface{}, error) {
			panic("boom")
		},
	}

	tasks.Run()

	err := tasks.GetErr()
	if err == nil {
		t.Fatal("expected panic to be recorded as an error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected panic value in error, got %v", err)
	}
}

func TestConcurrencyTasksRunReleasesSlotAfterPanic(t *testing.T) {
	var calls int32
	tasks := &ConcurrencyTasks[int]{
		Params:      []int{1, 2, 3},
		Concurrency: 2,
		Func: func(param int) (interface{}, error) {
			atomic.AddInt32(&calls, 1)
			if param <= 2 {
				panic(errors.New("failed"))
			}
			return param, nil
		},
	}

	done := make(chan struct{})
	go func() {
		tasks.Run()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run deadlocked after panic")
	}

	if calls != 3 {
		t.Fatalf("expected both tasks to run, got %d", calls)
	}
}

func TestConcurrencyTasksRunRecoversPanicInSerialMode(t *testing.T) {
	tasks := &ConcurrencyTasks[int]{
		Params:      []int{1},
		Concurrency: 1,
		Func: func(int) (interface{}, error) {
			panic("serial boom")
		},
	}

	tasks.Run()

	if err := tasks.GetErr(); err == nil || !strings.Contains(err.Error(), "serial boom") {
		t.Fatalf("串行 panic 应写入结果，实际 error=%v", err)
	}
}

func TestConcurrencyTasksRunSkipsWorkWhenContextAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	tasks := &ConcurrencyTasks[int]{
		Ctx:    ctx,
		Params: []int{1, 2, 3},
		Func: func(int) (interface{}, error) {
			calls.Add(1)
			return nil, nil
		},
	}

	tasks.Run()

	if calls.Load() != 0 {
		t.Fatalf("已取消 context 不应执行任务，实际 calls=%d", calls.Load())
	}
	for i, result := range tasks.Results {
		err, ok := result.(error)
		if !ok || !errors.Is(err, context.Canceled) {
			t.Fatalf("Results[%d]=%v, want context.Canceled", i, result)
		}
	}
}

func TestConcurrencyTasksRunHonorsConcurrencyLimit(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	tasks := &ConcurrencyTasks[int]{
		Params:      []int{1, 2, 3, 4, 5, 6},
		Concurrency: 2,
		Func: func(param int) (interface{}, error) {
			current := active.Add(1)
			for {
				observed := maximum.Load()
				if current <= observed || maximum.CompareAndSwap(observed, current) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			active.Add(-1)
			return param, nil
		},
	}

	tasks.Run()

	if maximum.Load() > 2 {
		t.Fatalf("最大并发=%d, want <=2", maximum.Load())
	}
}
