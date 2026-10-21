// 本文件属于框架持续归属运行时，取消和关闭都等待实际回调退出。
package ownership

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Runtime 管理固定上限的归属worker；不是应用侧调度器。
type Runtime struct {
	store    Store
	options  Options
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	active   atomic.Int64
	ready    atomic.Int64
	inflight atomic.Int64
	acquired atomic.Uint64
	revoked  atomic.Uint64
	unknown  atomic.Uint64
	statusMu sync.RWMutex
	status   map[string]*runtimeKeyProgress
}

// StartRuntime 校验合同并启动有限归属工作者。
func StartRuntime(ctx context.Context, store Store, options Options) (*Runtime, error) {
	options.Manifest = options.Manifest.Normalize()
	if err := options.Manifest.Validate(); err != nil {
		return nil, err
	}
	if store == nil || options.InstanceID == "" || options.SessionID == "" || options.MaxOwned <= 0 || options.MaxOwned > 64 || options.Activate == nil || options.Step == nil || options.Observe == nil {
		return nil, ErrUnknown
	}
	ctx, cancel := context.WithCancel(ctx)
	r := &Runtime{store: store, options: options, ctx: ctx, cancel: cancel, done: make(chan struct{}), status: map[string]*runtimeKeyProgress{}}
	go r.run()
	return r, nil
}

// Close 停止准入并等待真实工作者退出，ctx只约束调用方等待。
func (r *Runtime) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.cancel()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Done 在所有真实回调退出后关闭。
func (r *Runtime) Done() <-chan struct{} { return r.done }

type runtimeResult struct {
	key string
	err error
}

func (r *Runtime) run() {
	defer close(r.done)
	var workers sync.WaitGroup
	defer workers.Wait()
	results := make(chan runtimeResult, r.options.MaxOwned)
	running := map[string]bool{}
	cooldown := map[string]time.Time{}
	cursor := 0
	ticker := time.NewTicker(r.options.Manifest.Timing.Poll)
	defer ticker.Stop()
	for r.ctx.Err() == nil {
		for draining := true; draining; {
			select {
			case result := <-results:
				delete(running, result.key)
				if result.err != nil {
					cooldown[result.key] = time.Now().Add(r.options.Manifest.Timing.Stall)
				}
			default:
				draining = false
			}
		}
		for checked := 0; checked < len(r.options.Manifest.Keys) && len(running) < r.options.MaxOwned && r.ctx.Err() == nil; checked++ {
			key := r.options.Manifest.Keys[cursor%len(r.options.Manifest.Keys)]
			cursor++
			if running[key] || time.Now().Before(cooldown[key]) {
				continue
			}
			request := Credential{Scope: r.options.Manifest.Scope, Key: key, Service: r.options.Manifest.Service, InstanceID: r.options.InstanceID, SessionID: r.options.SessionID}
			ctx, stop := context.WithTimeout(r.ctx, r.options.Manifest.Timing.Renew)
			c, err := r.store.Acquire(ctx, request, r.options.Manifest.Timing.Lease)
			stop()
			if err != nil {
				continue
			}
			running[key] = true
			r.active.Add(1)
			r.acquired.Add(1)
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer r.active.Add(-1)
				err := r.runKey(c)
				results <- runtimeResult{c.Key, err}
			}()
		}
		select {
		case <-r.ctx.Done():
			return
		case result := <-results:
			delete(running, result.key)
			if result.err != nil {
				cooldown[result.key] = time.Now().Add(r.options.Manifest.Timing.Stall)
			}
		case <-ticker.C:
		}
	}
}

