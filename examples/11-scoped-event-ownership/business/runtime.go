// Package business 只定义演示的事务与恢复动作，消费循环、租约和WS会话账均由Core管理。
package business

import (
	"context"
	"encoding/json"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/api/dto"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/contract"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/models"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/digitalwayhk/core/pkg/server/router"
	"os"
	"sync"
)

// Runtime 是实例注入的应用回调状态，只保存恢复视图的计数，不另建会话账。
type Runtime struct {
	Store      *models.Store
	Context    *router.ServiceContext
	Owned      *ownership.Runtime
	Notify     func(*dto.Snapshot)
	mu         sync.RWMutex
	references map[string]ownership.ReferenceSnapshot
}

// New 创建有限演示键的实例状态。
func New(store *models.Store) *Runtime {
	return &Runtime{Store: store, references: map[string]ownership.ReferenceSnapshot{}}
}

// Key 从原事件数据读取应用稳定键，不修改OrderingKey、EventID或payload。
func (*Runtime) Key(e *event.Envelope) (string, error) {
	var v struct {
		Key string `json:"key"`
	}
	err := json.Unmarshal(e.Data, &v)
	return v.Key, err
}

// Restore 接收Core已经收敛的跨Gateway全量引用，不启动应用消费worker。
func (r *Runtime) Restore(_ context.Context, c ownership.Credential, s ownership.ReferenceSnapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old := r.references[c.Key]; old.Revision > s.Revision {
		return ownership.ErrStaleSnapshot
	}
	r.references[c.Key] = s
	return nil
}

// Handle 真实事务提交后发通知，返回成功才交给Provider做fenced ACK。
func (r *Runtime) Handle(ctx context.Context, c ownership.Credential, rows []*event.Envelope) error {
	if err := r.Store.Apply(ctx, c, rows); err != nil {
		return err
	}
	count, err := r.Store.Count(ctx, c.Key)
	if err != nil {
		return err
	}
	if r.Notify != nil {
		r.Notify(r.snapshot(c.Key, c.Epoch, c.InstanceID, count))
	}
	return nil
}

func (r *Runtime) snapshot(key string, epoch int64, instance string, count int64) *dto.Snapshot {
	r.mu.RLock()
	refs := r.references[key]
	r.mu.RUnlock()
	return &dto.Snapshot{Key: key, Epoch: epoch, InstanceID: instance, Count: count, PID: os.Getpid(), References: len(refs.References), ReferenceRevision: refs.Revision}
}

// Read 拒绝本实例非owner或未恢复状态；调用方同时复核Ready租约。
func (r *Runtime) Read(ctx context.Context, key string) (*dto.Snapshot, error) {
	if r.Owned == nil {
		return nil, ownership.ErrNotReady
	}
	status, err := r.Owned.Status(ctx, key)
	if err != nil {
		return nil, err
	}
	store, err := r.Context.MQManager.OwnershipStore(ctx)
	if err != nil {
		return nil, err
	}
	c, err := store.Resolve(ctx, contract.Scope, key)
	if err != nil {
		return nil, err
	}
	if c.InstanceID != status.InstanceID || c.Epoch != status.Epoch {
		return nil, ownership.ErrFenced
	}
	count, err := r.Store.ReadFenced(ctx, c)
	if err != nil {
		return nil, err
	}
	return r.snapshot(key, status.Epoch, status.InstanceID, count), nil
}

// IsCurrentNotice 拒绝旧owner迟到通知，不将本地存活视为归属证明。
func (r *Runtime) IsCurrentNotice(ctx context.Context, s *dto.Snapshot) bool {
	store, err := r.Context.MQManager.OwnershipStore(ctx)
	if err != nil {
		return false
	}
	c, err := store.Resolve(ctx, contract.Scope, s.Key)
	return err == nil && c.Epoch == s.Epoch && c.InstanceID == s.InstanceID
}
