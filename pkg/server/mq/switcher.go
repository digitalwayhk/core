package mq

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// SwitchStage tracks progress of a live provider migration.
type SwitchStage string

const (
	SwitchStageIdle        SwitchStage = ""
	SwitchStageDoubleWrite SwitchStage = "double-write"
	SwitchStageCatchUp     SwitchStage = "catch-up"
	SwitchStageReadNew     SwitchStage = "read-new"
	SwitchStageComplete    SwitchStage = "complete"
)

// Switcher orchestrates a zero-downtime migration between two MQ providers.
// Phases: double-write → catch-up → read-new → complete
// On failure at any phase, Rollback() restores the original provider.
type Switcher struct {
	mu                sync.Mutex
	mgr               *MQManager
	oldProvider       MQProvider
	newProvider       MQProvider
	stage             SwitchStage
	rollbackOnFailure bool
}

// NewSwitcher creates a Switcher backed by the given manager.
func NewSwitcher(mgr *MQManager, rollbackOnFailure bool) *Switcher {
	return &Switcher{mgr: mgr, rollbackOnFailure: rollbackOnFailure}
}

// Stage returns the current migration stage.
func (s *Switcher) Stage() SwitchStage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stage
}

// Begin starts a migration from the current provider to newProvider.
// It connects the new provider and enters the double-write stage.
func (s *Switcher) Begin(ctx context.Context, newProvider MQProvider) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stage != SwitchStageIdle {
		return fmt.Errorf("mq switcher: migration already in progress (stage=%s)", s.stage)
	}
	old := s.mgr.Current()
	if old == nil {
		return fmt.Errorf("mq switcher: no current provider to migrate from")
	}
	if err := newProvider.Connect(ctx); err != nil {
		return fmt.Errorf("mq switcher: connect new provider: %w", err)
	}
	s.oldProvider = old
	s.newProvider = newProvider
	s.stage = SwitchStageDoubleWrite
	return nil
}

// DoubleWritePublish publishes to both old and new providers simultaneously.
// It must be called for every publish during the double-write stage.
func (s *Switcher) DoubleWritePublish(ctx context.Context, subject string, data []byte, opts *PublishOptions) error {
	s.mu.Lock()
	stage := s.stage
	old, new_ := s.oldProvider, s.newProvider
	s.mu.Unlock()
	if stage != SwitchStageDoubleWrite {
		return fmt.Errorf("mq switcher: not in double-write stage (current=%s)", stage)
	}
	if err := old.Publish(ctx, subject, data, opts); err != nil {
		return err
	}
	if err := new_.Publish(ctx, subject, data, opts); err != nil {
		if s.rollbackOnFailure {
			_ = s.Rollback()
		}
		return fmt.Errorf("mq switcher: new provider publish failed: %w", err)
	}
	return nil
}

// AdvanceToCatchUp marks the migration as entering the catch-up stage.
// Callers are responsible for draining any backlog on the new provider before advancing further.
func (s *Switcher) AdvanceToCatchUp() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stage != SwitchStageDoubleWrite {
		return fmt.Errorf("mq switcher: expected double-write stage, got %s", s.stage)
	}
	s.stage = SwitchStageCatchUp
	return nil
}

// AdvanceToReadNew switches reads to the new provider.
// The new provider becomes the active current provider in the manager.
func (s *Switcher) AdvanceToReadNew() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stage != SwitchStageCatchUp {
		return fmt.Errorf("mq switcher: expected catch-up stage, got %s", s.stage)
	}
	s.mgr.Register(s.newProvider)
	if err := s.mgr.SetCurrent(s.newProvider.Name()); err != nil {
		return err
	}
	s.stage = SwitchStageReadNew
	return nil
}

// Complete finalises the migration and closes the old provider after a drain delay.
func (s *Switcher) Complete(ctx context.Context, drainDelay time.Duration) error {
	s.mu.Lock()
	if s.stage != SwitchStageReadNew {
		s.mu.Unlock()
		return fmt.Errorf("mq switcher: expected read-new stage, got %s", s.stage)
	}
	old := s.oldProvider
	s.stage = SwitchStageComplete
	s.mu.Unlock()

	// Allow in-flight messages to drain before closing the old provider.
	select {
	case <-ctx.Done():
	case <-time.After(drainDelay):
	}
	return old.Close()
}

// Rollback restores the old provider as the current active provider.
func (s *Switcher) Rollback() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.oldProvider == nil {
		return fmt.Errorf("mq switcher: nothing to roll back to")
	}
	// Restore old provider as current (it was never removed from registry).
	s.mgr.Register(s.oldProvider)
	if err := s.mgr.SetCurrent(s.oldProvider.Name()); err != nil {
		return err
	}
	if s.newProvider != nil {
		_ = s.newProvider.Close()
	}
	s.oldProvider = nil
	s.newProvider = nil
	s.stage = SwitchStageIdle
	return nil
}
