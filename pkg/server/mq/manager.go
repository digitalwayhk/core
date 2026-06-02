package mq

import (
	"context"
	"fmt"
	"sync"
)

// MQManager holds the active provider and allows dynamic registration.
type MQManager struct {
	mu       sync.RWMutex
	current  MQProvider
	registry map[string]MQProvider
}

// NewManager returns an initialised MQManager with no active provider.
func NewManager() *MQManager {
	return &MQManager{registry: make(map[string]MQProvider)}
}

// Register adds a provider to the registry. It does not make it active.
func (m *MQManager) Register(p MQProvider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registry[p.Name()] = p
}

// SetCurrent makes the named provider active. The provider must already be registered.
func (m *MQManager) SetCurrent(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.registry[name]
	if !ok {
		return fmt.Errorf("mq: provider %q not registered", name)
	}
	m.current = p
	return nil
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
