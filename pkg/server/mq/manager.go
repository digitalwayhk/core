package mq

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MQManager holds the active provider and allows dynamic registration.
// It also embeds an optional Switcher for zero-downtime provider migrations;
// when a switch is in progress, Publish automatically uses double-write.
type MQManager struct {
	mu       sync.RWMutex
	current  MQProvider
	registry map[string]MQProvider
	switcher *Switcher // non-nil only while a migration is in progress
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

// Publish delivers data to the active provider.
// During the double-write phase of a live migration it transparently publishes
// to both old and new providers via the embedded Switcher.
func (m *MQManager) Publish(ctx context.Context, subject string, data []byte, opts *PublishOptions) error {
	m.mu.RLock()
	sw := m.switcher
	m.mu.RUnlock()

	if sw != nil && sw.Stage() == SwitchStageDoubleWrite {
		return sw.DoubleWritePublish(ctx, subject, data, opts)
	}

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

// ============================================================
// Live migration helpers
// ============================================================

// BeginSwitch starts a zero-downtime migration to newProvider.
// After calling BeginSwitch, all Publish calls automatically double-write
// to both old and new providers until AdvanceSwitchPhase or CompleteSwitch
// finalises the migration.
//
// Returns an error if a migration is already in progress or newProvider
// cannot connect.
func (m *MQManager) BeginSwitch(ctx context.Context, newProvider MQProvider, rollbackOnFailure bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.switcher != nil && m.switcher.Stage() != SwitchStageIdle {
		return fmt.Errorf("mq: migration already in progress (stage=%s)", m.switcher.Stage())
	}
	sw := NewSwitcher(m, rollbackOnFailure)
	if err := sw.Begin(ctx, newProvider); err != nil {
		return err
	}
	m.switcher = sw
	return nil
}

// GetSwitcher returns the active Switcher so callers can advance phases manually.
// Returns nil when no migration is in progress.
func (m *MQManager) GetSwitcher() *Switcher {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.switcher
}

// CompleteSwitch advances through the remaining migration phases and finalises
// the switch to the new provider, waiting up to drainDelay for in-flight
// messages on the old provider to be processed before closing it.
//
// Phase sequence driven here: catch-up → read-new → complete.
// Callers that need fine-grained control can use GetSwitcher() directly.
func (m *MQManager) CompleteSwitch(ctx context.Context, drainDelay time.Duration) error {
	m.mu.RLock()
	sw := m.switcher
	m.mu.RUnlock()
	if sw == nil {
		return fmt.Errorf("mq: no migration in progress")
	}
	if err := sw.AdvanceToCatchUp(); err != nil {
		return err
	}
	if err := sw.AdvanceToReadNew(); err != nil {
		return err
	}
	if err := sw.Complete(ctx, drainDelay); err != nil {
		return err
	}
	m.mu.Lock()
	m.switcher = nil
	m.mu.Unlock()
	return nil
}

// RollbackSwitch aborts the current migration and restores the previous provider.
func (m *MQManager) RollbackSwitch() error {
	m.mu.RLock()
	sw := m.switcher
	m.mu.RUnlock()
	if sw == nil {
		return fmt.Errorf("mq: no migration in progress")
	}
	if err := sw.Rollback(); err != nil {
		return err
	}
	m.mu.Lock()
	m.switcher = nil
	m.mu.Unlock()
	return nil
}
