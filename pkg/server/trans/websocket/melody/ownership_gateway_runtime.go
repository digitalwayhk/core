// 本文件为已有WS会话建立唯一的框架快照心跳，断线后复用会话恢复，不要求客户端重订。
package melody

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/gofrs/uuid"
	"sync"
	"time"
)

type gatewayReferenceRuntime struct {
	ctx         context.Context
	cancel      context.CancelFunc
	manager     *MelodyManager
	store       ownership.Store
	mu          sync.Mutex
	gatewayID   string
	lastSuccess time.Time
	done        chan struct{}
	wake        chan struct{}
}

func newGatewayReferenceRuntime(manager *MelodyManager, store ownership.Store) (*gatewayReferenceRuntime, error) {
	lifetime, cancel := context.WithCancel(context.Background())
	r := &gatewayReferenceRuntime{ctx: lifetime, cancel: cancel, manager: manager, store: store, done: make(chan struct{}), wake: make(chan struct{}, 1)}
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if err := r.open(ctx); err != nil {
		cancel()
		return nil, err
	}
	go r.run()
	return r, nil
}

func (r *gatewayReferenceRuntime) open(ctx context.Context) error {
	identity, err := uuid.NewV4()
	if err != nil {
		return err
	}
	id := identity.String()
	if err := r.store.OpenGateway(ctx, id, 15*time.Second); err != nil {
		return err
	}
	r.gatewayID = id
	r.lastSuccess = time.Now()
	return nil
}

func (r *gatewayReferenceRuntime) flush() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ctx.Err(); err != nil {
		return err
	}
	ctx, stop := context.WithTimeout(r.ctx, 3*time.Second)
	defer stop()
	if time.Since(r.lastSuccess) >= 15*time.Second {
		if err := r.open(ctx); err != nil {
			return err
		}
	}
	refs, revision, err := r.manager.captureOwnedReferences()
	if err != nil {
		return err
	}
	if err := r.store.ReplaceGateway(ctx, ownership.GatewaySnapshot{GatewayID: r.gatewayID, Revision: revision, References: refs, Lease: 15 * time.Second}); err != nil {
		return err
	}
	r.lastSuccess = time.Now()
	if sc := r.manager.serviceContext; sc != nil && sc.Service != nil && len(refs) > 0 {
		// 与ServiceContext启停/切换使用同一服务级同步注册表，不读取会被置空的公开指针。
		if replay, ok := types.GetCrossNodeForwarderForService(sc.Service.Name).(interface{ ReplayLocalSubscriptions(context.Context) error }); ok {
			return replay.ReplayLocalSubscriptions(ctx)
		}
	}
	return nil
}

func (r *gatewayReferenceRuntime) run() {
	defer close(r.done)
	defer func() {
		r.cancel() // 先拒绝迟到刷新，再等待当前刷新退出和关闭lease。
		r.mu.Lock()
		id := r.gatewayID
		r.mu.Unlock()
		ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_ = r.store.CloseGateway(ctx, id)
	}()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.manager.closeChan:
			return
		case <-ticker.C:
			_ = r.flush()
		case <-r.wake:
			_ = r.flush()
		}
	}
}
