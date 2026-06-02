package cluster

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// clusterSwitcher orchestrates a zero-downtime migration between two
// DiscoveryProvider implementations (e.g. local → etcd).
type clusterSwitcher struct {
	mu          sync.Mutex
	current     DiscoveryProvider
	pending     DiscoveryProvider
	inProgress  bool
	switchedAt  time.Time
}

// NewClusterSwitcher creates a switcher starting with the given provider.
func NewClusterSwitcher(initial DiscoveryProvider) ProviderSwitcher {
	return &clusterSwitcher{current: initial}
}

func (s *clusterSwitcher) Current() DiscoveryProvider {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

// Begin starts a dual-registration phase: the new provider begins receiving
// registrations alongside the current one.
func (s *clusterSwitcher) Begin(ctx context.Context, to DiscoveryProvider) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inProgress {
		return ErrMigrationInProgress
	}
	if s.current == nil {
		return ErrNotStarted
	}
	// Copy all current running nodes to the new provider.
	nodes, err := s.current.List(ctx, "")
	if err != nil {
		// Non-fatal: new provider may not have history yet.
		logx.Errorf("cluster switcher: list current nodes: %v", err)
	}
	for _, n := range nodes {
		if regErr := to.Register(ctx, n); regErr != nil {
			logx.Errorf("cluster switcher: warm-up register %s to %s: %v", n.ID, to.Name(), regErr)
		}
	}
	s.pending = to
	s.inProgress = true
	return nil
}

// Complete promotes the pending provider to current and shuts down the old one.
func (s *clusterSwitcher) Complete(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.inProgress || s.pending == nil {
		return fmt.Errorf("cluster switcher: no migration in progress")
	}
	old := s.current
	s.current = s.pending
	s.pending = nil
	s.inProgress = false
	s.switchedAt = time.Now()

	go func() {
		// Give the old provider a brief drain window before closing.
		time.Sleep(5 * time.Second)
		if err := old.Close(); err != nil {
			logx.Errorf("cluster switcher: close old provider %s: %v", old.Name(), err)
		}
	}()
	return nil
}

// Rollback aborts the migration and keeps the current provider active.
func (s *clusterSwitcher) Rollback(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.inProgress || s.pending == nil {
		return fmt.Errorf("cluster switcher: no migration in progress")
	}
	pending := s.pending
	s.pending = nil
	s.inProgress = false
	go func() {
		if err := pending.Close(); err != nil {
			logx.Errorf("cluster switcher: close pending provider %s: %v", pending.Name(), err)
		}
	}()
	return nil
}
