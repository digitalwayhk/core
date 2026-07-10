package utils

import (
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
