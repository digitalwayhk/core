// 本文件在原选批/确认边界间复用固定worker；实际target退出并提交原结果后才归还组和执行槽。
package nosql

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type writeBehindGroupJob[T types.IModel] struct {
	ctx    context.Context
	record WriteBehindFailureRecord[T]
	result chan writeBehindGroupResult
}
type writeBehindGroupResult struct {
	confirmed int
	err       error
}
type writeBehindGroupExecution[T types.IModel] struct {
	slots   chan struct{}
	jobs    chan writeBehindGroupJob[T]
	active  atomic.Int32
	mu      sync.Mutex
	changed chan struct{}
}

func newWriteBehindGroupExecution[T types.IModel](limit int) *writeBehindGroupExecution[T] {
	g := &writeBehindGroupExecution[T]{slots: make(chan struct{}, limit), jobs: make(chan writeBehindGroupJob[T], limit), changed: make(chan struct{})}
	for range limit {
		g.slots <- struct{}{}
	}
	return g
}
func (g *writeBehindGroupExecution[T]) changeSignal() <-chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.changed
}
func (g *writeBehindGroupExecution[T]) signal() {
	g.mu.Lock()
	close(g.changed)
	g.changed = make(chan struct{})
	g.mu.Unlock()
}

// enqueueIndependentGroups只在原短互斥内选择和预留；所有在途+队列合计不超过执行槽数。
func (p *PrefixedBadgerDB[T]) enqueueIndependentGroups(ctx context.Context, limit int) (jobs []writeBehindGroupJob[T], busy bool, err error) {
	rt := p.disposition
	g := rt.groups
	p.syncExecMu.Lock()
	defer p.syncExecMu.Unlock()
	if ctx.Err() != nil {
		return nil, false, ctx.Err()
	}
	if rt.ctx.Err() != nil || p.IsClosed() {
		return nil, false, ErrWriteStoreClosed
	}
	reserved := 0
	for reserved < limit {
		select {
		case <-g.slots:
			reserved++
		default:
			goto reservedSlots
		}
	}
reservedSlots:
	if reserved == 0 {
		return nil, true, nil
	}
	defer func() {
		for range reserved - len(jobs) {
			g.slots <- struct{}{}
		}
	}()
	if err = p.pruneDispositionCauses(); err != nil {
		return nil, false, err
	}
	selected, notify, e := p.selectDispositionRound(reserved)
	// 只重建已回滚的纯本地选批写集；直到实际事务能提交才预留attempt和调用target。
	for size := reserved / 2; errors.Is(e, badger.ErrTxnTooBig) && size > 0; size /= 2 {
		selected, notify, e = p.selectDispositionRound(size)
	}
	if e != nil {
		return nil, false, e
	}
	// 组和ID在选择事务提交之后、释放syncExecMu之前登记，下一次选批不会误判仍在执行的InFlight。
	results := make(chan writeBehindGroupResult, reserved)
	for _, r := range selected {
		rt.activeIDs.Store(r.ID, false) // false是target，true仍只表示失败handler。
		rt.activeGroups.Store(dispositionGroup(&r), true)
		j := writeBehindGroupJob[T]{ctx: ctx, record: r, result: results}
		jobs = append(jobs, j)
		g.jobs <- j // 已先预留有界槽，jobs队列不会满。
	}
	for _, r := range notify {
		p.dispatchDisposition(r)
	}
	busy = len(g.slots)+reserved-len(jobs) < cap(g.slots)
	return jobs, busy, nil
}

// processIndependentGroupBatch等待本调用选中的真实结果；取消可返回，但旧回调仍占原槽直到实际退出。
func (p *PrefixedBadgerDB[T]) processIndependentGroupBatch(ctx context.Context, limit int) (int, error) {
	rt := p.disposition
	g := rt.groups
	// 通知推进新写/决策；同一手动调用另沿原最小重试间隔复查到期资格，
	// 不依赖无关在途target退出，也不绕过持久NextAttemptAt或有限预算。
	retryTimer := time.NewTimer(rt.policy.MinRetryInterval)
	defer retryTimer.Stop()
	for {
		changed := g.changeSignal()
		jobs, busy, err := p.enqueueIndependentGroups(ctx, limit)
		if err != nil {
			return 0, err
		}
		if len(jobs) == 0 {
			if !busy {
				return 0, nil
			}
			retryTimer.Reset(rt.policy.MinRetryInterval)
			select {
			case <-changed:
				continue
			case <-retryTimer.C:
				continue
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-rt.ctx.Done():
				return 0, ErrWriteStoreClosed
			}
		}
		confirmed := 0
		var combined error
		for range jobs {
			select {
			case r := <-jobs[0].result:
				confirmed += r.confirmed
				combined = errors.Join(combined, r.err)
			case <-ctx.Done():
				return confirmed, errors.Join(combined, ctx.Err())
			case <-rt.ctx.Done():
				return confirmed, errors.Join(combined, ErrWriteStoreClosed)
			}
		}
		return confirmed, combined
	}
}

func (p *PrefixedBadgerDB[T]) independentGroupWorker() {
	defer p.wg.Done()
	rt := p.disposition
	g := rt.groups
	for {
		select {
		case <-rt.ctx.Done():
			return
		case job := <-g.jobs:
			// 与关闭和调用者取消关联；不以取消代替回调真正退出，不启动替代worker。
			ctx, cancel := context.WithCancel(job.ctx)
			stop := context.AfterFunc(rt.ctx, cancel)
			if rt.ctx.Err() != nil {
				cancel()
			}
			g.active.Add(1)
			started := time.Now()
			n, notices, err := p.executeDispositionBatch(ctx, []WriteBehindFailureRecord[T]{job.record}, true)
			stop()
			cancel()
			p.recordSyncMetrics(n, err, time.Since(started))
			p.syncExecMu.Lock()
			rt.activeIDs.Delete(job.record.ID)
			rt.activeGroups.Delete(dispositionGroup(&job.record))
			p.syncExecMu.Unlock()
			g.active.Add(-1)
			g.slots <- struct{}{}
			job.result <- writeBehindGroupResult{n, err}
			for _, r := range notices {
				p.dispatchDisposition(r)
			}
			p.triggerSync()
		}
	}
}

// syncWithIndependentGroups不等待整轮target；已确认组可在其他worker占用时重新选取自己的后继。
func (p *PrefixedBadgerDB[T]) syncWithIndependentGroups() {
	rt := p.disposition
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	var historyNext time.Time
	for rt.ctx.Err() == nil {
		jobs, _, err := p.enqueueIndependentGroups(rt.ctx, p.manager.config.SyncBatchSize)
		if !time.Now().Before(historyNext) {
			limit := rt.policy.ScanLimit
			if limit > 128 {
				limit = 128
			}
			_, historyErr := p.CompactWriteBehindHistory(rt.ctx, limit)
			err = errors.Join(err, historyErr)
			historyNext = time.Now().Add(time.Second)
		}
		delay := rt.policy.MinRetryInterval
		if len(jobs) > 0 {
			delay = p.manager.config.SyncBacklogDrainDelay
		}
		logx.Debugw("write_behind_group_dispatch", logx.Field("selected", len(jobs)), logx.Field("target_active", rt.groups.active.Load()), logx.Field("target_queued", len(rt.groups.jobs)), logx.Field("target_limit", cap(rt.groups.slots)), logx.Field("has_error", err != nil))
		if delay == 0 && len(jobs) > 0 {
			continue
		}
		timer.Reset(delay)
		select {
		case <-rt.ctx.Done():
			return
		case <-p.syncTrigger:
		case <-timer.C:
		}
	}
}
