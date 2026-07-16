package cluster

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// MembershipManager sends heartbeats for a registered node and handles
// graceful deregistration on shutdown.
type MembershipManager struct {
	registry   ClusterRegistry
	nodeID     string
	interval   time.Duration
	stopCh     chan struct{}
	doneCh     chan struct{}
	startOnce  sync.Once
	stopOnce   sync.Once
	stopErr    error
	retries    int
	retryDelay time.Duration
}

// MembershipOption 调整成员注销策略。
type MembershipOption func(*MembershipManager)

// WithDeregisterRetry 配置有界注销重试，主要用于环境调优和确定性测试。
func WithDeregisterRetry(attempts int, delay time.Duration) MembershipOption {
	return func(manager *MembershipManager) {
		if attempts > 0 {
			manager.retries = attempts
		}
		manager.retryDelay = delay
	}
}

// NewMembershipManager creates a manager that heartbeats every interval.
func NewMembershipManager(registry ClusterRegistry, nodeID string, interval time.Duration, options ...MembershipOption) *MembershipManager {
	manager := &MembershipManager{
		registry:   registry,
		nodeID:     nodeID,
		interval:   interval,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
		retries:    3,
		retryDelay: 20 * time.Millisecond,
	}
	for _, option := range options {
		option(manager)
	}
	return manager
}

// Start begins sending heartbeats in a background goroutine.
func (m *MembershipManager) Start(ctx context.Context) {
	m.startOnce.Do(func() {
		go func() {
			defer close(m.doneCh)
			m.run(ctx)
		}()
	})
}

// Stop gracefully deregisters the node and stops heartbeating.
func (m *MembershipManager) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	// Stop 若先于 Start 调用，也要阻止之后启动新 worker。
	m.startOnce.Do(func() { close(m.doneCh) })
	m.stopOnce.Do(func() {
		close(m.stopCh)
		m.stopErr = m.deregister(ctx)
	})

	select {
	case <-m.doneCh:
	case <-ctx.Done():
		if m.stopErr == nil {
			m.stopErr = ctx.Err()
		}
	}
	return m.stopErr
}

func (m *MembershipManager) deregister(ctx context.Context) error {
	var lastErr error
	for attempt := 0; attempt < m.retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := m.registry.Deregister(ctx, m.nodeID)
		if err == nil || errors.Is(err, ErrNodeNotFound) {
			return nil
		}
		lastErr = err
		if attempt == m.retries-1 || m.retryDelay <= 0 {
			continue
		}
		delay := m.retryDelay << attempt
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

func (m *MembershipManager) run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.registry.Heartbeat(ctx, m.nodeID); err != nil {
				logx.Errorf("cluster membership: heartbeat %s: %v", m.nodeID, err)
			}
		}
	}
}
