// 本文件在固定worker中执行已预留组；超时只取消单元，真实未退出仍占组和槽。
package ownership

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type groupWorkJob struct {
	lease     *groupWorkLease
	group     string
	identity  groupWorkIdentity
	queuedAt  time.Time
	startedAt time.Time
	ctx       context.Context
	timedOut  atomic.Bool
	once      sync.Once
}

func (j *groupWorkJob) markTimeout() {
	if j.ctx != nil && j.ctx.Err() == context.DeadlineExceeded && j.timedOut.CompareAndSwap(false, true) {
		j.lease.budget.timeouts.Add(1)
	}
}
func (j *groupWorkJob) finish() {
	j.once.Do(func() {
		l := j.lease
		b := l.budget
		b.mu.Lock()
		delete(b.held, j.identity)
		b.mu.Unlock()
		l.pending.Add(-1)
		b.release(1)
		l.jobs.Done()
		l.signal()
	})
}
func (j *groupWorkJob) run() {
	defer j.finish()
	l := j.lease
	b := l.budget
	if l.ctx.Err() != nil || b.ctx.Err() != nil {
		return
	}
	b.active.Add(1)
	defer b.active.Add(-1)
	b.started.Add(1)
	b.queueNanos.Add(uint64(time.Since(j.queuedAt)))
	ctx, cancel := context.WithTimeout(l.ctx, l.r.options.Manifest.Timing.Execution)
	defer cancel()
	j.ctx = ctx
	j.startedAt = time.Now()
	l.mu.Lock()
	l.running[j] = struct{}{}
	l.mu.Unlock()
	l.r.inflight.Add(1)
	progress, err := j.execute(ctx)
	j.markTimeout()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil && l.ctx.Err() == nil {
		guard, stop := context.WithTimeout(ctx, l.r.options.Manifest.Timing.Renew)
		err = l.r.store.Validate(guard, l.c, true)
		stop()
		if err != nil {
			l.cancel()
		}
	}
	l.r.inflight.Add(-1)
	l.mu.Lock()
	delete(l.running, j)
	l.mu.Unlock()
	if errors.Is(err, ErrFenced) || errors.Is(err, ErrNotReady) {
		l.cancel()
	}
	if err != nil {
		b.failed.Add(1)
		l.failed.Store(true)
	} else {
		l.failed.Store(false)
	}
	if err == nil && progress && ctx.Err() == nil && l.ctx.Err() == nil {
		now := time.Now().UnixNano()
		l.progress.Store(now)
		l.confirmed.Store(now)
		b.completed.Add(1)
		return
	}
	if err == nil {
		b.noProgress.Add(1)
	}
	timer := time.NewTimer(l.r.options.Manifest.Timing.Poll)
	defer timer.Stop()
	select {
	case <-l.ctx.Done():
	case <-b.ctx.Done():
	case <-timer.C:
	}
}
func (j *groupWorkJob) execute(ctx context.Context) (progress bool, err error) {
	l := j.lease
	guard, stop := context.WithTimeout(ctx, l.r.options.Manifest.Timing.Renew)
	err = l.r.store.Validate(guard, l.c, true)
	stop()
	if err != nil {
		l.cancel()
		return false, err
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if l.ctx.Err() != nil {
		return false, l.ctx.Err()
	}
	started := time.Now()
	defer func() {
		l.budget.handlerNanos.Add(uint64(time.Since(started)))
		if recover() != nil {
			progress = false
			err = fmt.Errorf("ownership group callback panic")
		}
	}()
	return l.r.options.GroupWork.StepGroup(ctx, l.c, j.group)
}
