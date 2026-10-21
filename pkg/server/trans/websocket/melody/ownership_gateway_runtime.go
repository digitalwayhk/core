// 本文件为已有WS会话建立唯一的框架快照心跳，断线后复用会话恢复，不要求客户端重订。
package melody

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/gofrs/uuid"
	"sync"
	"time"
)

type gatewayReferenceRuntime struct {
	manager     *MelodyManager
	store       ownership.Store
	mu          sync.Mutex
	gatewayID   string
	lastSuccess time.Time
	done        chan struct{}
	wake        chan struct{}
}

func newGatewayReferenceRuntime(manager *MelodyManager, store ownership.Store) (*gatewayReferenceRuntime, error) {
	r := &gatewayReferenceRuntime{manager: manager, store: store, done: make(chan struct{}), wake: make(chan struct{}, 1)}
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if err := r.open(ctx); err != nil {
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
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
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
	return nil
}

func (r *gatewayReferenceRuntime) run() {
	defer close(r.done)
	defer func() {
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
