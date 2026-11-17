// 本文件跟踪一个原租约的有限组工作，不把业务身份写入指标或日志。
package ownership

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

type groupWorkLease struct {
	r         *Runtime
	c         Credential
	ctx       context.Context
	cancel    context.CancelFunc
	progress  *atomic.Int64
	failed    *atomic.Bool
	confirmed *atomic.Int64
	budget    *GroupWorkBudget
	jobs      sync.WaitGroup
	pending   atomic.Int64
	notify    chan struct{}
	mu        sync.Mutex
	running   map[*groupWorkJob]struct{}
}

func nowWorkTime() time.Time      { return time.Now() }
func (l *groupWorkLease) finish() { l.cancel(); l.jobs.Wait(); l.budget.detach(l) }
func (l *groupWorkLease) signal() {
	select {
	case l.notify <- struct{}{}:
	default:
	}
}
func (l *groupWorkLease) execution() (int64, time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var earliest time.Time
	for j := range l.running {
		if earliest.IsZero() || j.startedAt.Before(earliest) {
			earliest = j.startedAt
		}
		j.markTimeout()
	}
	return int64(len(l.running)), earliest
}
func (l *groupWorkLease) waitPoll() {
	timer := time.NewTimer(l.r.options.Manifest.Timing.Poll)
	defer timer.Stop()
	select {
	case <-l.ctx.Done():
	case <-l.notify:
	case <-timer.C:
	}
}
