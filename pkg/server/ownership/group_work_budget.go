// 本文件是原Runtime共用的固定执行面；不公开应用提交接口，不保存业务任务副本。
package ownership

import (
	"context"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// GroupWorkBudget由同实例所有组Runtime共享；应用只声明预算，提交由Runtime内部执行。
type GroupWorkBudget struct {
	instance        string
	options         GroupWorkBudgetOptions
	ctx             context.Context
	cancel          context.CancelFunc
	mu              sync.Mutex
	closing         bool
	service         string
	jobs            chan *groupWorkJob
	permits         chan struct{}
	held            map[groupWorkIdentity]struct{}
	leases          map[*groupWorkLease]struct{}
	workers         sync.WaitGroup
	attachments     sync.WaitGroup
	once            sync.Once
	done            chan struct{}
	active          atomic.Int64
	reserved        atomic.Int64
	queued          atomic.Int64
	started         atomic.Uint64
	completed       atomic.Uint64
	failed          atomic.Uint64
	noProgress      atomic.Uint64
	selections      atomic.Uint64
	selectionErrors atomic.Uint64
	timeouts        atomic.Uint64
	capacityWaits   atomic.Uint64
	queueNanos      atomic.Uint64
	handlerNanos    atomic.Uint64
	selectionNanos  atomic.Uint64
}

// NewGroupWorkBudget创建一个实例共用预算；普通Step不需要创建，实例不得每scope各建一份。
func NewGroupWorkBudget(instanceID string, o GroupWorkBudgetOptions) (*GroupWorkBudget, error) {
	if strings.TrimSpace(instanceID) == "" || len(instanceID) > 512 {
		return nil, ErrUnknown
	}
	if err := o.validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	b := &GroupWorkBudget{instance: instanceID, options: o, ctx: ctx, cancel: cancel, jobs: make(chan *groupWorkJob, o.Prefetch), permits: make(chan struct{}, o.Prefetch), held: map[groupWorkIdentity]struct{}{}, leases: map[*groupWorkLease]struct{}{}, done: make(chan struct{})}
	for i := 0; i < o.Prefetch; i++ {
		b.permits <- struct{}{}
	}
	b.workers.Add(o.Workers)
	for i := 0; i < o.Workers; i++ {
		go func() {
			defer b.workers.Done()
			for j := range b.jobs {
				b.queued.Add(-1)
				j.run()
			}
		}()
	}
	return b, nil
}
func (b *GroupWorkBudget) valid(instance, service string) error {
	if b == nil || b.instance != instance {
		return ErrUnknown
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closing {
		return ErrNotReady
	}
	if b.service != "" && b.service != service {
		return ErrUnknown
	}
	b.service = service
	return nil
}
func (b *GroupWorkBudget) attach(l *groupWorkLease) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closing {
		return ErrNotReady
	}
	if len(b.leases) >= 4096 {
		return ErrCapacity
	}
	if b.service != "" && b.service != l.c.Service {
		return ErrUnknown
	}
	b.service = l.c.Service
	b.leases[l] = struct{}{}
	b.attachments.Add(1)
	return nil
}
func (b *GroupWorkBudget) detach(l *groupWorkLease) {
	b.mu.Lock()
	if _, ok := b.leases[l]; ok {
		delete(b.leases, l)
		b.attachments.Done()
	}
	b.mu.Unlock()
}
func (b *GroupWorkBudget) acquire(ctx context.Context) (int, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-b.ctx.Done():
		return 0, ErrNotReady
	case <-b.permits:
	default:
		b.capacityWaits.Add(1)
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-b.ctx.Done():
			return 0, ErrNotReady
		case <-b.permits:
		}
	}
	n := 1
	b.reserved.Add(1)
	for n < b.options.Workers {
		select {
		case <-b.permits:
			n++
			b.reserved.Add(1)
		default:
			return n, nil
		}
	}
	return n, nil
}
func (b *GroupWorkBudget) release(n int) {
	for i := 0; i < n; i++ {
		b.reserved.Add(-1)
		b.permits <- struct{}{}
	}
}
func (b *GroupWorkBudget) excluded(c Credential) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	groups := []string{}
	for id := range b.held {
		if id.scope == c.Scope && id.key == c.Key {
			groups = append(groups, id.group)
		}
	}
	sort.Strings(groups)
	return groups
}
func (b *GroupWorkBudget) enqueue(l *groupWorkLease, groups []string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closing || l.ctx.Err() != nil {
		return ErrNotReady
	}
	seen := map[string]bool{}
	for _, g := range groups {
		if g == "" || len(g) > 512 || seen[g] {
			return ErrUnknown
		}
		seen[g] = true
		if _, ok := b.held[groupWorkIdentity{l.c.Scope, l.c.Key, g}]; ok {
			return ErrBusy
		}
	}
	for _, g := range groups {
		id := groupWorkIdentity{l.c.Scope, l.c.Key, g}
		b.held[id] = struct{}{}
		l.pending.Add(1)
		l.jobs.Add(1)
		b.queued.Add(1)
		b.jobs <- &groupWorkJob{lease: l, group: g, identity: id, queuedAt: nowWorkTime()}
	}
	return nil
}

// Close停止准入、取消登记租约，并等待固定worker与所有真实回调退出；超时不代表退出。
func (b *GroupWorkBudget) Close(ctx context.Context) error {
	if b == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	b.once.Do(func() {
		b.mu.Lock()
		b.closing = true
		b.cancel()
		for l := range b.leases {
			l.cancel()
		}
		close(b.jobs)
		b.mu.Unlock()
		go func() { b.workers.Wait(); b.attachments.Wait(); close(b.done) }()
	})
	select {
	case <-b.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Done仅在全部固定worker和登记租约的真实回调退出后关闭。
func (b *GroupWorkBudget) Done() <-chan struct{} { return b.done }
