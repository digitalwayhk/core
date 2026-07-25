package mq

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"
)

// MQManager 管理当前 MQ Provider、动态注册表和可选的无停机迁移状态。
type MQManager struct {
	mu        sync.RWMutex
	current   MQProvider
	registry  map[string]MQProvider
	switcher  *Switcher
	closed    bool
	closeOnce sync.Once
	closeErr  error
}

// NewManager returns an initialised MQManager with no active provider.
func NewManager() *MQManager {
	return &MQManager{registry: make(map[string]MQProvider)}
}

// Register adds a provider to the registry. It does not make it active.
func (m *MQManager) Register(p MQProvider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.registry[p.Name()] = p
}

// SetCurrent makes the named provider active. The provider must already be registered.
func (m *MQManager) SetCurrent(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrNotConnected
	}
	p, ok := m.registry[name]
	if !ok {
		return fmt.Errorf("mq: provider %q not registered", name)
	}
	m.current = p
	return nil
}

// Close disconnects every distinct registered provider and permanently closes
// the manager. Concurrent and repeated calls return the same joined error.
func (m *MQManager) Close() error {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		type providerEntry struct {
			key      string
			provider MQProvider
		}
		providers := make([]providerEntry, 0, len(m.registry)+1)
		for key, provider := range m.registry {
			providers = append(providers, providerEntry{key: key, provider: provider})
		}
		if m.current != nil {
			providers = append(providers, providerEntry{key: m.current.Name(), provider: m.current})
		}
		sort.SliceStable(providers, func(i, j int) bool {
			if providers[i].key != providers[j].key {
				return providers[i].key < providers[j].key
			}
			return providers[i].provider.Name() < providers[j].provider.Name()
		})
		m.current = nil
		m.registry = make(map[string]MQProvider)
		m.mu.Unlock()

		type providerPointer struct {
			typ     reflect.Type
			pointer uintptr
		}
		seenPointers := make(map[providerPointer]struct{}, len(providers))
		var closeErrors []error
		for _, entry := range providers {
			provider := entry.provider
			if provider == nil {
				continue
			}
			value := reflect.ValueOf(provider)
			if value.Kind() == reflect.Ptr && !value.IsNil() {
				pointer := providerPointer{typ: value.Type(), pointer: value.Pointer()}
				if _, exists := seenPointers[pointer]; exists {
					continue
				}
				seenPointers[pointer] = struct{}{}
			}
			name := provider.Name()
			if err := provider.Close(); err != nil {
				closeErrors = append(closeErrors, fmt.Errorf("mq: close provider %q: %w", name, err))
			}
		}
		m.closeErr = errors.Join(closeErrors...)
	})
	return m.closeErr
}

// Current returns a snapshot of the active provider, or nil if none is set.
// Calls made directly on that snapshot are not coordinated by the Manager's
// lifecycle gate and may overlap with Close.
func (m *MQManager) Current() MQProvider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// Health returns nil when the active provider is healthy.
func (m *MQManager) Health(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed || m.current == nil {
		return ErrNotConnected
	}
	return m.current.Health(ctx)
}

// Publish 将消息交给当前 Provider；迁移双写阶段会透明地改由 Switcher 发布。
func (m *MQManager) Publish(ctx context.Context, subject string, data []byte, opts *PublishOptions) error {
	m.mu.RLock()
	if m.closed || m.current == nil {
		m.mu.RUnlock()
		return ErrNotConnected
	}
	switcher := m.switcher
	if switcher == nil {
		defer m.mu.RUnlock()
		return m.current.Publish(ctx, subject, data, opts)
	}
	m.mu.RUnlock()

	if switcher.Stage() == SwitchStageDoubleWrite {
		return switcher.DoubleWritePublish(ctx, subject, data, opts)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed || m.current == nil {
		return ErrNotConnected
	}
	return m.current.Publish(ctx, subject, data, opts)
}

// Subscribe delegates to the active provider.
func (m *MQManager) Subscribe(ctx context.Context, subject string, handler func(*Message)) (func(), error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed || m.current == nil {
		return nil, ErrNotConnected
	}
	return m.current.Subscribe(ctx, subject, handler)
}

// SubscribeReliable 仅在当前 Provider 明确实现 ReliableMQProvider 时启用。
func (m *MQManager) SubscribeReliable(
	ctx context.Context,
	subject string,
	options ReliableSubscribeOptions,
	handler func(*Message) error,
) (func(), error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed || m.current == nil {
		return nil, ErrNotConnected
	}
	provider, ok := m.current.(ReliableMQProvider)
	if !ok {
		return nil, ErrReliableSubscribeUnsupported
	}
	return provider.SubscribeReliable(ctx, subject, options, handler)
}

// OrderedReliableInfo 返回当前 provider 的 ordered-reliable 声明；未实现则 ok=false。
func (m *MQManager) OrderedReliableInfo() (OrderedReliableCapability, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed || m.current == nil {
		return OrderedReliableCapability{}, false
	}
	provider, ok := m.current.(OrderedReliableMQProvider)
	if !ok {
		return OrderedReliableCapability{}, false
	}
	info := provider.OrderedReliableInfo()
	return info, info.Valid()
}

// RequireOrderedReliable 校验当前 provider 已声明并具备合法的 ordered-reliable 能力。
func (m *MQManager) RequireOrderedReliable() error {
	info, ok := m.OrderedReliableInfo()
	if !ok || !info.Valid() {
		return ErrOrderedReliableUnsupported
	}
	return nil
}

// BeginSwitch 启动到 newProvider 的无停机迁移。
// 连接前先占用迁移槽，避免并发请求启动第二次迁移。
func (m *MQManager) BeginSwitch(ctx context.Context, newProvider MQProvider, rollbackOnFailure bool) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrNotConnected
	}
	if m.switcher != nil {
		m.mu.Unlock()
		return fmt.Errorf("mq: migration already in progress")
	}
	switcher := NewSwitcher(m, rollbackOnFailure)
	m.switcher = switcher
	m.mu.Unlock()

	if err := switcher.Begin(ctx, newProvider); err != nil {
		m.mu.Lock()
		if m.switcher == switcher {
			m.switcher = nil
		}
		m.mu.Unlock()
		return err
	}
	return nil
}

// GetSwitcher 返回当前迁移控制器；没有迁移时返回 nil。
func (m *MQManager) GetSwitcher() *Switcher {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.switcher
}

// CompleteSwitch 推进当前迁移并将新 Provider 切换为当前 Provider。
func (m *MQManager) CompleteSwitch(ctx context.Context, drainDelay time.Duration) error {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return ErrNotConnected
	}
	switcher := m.switcher
	m.mu.RUnlock()
	if switcher == nil {
		return fmt.Errorf("mq: no migration in progress")
	}
	if err := switcher.AdvanceToCatchUp(); err != nil {
		return err
	}
	if err := switcher.AdvanceToReadNew(); err != nil {
		return err
	}
	if err := switcher.Complete(ctx, drainDelay); err != nil {
		return err
	}
	m.mu.Lock()
	if m.switcher == switcher {
		m.switcher = nil
	}
	m.mu.Unlock()
	return nil
}

// RollbackSwitch 中止当前迁移并恢复旧 Provider。
func (m *MQManager) RollbackSwitch() error {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return ErrNotConnected
	}
	switcher := m.switcher
	m.mu.RUnlock()
	if switcher == nil {
		return fmt.Errorf("mq: no migration in progress")
	}
	if err := switcher.Rollback(); err != nil {
		return err
	}
	m.mu.Lock()
	if m.switcher == switcher {
		m.switcher = nil
	}
	m.mu.Unlock()
	return nil
}
