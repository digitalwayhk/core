package authstate

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/types"
)

const IdentityChangedEventType = "auth.casdoor.identity.changed"

type EventBridge interface {
	Subscribe(string, event.Handler) (func(), error)
	SubscribeExternal(context.Context, string) (func(), error)
}

type managerOptions struct {
	events EventBridge
}

type Option func(*managerOptions)

// WithEventBridge 为撤销 Manager 绑定 ServiceContext 专属的本地/外部事件桥。
func WithEventBridge(bridge EventBridge) Option {
	return func(options *managerOptions) { options.events = bridge }
}

// IdentityChangedSubject 返回服务专属的 Casdoor 身份控制主题。
func IdentityChangedSubject(service string) string {
	return strings.TrimSpace(service) + ".auth.casdoor.identity.changed"
}

// Manager 将权威撤销状态与本地已确认快照组合为请求授权边界。
type Manager struct {
	service        string
	authority      Store
	snapshot       Store
	shared         bool
	events         EventBridge
	ctx            context.Context
	cancel         context.CancelFunc
	closing        atomic.Bool
	subMu          sync.Mutex
	localCancel    func()
	externalCancel func()
	closeOnce      sync.Once
	closeErr       error
}

func NewManager(service string, cfg config.AuthRevocationConfig, options ...Option) (*Manager, error) {
	if strings.TrimSpace(service) == "" {
		return nil, errors.New("认证撤销服务名不能为空")
	}
	if cfg.Mode != config.AuthRevocationModeLocal && cfg.Mode != config.AuthRevocationModeShared {
		return nil, errors.New("认证撤销模式无效")
	}
	snapshot, err := OpenBadgerStore(cfg.BadgerPath)
	if err != nil {
		return nil, err
	}
	resolved := managerOptions{}
	for _, apply := range options {
		if apply != nil {
			apply(&resolved)
		}
	}
	if cfg.Mode != config.AuthRevocationModeShared {
		manager := newManagerWithStores(service, snapshot, snapshot, false)
		if err := manager.bindEventBridge(resolved.events); err != nil {
			_ = manager.Close()
			return nil, err
		}
		return manager, nil
	}
	authority, err := NewConfiguredRedisStore(cfg.Redis)
	if err != nil {
		_ = snapshot.Close()
		return nil, err
	}
	manager := newManagerWithStores(service, authority, snapshot, true)
	if err := manager.bindEventBridge(resolved.events); err != nil {
		_ = manager.Close()
		return nil, err
	}
	return manager, nil
}

func newManagerWithStores(service string, authority, snapshot Store, shared bool) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{service: strings.TrimSpace(service), authority: authority, snapshot: snapshot, shared: shared, ctx: ctx, cancel: cancel}
}

func (m *Manager) Authorize(ctx context.Context, identity types.AuthIdentity) error {
	if m == nil || m.closing.Load() {
		return ErrAuthorityUnavailable
	}
	if identity.Provider != types.AuthProviderCasdoor {
		return nil
	}
	key := identityKey(m.service, identity)
	if err := key.validate(); err != nil || identity.UID == "" {
		return ErrIdentityRevoked
	}
	state, err := m.authority.Current(ctx, key)
	if err != nil {
		return ErrAuthorityUnavailable
	}
	if state.Blocked || state.Generation != identity.Generation {
		return ErrIdentityRevoked
	}
	if m.shared && m.snapshot != nil {
		if err := m.snapshot.SaveSnapshot(ctx, state); err != nil {
			return ErrAuthorityUnavailable
		}
	}
	return nil
}

func (m *Manager) Current(ctx context.Context, identity types.AuthIdentity) (State, error) {
	if m == nil || m.closing.Load() {
		return State{}, ErrAuthorityUnavailable
	}
	key := identityKey(m.service, identity)
	if err := key.validate(); err != nil {
		return State{}, err
	}
	state, err := m.authority.Current(ctx, key)
	if err != nil {
		return State{}, ErrAuthorityUnavailable
	}
	if m.shared && m.snapshot != nil {
		if err := m.snapshot.SaveSnapshot(ctx, state); err != nil {
			return State{}, ErrAuthorityUnavailable
		}
	}
	return state, nil
}

func (m *Manager) ApplyEvent(ctx context.Context, event types.CasdoorEvent, retention time.Duration) (ApplyResult, error) {
	if m == nil || m.closing.Load() {
		return ApplyResult{}, ErrAuthorityUnavailable
	}
	if event.ServiceName != m.service {
		return ApplyResult{}, ErrInvalidEvent
	}
	result, err := m.authority.Apply(ctx, event, retention)
	if err != nil {
		if errors.Is(err, ErrInvalidEvent) {
			return ApplyResult{}, err
		}
		return ApplyResult{}, ErrAuthorityUnavailable
	}
	if m.shared && m.snapshot != nil {
		if err := m.snapshot.SaveSnapshot(ctx, result.State); err != nil {
			return ApplyResult{}, ErrAuthorityUnavailable
		}
	}
	return result, nil
}

