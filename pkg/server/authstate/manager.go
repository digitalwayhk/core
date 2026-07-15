package authstate

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// Manager 将权威撤销状态与本地已确认快照组合为请求授权边界。
type Manager struct {
	service   string
	authority Store
	snapshot  Store
	shared    bool
	closeOnce sync.Once
	closeErr  error
}

func NewManager(service string, cfg config.AuthRevocationConfig) (*Manager, error) {
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
	if cfg.Mode != config.AuthRevocationModeShared {
		return newManagerWithStores(service, snapshot, snapshot, false), nil
	}
	authority, err := NewConfiguredRedisStore(cfg.Redis)
	if err != nil {
		_ = snapshot.Close()
		return nil, err
	}
	return newManagerWithStores(service, authority, snapshot, true), nil
}

func newManagerWithStores(service string, authority, snapshot Store, shared bool) *Manager {
	return &Manager{service: strings.TrimSpace(service), authority: authority, snapshot: snapshot, shared: shared}
}

func (m *Manager) Authorize(ctx context.Context, identity types.AuthIdentity) error {
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
	if err := m.authority.MarkControlPublished(ctx, event); err != nil {
		if errors.Is(err, ErrEventNotFound) {
			return err
		}
		return ErrAuthorityUnavailable
	}
	return nil
}

func (m *Manager) SavePendingHook(ctx context.Context, hook PendingHook) error {
	if m.snapshot == nil {
		return ErrAuthorityUnavailable
	}
	if err := m.snapshot.SavePendingHook(ctx, hook); err != nil {
		return ErrAuthorityUnavailable
	}
	return nil
}

func (m *Manager) PendingHooks(ctx context.Context, limit int) ([]PendingHook, error) {
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
	if m.snapshot == nil {
		return ErrAuthorityUnavailable
	}
	if err := m.snapshot.AckHook(ctx, id); err != nil {
		return ErrAuthorityUnavailable
	}
	return nil
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
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
