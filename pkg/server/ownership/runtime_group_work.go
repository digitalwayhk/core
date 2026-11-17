// 本文件是既有runKey的显式组分支；持续发现只受有限额度约束，不等待整批完成。
package ownership

import (
	"context"
	"errors"
	"time"
)

func (r *Runtime) runGroupWork(l *groupWorkLease, stage func(func(context.Context) error) error, restore func() error) error {
	b := l.budget
	for l.ctx.Err() == nil {
		if err := restore(); err != nil {
			return err
		}
		quota, err := b.acquire(l.ctx)
		if err != nil {
			return err
		}
		request := GroupWorkRequest{Limit: quota, ExcludedGroups: b.excluded(l.c)}
		var groups []string
		b.selections.Add(1)
		selectionStarted := time.Now()
		err = stage(func(ctx context.Context) error {
			var e error
			groups, e = r.options.GroupWork.NextGroups(ctx, l.c, request)
			return e
		})
		b.selectionNanos.Add(uint64(time.Since(selectionStarted)))
		if errors.Is(err, ErrFenced) || errors.Is(err, ErrNotReady) {
			l.cancel()
		}
		if err == nil && len(groups) > quota {
			err = ErrCapacity
		}
		if err == nil && len(groups) > 0 {
			err = b.enqueue(l, groups)
		}
		if err != nil {
			b.release(quota)
			b.selectionErrors.Add(1)
			l.failed.Store(true)
			if l.ctx.Err() != nil {
				return l.ctx.Err()
			}
			l.waitPoll()
			continue
		}
		b.release(quota - len(groups))
		if len(groups) == 0 {
			if l.pending.Load() == 0 {
				l.failed.Store(false)
			}
			l.waitPoll()
		}
	}
	return l.ctx.Err()
}