func (m *Manager) ConfirmActive(ctx context.Context, identity types.AuthIdentity, expectedGeneration uint64) (State, error) {
	if m == nil || m.closing.Load() {
		return State{}, ErrAuthorityUnavailable
	}
	key := identityKey(m.service, identity)
	if err := key.validate(); err != nil {
		return State{}, err
	}
	state, err := m.authority.ConfirmActive(ctx, key, expectedGeneration)
	if errors.Is(err, ErrGenerationChanged) {
		return State{}, err
	}
	if err != nil {
		return State{}, ErrAuthorityUnavailable
	}
	if m.shared && m.snapshot != nil {
		if err := m.snapshot.SaveSnapshot(ctx, state); err != nil {
			return State{}, ErrAuthorityUnavailable
		}
	}
	return state, nil
}

func (m *Manager) MarkControlPublished(ctx context.Context, event types.CasdoorEvent) error {
	if m == nil || m.closing.Load() {
		return ErrAuthorityUnavailable
	}
	if err := m.authority.MarkControlPublished(ctx, event); err != nil {
		if errors.Is(err, ErrEventNotFound) {
			return err
		}
		return ErrAuthorityUnavailable
	}
	return nil
}

func (m *Manager) SavePendingHook(ctx context.Context, hook PendingHook) error {
	if m == nil || m.closing.Load() {
		return ErrAuthorityUnavailable
	}
	if m.snapshot == nil {
		return ErrAuthorityUnavailable
	}
	if err := m.snapshot.SavePendingHook(ctx, hook); err != nil {
		return ErrAuthorityUnavailable
	}
	return nil
}

func (m *Manager) PendingHooks(ctx context.Context, limit int) ([]PendingHook, error) {
	if m == nil || m.closing.Load() {
		return nil, ErrAuthorityUnavailable
	}
	if m.snapshot == nil {
		return nil, ErrAuthorityUnavailable
	}
	hooks, err := m.snapshot.PendingHooks(ctx, limit)
	if err != nil {
		return nil, ErrAuthorityUnavailable
	}
	return hooks, nil
}

func (m *Manager) AckHook(ctx context.Context, id string) error {
	if m == nil || m.closing.Load() {
		return ErrAuthorityUnavailable
	}
	if m.snapshot == nil {
		return ErrAuthorityUnavailable
	}
	if err := m.snapshot.AckHook(ctx, id); err != nil {
		return ErrAuthorityUnavailable
	}
	return nil
}

func (m *Manager) bindEventBridge(bridge EventBridge) error {
	if m == nil || m.closing.Load() {
		return ErrAuthorityUnavailable
	}
	if bridge == nil {
		if m.shared {
			return errors.New("共享认证撤销需要外部EventBridge")
		}
		return nil
	}
	localCancel, err := bridge.Subscribe(IdentityChangedEventType, m.handleIdentityChanged)
	if err != nil {
		return err
	}
	var externalCancel func()
	if m.shared {
		externalCancel, err = bridge.SubscribeExternal(m.ctx, IdentityChangedSubject(m.service))
		if err != nil {
			localCancel()
			return err
		}
	}
	m.subMu.Lock()
	m.events = bridge
	m.localCancel = localCancel
	m.externalCancel = externalCancel
	m.subMu.Unlock()
	return nil
}

func (m *Manager) handleIdentityChanged(envelope *event.Envelope) {
	if m == nil || m.closing.Load() || envelope == nil {
		return
	}
	payload := types.CasdoorEvent{}
	if json.Unmarshal(envelope.Data, &payload) != nil || payload.ServiceName != m.service || payload.Provider != types.AuthProviderCasdoor {
		return
	}
	state := State{
		Key: eventIdentityKey(payload), Generation: payload.Generation, Blocked: payload.Blocked,
		EventOrder: payload.EventOrder, UID: payload.UID, UpdatedAt: time.Now().UTC(),
	}
	if m.snapshot != nil {
		_ = m.snapshot.SaveSnapshot(m.ctx, state)
	}
}

// BeginClose 立即拒绝新认证并停止事件订阅，但保留存储到 ServiceContext 完成其他组件清理。
func (m *Manager) BeginClose() {
	if m == nil || !m.closing.CompareAndSwap(false, true) {
		return
	}
	if m.cancel != nil {
		m.cancel()
	}
	m.subMu.Lock()
	localCancel := m.localCancel
	externalCancel := m.externalCancel
	m.localCancel = nil
	m.externalCancel = nil
	m.subMu.Unlock()
	if externalCancel != nil {
		externalCancel()
	}
	if localCancel != nil {
		localCancel()
	}
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	m.BeginClose()
	m.closeOnce.Do(func() {
		if m.authority != nil {
			m.closeErr = m.authority.Close()
		}
		if m.shared && m.snapshot != nil {
			if err := m.snapshot.Close(); m.closeErr == nil {
				m.closeErr = err
			}
		}
	})
	return m.closeErr
}
