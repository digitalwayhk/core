// 本文件只采样既有WS会话作为恢复权威，不创建应用会话账或业务消息队列。
package melody

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/digitalwayhk/core/pkg/server/types"
	"sort"
)

func (mm *MelodyManager) captureOwnedReferences() ([]ownership.ScopedReference, int64, error) {
	mm.ownershipRevision.CompareAndSwap(0, 1)
	for attempt := 0; attempt < 4; attempt++ {
		before := mm.ownershipRevision.Load()
		mm.subscriptionsMu.RLock()
		sessions := make([]*SessionSubscriptions, 0, len(mm.subscriptions))
		for _, session := range mm.subscriptions {
			sessions = append(sessions, session)
		}
		mm.subscriptionsMu.RUnlock()
		refs := []ownership.ScopedReference{}
		seen := map[string]bool{}
		for _, session := range sessions {
			session.mu.RLock()
			for _, routers := range session.subscriptions {
				for _, api := range routers {
					ref, ok, err := ownedWebSocketReference(api)
					if err != nil {
						session.mu.RUnlock()
						return nil, 0, err
					}
					if !ok {
						continue
					}
					ref.SessionID = session.ownershipSessionID
					id := ref.Scope + "\x00" + ref.Key + "\x00" + ref.SessionID + "\x00" + ref.Name
					if !seen[id] {
						seen[id] = true
						refs = append(refs, ref)
					}
					if len(refs) > 1024 {
						session.mu.RUnlock()
						return nil, 0, ownership.ErrCapacity
					}
				}
			}
			session.mu.RUnlock()
		}
		if before == mm.ownershipRevision.Load() {
			sort.Slice(refs, func(i, j int) bool {
				a, b := refs[i], refs[j]
				return a.Scope+a.Key+a.SessionID+a.Name < b.Scope+b.Key+b.SessionID+b.Name
			})
			return refs, before, nil
		}
	}
	return nil, 0, ownership.ErrUnknown
}

func ownedWebSocketReference(api types.IRouter) (ref ownership.ScopedReference, ok bool, err error) {
	defer func() {
		if recover() != nil {
			err = ownership.ErrUnknown
		}
	}()
	source, ok := api.(types.IWebSocketOwnedReference)
	if !ok {
		return ref, false, nil
	}
	ref.Scope, ref.Key, ref.Name = source.WebSocketOwnedReference()
	if ref.Scope == "" || ref.Key == "" || ref.Name == "" || len(ref.Scope) > 512 || len(ref.Key) > 512 || len(ref.Name) > 512 {
		return ref, true, ownership.ErrUnknown
	}
	return ref, true, nil
}

func (s *SessionSubscriptions) markOwnershipChange() {
	if s.manage != nil {
		s.manage.ownershipRevision.Add(1)
		s.manage.ownershipMu.Lock()
		runtime := s.manage.ownershipReferences
		s.manage.ownershipMu.Unlock()
		if runtime != nil {
			select {
			case runtime.wake <- struct{}{}:
			default:
			}
		}
	}
}

func (mm *MelodyManager) ensureOwnershipReferences(ctx context.Context, ref ownership.ScopedReference) error {
	if mm == nil || mm.serviceContext == nil || mm.serviceContext.MQManager == nil {
		return ownership.ErrUnsupported
	}
	store, err := mm.serviceContext.MQManager.OwnershipStore(ctx)
	if err != nil {
		return err
	}
	if _, err := store.References(ctx, ref.Scope, ref.Key); err != nil {
		return err
	}
	mm.ownershipMu.Lock()
	defer mm.ownershipMu.Unlock()
	if mm.isClosed() {
		return ownership.ErrNotReady
	}
	if mm.ownershipReferences == nil {
		runtime, err := newGatewayReferenceRuntime(mm, store)
		if err != nil {
			return err
		}
		mm.ownershipReferences = runtime
	}
	return nil
}

func (mm *MelodyManager) flushOwnershipReferences() error {
	if mm == nil {
		return nil
	}
	mm.ownershipMu.Lock()
	runtime := mm.ownershipReferences
	mm.ownershipMu.Unlock()
	if runtime == nil {
		return nil
	}
	return runtime.flush()
}

func (mm *MelodyManager) stopOwnershipReferences() {
	if mm == nil {
		return
	}
	mm.ownershipMu.Lock()
	runtime := mm.ownershipReferences
	mm.ownershipMu.Unlock()
	if runtime != nil {
		<-runtime.done
	}
}
