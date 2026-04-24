package utils

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConcurrencyTasks_Basic(t *testing.T) {
	params := []int{1, 2, 3, 4, 5}
	tasks := &ConcurrencyTasks[int]{
		Ctx:    context.Background(),
		Params: params,
		Func: func(p int) (interface{}, error) {
			return p * 2, nil
		},
	}
	tasks.Run()
	assert.Len(t, tasks.Results, len(params))
	for i, r := range tasks.Results {
		assert.Equal(t, params[i]*2, r)
	}
}

func TestConcurrencyTasks_Empty(t *testing.T) {
	tasks := &ConcurrencyTasks[int]{
		Ctx:    context.Background(),
		Params: []int{},
		Func:   func(p int) (interface{}, error) { return p, nil },
	}
	tasks.Run()
	assert.Len(t, tasks.Results, 0)
}

func TestConcurrencyTasks_WithError(t *testing.T) {
	params := []int{1, 2, 3}
	expectedErr := errors.New("test error")
	tasks := &ConcurrencyTasks[int]{
		Ctx:    context.Background(),
		Params: params,
		Func: func(p int) (interface{}, error) {
			if p == 2 {
				return nil, expectedErr
			}
			return p, nil
		},
	}
	tasks.Run()
	err := tasks.GetErr()
	assert.Equal(t, expectedErr, err)
}

func TestConcurrencyTasks_Successes(t *testing.T) {
	params := []int{1, 2, 3, 4}
	tasks := &ConcurrencyTasks[int]{
		Ctx:    context.Background(),
		Params: params,
		Func: func(p int) (interface{}, error) {
			if p%2 == 0 {
				return nil, errors.New("even error")
			}
			return p, nil
		},
	}
	tasks.Run()
	// Params 1 and 3 succeed (2 successes), params 2 and 4 fail
	assert.Equal(t, 2, tasks.Successes())
}

func TestConcurrencyTasks_SingleConcurrency(t *testing.T) {
	params := []string{"a", "b", "c"}
	results := make([]string, 0)
	tasks := &ConcurrencyTasks[string]{
		Ctx:         context.Background(),
		Params:      params,
		Concurrency: 1,
		Func: func(p string) (interface{}, error) {
			results = append(results, p)
			return p, nil
		},
	}
	tasks.Run()
	assert.Len(t, tasks.Results, 3)
	assert.Equal(t, 3, tasks.Successes())
}

func TestConcurrencyTasks_GetErr_NoError(t *testing.T) {
	tasks := &ConcurrencyTasks[int]{
		Ctx:    context.Background(),
		Params: []int{1, 2},
		Func:   func(p int) (interface{}, error) { return p, nil },
	}
	tasks.Run()
	assert.Nil(t, tasks.GetErr())
}
