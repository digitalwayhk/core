package router

import (
	"sync"
	"testing"
)

func TestServiceContextRunStateFlagConcurrentAccess(t *testing.T) {
	// 完整 SetRunState 中 membership/broker 的串行化属于任务 12.6；本节仅验证运行状态 flag 的原子读写。
	const (
		workers    = 32
		iterations = 200
	)
	ctx := &ServiceContext{}
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(workers)
	for worker := 0; worker < workers; worker++ {
		worker := worker
		go func() {
			defer wait.Done()
			<-start
			for iteration := 0; iteration < iterations; iteration++ {
				ctx.isStart.Store((worker+iteration)%2 == 0)
				_ = ctx.isStart.Load()
			}
		}()
	}
	close(start)
	wait.Wait()
}
