package utils

import (
	"github.com/zeromicro/go-zero/core/logx"
	"sync"
)

const (
	gConcurrencyCount = 8
)

type ConcurrencyTasks[T interface{}] struct {
	Params      []T
	Results     []interface{}
	Func        func(param T) (interface{}, error)
	Concurrency int
	ch          chan struct{}
	wg          sync.WaitGroup
}

func (t *ConcurrencyTasks[T]) Run() {
	rs := make([]interface{}, len(t.Params))
	t.Results = rs
	if len(t.Params) == 0 {
		return
	}

	var count = gConcurrencyCount
	if t.Concurrency > 0 {
		count = t.Concurrency
	}
	if count == 1 {
		for i := 0; i < len(t.Params); i++ {
			t.extFun(i)
		}
		return
	}
	t.wg.Add(len(t.Params))
	t.ch = make(chan struct{}, count)
	for i := 0; i < len(t.Params); i++ {
		t.ch <- struct{}{}
		go t.doFun(i)
	}
	close(t.ch)
	t.wg.Wait()
}

func (t *ConcurrencyTasks[T]) extFun(i int) {
	//执行
	var param = t.Params[i]
	r, err := t.Func(param)
	if err != nil {
		t.Results[i] = err
	} else {
		t.Results[i] = r
	}
}

func (t *ConcurrencyTasks[T]) doFun(i int) {
	//logger.LogInfof("doFun start,i=%d", i)
	var param T
	defer func() {
		// 从panic中恢复
		if e := recover(); e != nil {
			err := e.(error)
			logx.Infof("[PANIC]param=%s,err=%v\n", param, err)
		}
	}()
	defer t.wg.Done()
	//执行
	param = t.Params[i]
	r, err := t.Func(param)
	if err != nil {
		t.Results[i] = err
	} else {
		t.Results[i] = r
	}
	<-t.ch
}

func (t *ConcurrencyTasks[T]) Successes() int {
	successes := 0
	for _, r := range t.Results {
		if _, ok := r.(error); ok {
			continue
		}
		successes += 1
	}
	return successes
}

func (t *ConcurrencyTasks[T]) GetErr() error {
	for _, result := range t.Results {
		if err, ok := result.(error); ok {
			return err
		}
	}
	return nil
}