func (r *Runtime) runKey(c Credential) (result error) {
	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()
	var stageSince, lastProgress atomic.Int64
	lastProgress.Store(time.Now().UnixNano())
	keyProgress := &runtimeKeyProgress{context: ctx, credential: c, stageSince: &stageSince}
	r.statusMu.Lock()
	r.status[c.Key] = keyProgress
	r.statusMu.Unlock()
	defer func() {
		r.statusMu.Lock()
		if r.status[c.Key] == keyProgress {
			delete(r.status, c.Key)
		}
		r.statusMu.Unlock()
	}()
	watchDone := make(chan struct{})
	readyState := &runtimeReadyState{}
	go func() {
		defer close(watchDone)
		defer readyState.lose(r)
		r.watchKey(ctx, c, cancel, &stageSince, &lastProgress, readyState)
	}()
	defer func() { cancel(); <-watchDone }()
	stage := func(callback func(context.Context) error) (err error) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r.inflight.Add(1)
		defer r.inflight.Add(-1)
		stageSince.Store(time.Now().UnixNano())
		defer stageSince.Store(0)
		workCtx, stop := context.WithTimeout(ctx, r.options.Manifest.Timing.Execution)
		defer stop()
		timeout := time.AfterFunc(r.options.Manifest.Timing.Execution, func() { r.revoked.Add(1); cancel() })
		defer timeout.Stop()
		defer func() {
			if recover() != nil {
				err = fmt.Errorf("ownership callback panic")
			}
		}()
		err = callback(workCtx)
		if workCtx.Err() != nil {
			return workCtx.Err()
		}
		return err
	}
	if err := stage(func(ctx context.Context) error { return r.options.Activate(ctx, c) }); err != nil {
		return err
	}
	var revision int64
	restore := func() error {
		query, stop := context.WithTimeout(ctx, r.options.Manifest.Timing.Renew)
		snapshot, err := r.store.References(query, c.Scope, c.Key)
		stop()
		if err != nil {
			return err
		}
		if snapshot.Revision < revision {
			return ErrStaleSnapshot
		}
		if snapshot.Revision == revision {
			return nil
		}
		if r.options.Restore != nil {
			if err := stage(func(ctx context.Context) error { return r.options.Restore(ctx, c, snapshot) }); err != nil {
				return err
			}
		}
		revision = snapshot.Revision
		return nil
	}
	if err := restore(); err != nil {
		return err
	}
	query, stop := context.WithTimeout(ctx, r.options.Manifest.Timing.Renew)
	err := r.store.Ready(query, c)
	stop()
	if err != nil {
		return err
	}
	readyState.mu.Lock()
	if readyState.lost || ctx.Err() != nil {
		readyState.mu.Unlock()
		return ErrFenced
	}
	readyState.ready = true
	r.ready.Add(1)
	readyState.mu.Unlock()
	for ctx.Err() == nil {
		if err := restore(); err != nil {
			return err
		}
		progress := false
		err := stage(func(ctx context.Context) error { var err error; progress, err = r.options.Step(ctx, c); return err })
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if progress && err == nil {
			lastProgress.Store(time.Now().UnixNano())
			keyProgress.confirmed.Store(time.Now().UnixNano())
			continue
		}
		// 失败保留原工作，有限退避；不能增加goroutine越过本key屏障。
		timer := time.NewTimer(r.options.Manifest.Timing.Poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return ctx.Err()
}

func (r *Runtime) watchKey(ctx context.Context, c Credential, cancel context.CancelFunc, stageSince, lastProgress *atomic.Int64, readyState *runtimeReadyState) {
	timing := r.options.Manifest.Timing
	ticker := time.NewTicker(min(timing.Renew, timing.Execution/2))
	defer ticker.Stop()
	lastKnown := time.Now()
	defer func() {
		release, stop := context.WithTimeout(context.Background(), timing.Renew)
		defer stop()
		_ = r.store.Release(release, c)
	}()
	defer readyState.lose(r)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		now := time.Now()
		since := stageSince.Load()
		if since > 0 && now.Sub(time.Unix(0, since)) >= timing.Execution {
			r.revoked.Add(1)
			cancel()
			return
		}
		probe, stop := context.WithTimeout(ctx, timing.Renew)
		observation, err := r.observe(probe, c)
		stop()
		if err != nil || observation.At.IsZero() || observation.At.After(now.Add(timing.Renew)) || time.Since(observation.At) > timing.Observation {
			observation = Observation{At: now, Delivered: -1, Unread: -1, Queued: -1, Due: -1}
		}
		if since > 0 && observation.Delivered == 0 {
			observation.Delivered = 1
		}
		if observation.Known() {
			lastKnown = time.Now()
			if !observation.HasWork() {
				lastProgress.Store(time.Now().UnixNano())
			}
		} else {
			r.unknown.Add(1)
		}
		if err := observation.Check(time.Now(), time.Unix(0, lastProgress.Load()), lastKnown, timing); err != nil {
			r.revoked.Add(1)
			cancel()
			return
		}
		renew, stop := context.WithTimeout(ctx, timing.Renew)
		err = r.store.Renew(renew, c, timing.Lease)
		stop()
		if err != nil {
			r.revoked.Add(1)
			cancel()
			return
		}
	}
}
