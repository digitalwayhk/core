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

const (
	IdentityChangedEventType = "auth.casdoor.identity.changed"
	casdoorEventHookTimeout  = 3 * time.Second
)

type EventBridge interface {
	Subscribe(string, event.Handler) (func(), error)
	SubscribeExternal(context.Context, string) (func(), error)
	Publish(context.Context, event.PublishRequest) error
}

type managerOptions struct {
	events EventBridge
	hook   types.ICasdoorEventHookProvider
}

type Option func(*managerOptions)

// WithEventBridge 为撤销 Manager 绑定 ServiceContext 专属的本地/外部事件桥。
func WithEventBridge(bridge EventBridge) Option {
	return func(options *managerOptions) { options.events = bridge }
}

// WithCasdoorEventHook 为 Manager 配置持久化重试的业务事件 Hook。
func WithCasdoorEventHook(hook types.ICasdoorEventHookProvider) Option {
	return func(options *managerOptions) { options.hook = hook }
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
	hook           types.ICasdoorEventHookProvider
	hookWake       chan struct{}
	hookWG         sync.WaitGroup
	hookBackoff    []time.Duration
	hookSlots      chan struct{}
	hookTimeout    time.Duration
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
		manager.hook = resolved.hook
		if err := manager.bindEventBridge(resolved.events); err != nil {
			_ = manager.Close()
			return nil, err
		}
		manager.startHookWorker()
		return manager, nil
	}
	authority, err := NewConfiguredRedisStore(cfg.Redis)
	if err != nil {
		_ = snapshot.Close()
		return nil, err
	}
	manager := newManagerWithStores(service, authority, snapshot, true)
	manager.hook = resolved.hook
	if err := manager.bindEventBridge(resolved.events); err != nil {
		_ = manager.Close()
		return nil, err
	}
	manager.startHookWorker()
	return manager, nil
}

func newManagerWithStores(service string, authority, snapshot Store, shared bool) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		service: strings.TrimSpace(service), authority: authority, snapshot: snapshot, shared: shared,
		ctx: ctx, cancel: cancel, hookWake: make(chan struct{}, 1),
		hookBackoff: []time.Duration{time.Second, 5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute},
		hookSlots:   make(chan struct{}, 1), hookTimeout: casdoorEventHookTimeout,
	}
}

// ProcessEvent 原子应用权威状态，并等待可靠控制事件被 EventBridge 接受后再返回。
func (m *Manager) ProcessEvent(ctx context.Context, value types.CasdoorEvent, retention time.Duration) (ApplyResult, error) {
	result, err := m.ApplyEvent(ctx, value, retention)
	if err != nil {
		return ApplyResult{}, err
	}
	value.Generation = result.State.Generation
	value.Blocked = result.State.Blocked
	value.EventOrder = result.State.EventOrder
	if result.State.UID != "" {
		value.UID = result.State.UID
	}
	hookID := pendingHookID(value)
	if m.hook != nil && !result.ControlPublished {
		if err := m.SavePendingHook(ctx, PendingHook{ID: hookID, Event: value}); err != nil {
			return ApplyResult{}, err
		}
	}
	if result.ControlPublished {
		if m.hook != nil {
			if err := m.snapshot.MarkPendingHookReady(ctx, hookID); err != nil && !errors.Is(err, ErrEventNotFound) {
				return ApplyResult{}, ErrAuthorityUnavailable
			}
			m.wakeHookWorker()
		}
		return result, nil
	}
	if m.events == nil {
		return ApplyResult{}, ErrAuthorityUnavailable
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ApplyResult{}, ErrInvalidEvent
	}
	envelope := event.NewEnvelope(m.service, IdentityChangedEventType, data)
	envelope.ID = value.ID
	envelope.Subject = value.ProviderSubject
	envelope.IdempotencyKey = value.ID
	envelope.ShardKey = string(value.AuthType) + ":" + value.ProviderSubject
	if err := m.events.Publish(ctx, event.PublishRequest{
		Class: event.ControlDelivery, External: m.shared, Subject: IdentityChangedSubject(m.service), Envelope: envelope,
	}); err != nil {
		return ApplyResult{}, ErrAuthorityUnavailable
	}
	if err := m.MarkControlPublished(ctx, value); err != nil {
		return ApplyResult{}, err
	}
	result.ControlPublished = true
	if m.hook != nil {
		if err := m.snapshot.MarkPendingHookReady(ctx, hookID); err != nil {
			return ApplyResult{}, ErrAuthorityUnavailable
		}
		m.wakeHookWorker()
	}
	return result, nil
}

func pendingHookID(value types.CasdoorEvent) string {
	fingerprint, _ := eventFingerprint(value)
	return fingerprint
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

func (m *Manager) startHookWorker() {
	if m == nil || m.hook == nil {
		return
	}
	m.hookWG.Add(1)
	go m.runHookWorker()
	m.wakeHookWorker()
}

func (m *Manager) wakeHookWorker() {
	if m == nil || m.hook == nil || m.closing.Load() {
		return
	}
	select {
	case m.hookWake <- struct{}{}:
	default:
	}
}

func (m *Manager) runHookWorker() {
	defer m.hookWG.Done()
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.hookWake:
		case <-timer.C:
		}
		now := time.Now().UTC()
		_ = m.processPendingHooks(m.ctx, now)
		delay := m.nextHookDelay(m.ctx, now)
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(delay)
	}
}

func (m *Manager) processPendingHooks(ctx context.Context, now time.Time) error {
	if m == nil || m.hook == nil {
		return nil
	}
	if len(m.hookBackoff) == 0 {
		return errors.New("Casdoor事件Hook重试配置不能为空")
	}
	pending, err := m.PendingHooks(ctx, 64)
	if err != nil {
		return err
	}
	for _, item := range pending {
		if !item.Ready || item.NextAttempt.After(now) {
			continue
		}
		if err := invokeCasdoorEventHook(ctx, m.hook, item.Event, m.hookSlots, m.hookTimeout); err != nil {
			item.Attempts++
			index := item.Attempts - 1
			if index >= len(m.hookBackoff) {
				index = len(m.hookBackoff) - 1
			}
			item.NextAttempt = now.Add(m.hookBackoff[index])
			if saveErr := m.SavePendingHook(ctx, item); saveErr != nil {
				return saveErr
			}
			continue
		}
		if err := m.AckHook(ctx, item.ID); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) nextHookDelay(ctx context.Context, now time.Time) time.Duration {
	pending, err := m.PendingHooks(ctx, 64)
	if err != nil {
		return time.Minute
	}
	for _, item := range pending {
		if !item.Ready {
			continue
		}
		if !item.NextAttempt.After(now) {
			return time.Millisecond
		}
		return item.NextAttempt.Sub(now)
	}
	return time.Minute
}

func invokeCasdoorEventHook(
	ctx context.Context,
	hook types.ICasdoorEventHookProvider,
	value types.CasdoorEvent,
	slots chan struct{},
	timeout time.Duration,
) error {
	if timeout <= 0 || slots == nil {
		return errors.New("Casdoor事件Hook执行配置无效")
	}
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	default:
		return errors.New("Casdoor事件Hook上次执行尚未结束")
	}
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		defer func() { <-slots }()
		defer func() {
			if recover() != nil {
				result <- errors.New("Casdoor事件Hook panic")
			}
		}()
		result <- hook.OnCasdoorEvent(hookCtx, value)
	}()
	select {
	case err := <-result:
		return err
	case <-hookCtx.Done():
		return hookCtx.Err()
	}
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
	m.hookWG.Wait()
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
