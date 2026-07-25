// 本文件提供保序、有界且可取消的批量任务执行能力。
package utils

import (
	"context"
	"fmt"
	"sync"

	"github.com/zeromicro/go-zero/core/logx"
)

const gConcurrencyCount = 8

// ConcurrencyTasks 按 Params 顺序保存并发任务的结果或错误。
//
// Func 已经开始执行后，取消 Ctx 不会强制中断它；Func 需要自行响应业务上下文。
type ConcurrencyTasks[T interface{}] struct {
	Ctx         context.Context
	Params      []T
	Results     []interface{}
	Func        func(param T) (interface{}, error)
	Concurrency int
}

// Run 使用固定数量的 worker 执行任务，并保持 Results 与 Params 的索引一致。
func (t *ConcurrencyTasks[T]) Run() {
	t.Results = make([]interface{}, len(t.Params))
	if len(t.Params) == 0 {
		return
	}

	ctx := t.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		for index := range t.Results {
			t.Results[index] = err
		}
		return
	}

	workerCount := t.Concurrency
	if workerCount <= 0 {
		workerCount = gConcurrencyCount
	}
	if workerCount > len(t.Params) {
		workerCount = len(t.Params)
	}

	jobs := make(chan int)
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for index := range jobs {
				t.execute(index)
			}
		}()
	}

	next := 0
schedule:
	for ; next < len(t.Params); next++ {
		select {
		case jobs <- next:
		case <-ctx.Done():
			break schedule
		}
	}
	close(jobs)
	workers.Wait()

	for ; next < len(t.Params); next++ {
		t.Results[next] = ctx.Err()
	}
}

// execute 将普通错误和 panic 统一写入对应的结果位置。
func (t *ConcurrencyTasks[T]) execute(index int) {
	param := t.Params[index]
	defer func() {
		if recovered := recover(); recovered != nil {
			err := fmt.Errorf("panic: %v", recovered)
			t.Results[index] = err
			logx.Errorf("[PANIC]param=%v,err=%v", param, err)
		}
	}()

	result, err := t.Func(param)
	if err != nil {
		t.Results[index] = err
		return
	}
	t.Results[index] = result
}

// Successes 返回没有保存 error 的结果数量。
func (t *ConcurrencyTasks[T]) Successes() int {
	successes := 0
	for _, result := range t.Results {
		if _, ok := result.(error); ok {
			continue
		}
		successes++
	}
	return successes
}

// GetErr 返回 Results 中第一个错误；没有错误时返回 nil。
func (t *ConcurrencyTasks[T]) GetErr() error {
	for _, result := range t.Results {
		if err, ok := result.(error); ok {
			return err
		}
	}
	return nil
}
