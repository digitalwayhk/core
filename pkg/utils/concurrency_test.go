package utils

import (
	"errors"
	"sync/atomic"
	"testing"
)

func TestConcurrencyTasks_Empty(t *testing.T) {
	tasks := ConcurrencyTasks[int]{
		Params: nil,
		Func: func(p int) (interface{}, error) {
			return p * 2, nil
		},
	}
	tasks.Run()
	if len(tasks.Results) != 0 {
		t.Fatalf("expected empty results, got %d", len(tasks.Results))
	}
	if tasks.Successes() != 0 {
		t.Fatalf("Successes on empty = %d, want 0", tasks.Successes())
	}
	if tasks.GetErr() != nil {
		t.Fatalf("GetErr on empty = %v, want nil", tasks.GetErr())
	}
}

func TestConcurrencyTasks_Sequential(t *testing.T) {
	params := []int{1, 2, 3, 4, 5}
	tasks := ConcurrencyTasks[int]{
		Params:      params,
		Concurrency: 1, // forces the sequential code path
		Func: func(p int) (interface{}, error) {
			return p * p, nil
		},
	}
	tasks.Run()
	if len(tasks.Results) != len(params) {
		t.Fatalf("results len = %d, want %d", len(tasks.Results), len(params))
	}
	for i, p := range params {
		if got, want := tasks.Results[i], p*p; got != want {
			t.Errorf("Results[%d] = %v, want %v", i, got, want)
		}
	}
	if tasks.Successes() != len(params) {
		t.Errorf("Successes = %d, want %d", tasks.Successes(), len(params))
	}
	if tasks.GetErr() != nil {
		t.Errorf("GetErr = %v, want nil", tasks.GetErr())
	}
}

func TestConcurrencyTasks_Concurrent(t *testing.T) {
	const n = 50
	params := make([]int, n)
	for i := range params {
		params[i] = i
	}
	var calls int32
	tasks := ConcurrencyTasks[int]{
		Params:      params,
		Concurrency: 8,
		Func: func(p int) (interface{}, error) {
			atomic.AddInt32(&calls, 1)
			return p + 1, nil
		},
	}
	tasks.Run()
	if int(calls) != n {
		t.Fatalf("call count = %d, want %d", calls, n)
	}
	if len(tasks.Results) != n {
		t.Fatalf("results len = %d, want %d", len(tasks.Results), n)
	}
	for i := 0; i < n; i++ {
		if tasks.Results[i] != i+1 {
			t.Errorf("Results[%d] = %v, want %d", i, tasks.Results[i], i+1)
		}
	}
	if tasks.Successes() != n {
		t.Errorf("Successes = %d, want %d", tasks.Successes(), n)
	}
}

func TestConcurrencyTasks_Errors(t *testing.T) {
	wantErr := errors.New("boom")
	tasks := ConcurrencyTasks[int]{
		Params:      []int{1, 2, 3, 4},
		Concurrency: 1,
		Func: func(p int) (interface{}, error) {
			if p%2 == 0 {
				return nil, wantErr
			}
			return p, nil
		},
	}
	tasks.Run()
	if tasks.Successes() != 2 {
		t.Errorf("Successes = %d, want 2", tasks.Successes())
	}
	if tasks.GetErr() == nil {
		t.Errorf("GetErr = nil, want %v", wantErr)
	}
}

func TestConcurrencyTasks_DefaultConcurrency(t *testing.T) {
	// Concurrency==0 must default (to 8) and still run correctly.
	params := []int{10, 20, 30}
	tasks := ConcurrencyTasks[int]{
		Params: params,
		Func: func(p int) (interface{}, error) {
			return p, nil
		},
	}
	tasks.Run()
	if len(tasks.Results) != len(params) {
		t.Fatalf("results len = %d, want %d", len(tasks.Results), len(params))
	}
	for i, p := range params {
		if tasks.Results[i] != p {
			t.Errorf("Results[%d] = %v, want %d", i, tasks.Results[i], p)
		}
	}
}
