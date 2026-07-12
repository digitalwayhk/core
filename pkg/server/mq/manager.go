package mq

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
)

// MQManager holds the active provider and allows dynamic registration.
type MQManager struct {
	mu        sync.RWMutex
	current   MQProvider
	registry  map[string]MQProvider
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
		providers := make([]MQProvider, 0, len(m.registry)+1)
		if m.current != nil {
			providers = append(providers, m.current)
		}
		for _, provider := range m.registry {
			providers = append(providers, provider)
		}
		m.current = nil
		m.registry = make(map[string]MQProvider)
		m.mu.Unlock()

		seenPointers := make(map[uintptr]struct{}, len(providers))
		var closeErrors []error
		for _, provider := range providers {
			if provider == nil {
				continue
			}
			value := reflect.ValueOf(provider)
			if value.Kind() == reflect.Ptr && !value.IsNil() {
				pointer := value.Pointer()
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

// Current returns the active provider, or nil if none is set.
func (m *MQManager) Current() MQProvider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// Health returns nil when the active provider is healthy.
func (m *MQManager) Health(ctx context.Context) error {
	p := m.Current()
	if p == nil {
		return ErrNotConnected
	}
	return p.Health(ctx)
}

// Publish delegates to the active provider.
func (m *MQManager) Publish(ctx context.Context, subject string, data []byte, opts *PublishOptions) error {
	p := m.Current()
	if p == nil {
		return ErrNotConnected
	}
	return p.Publish(ctx, subject, data, opts)
}

// Subscribe delegates to the active provider.
func (m *MQManager) Subscribe(ctx context.Context, subject string, handler func(*Message)) (func(), error) {
	p := m.Current()
	if p == nil {
		return nil, ErrNotConnected
	}
	return p.Subscribe(ctx, subject, handler)
}
