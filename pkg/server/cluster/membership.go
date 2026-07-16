package cluster

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// MembershipManager sends heartbeats for a registered node and handles
// graceful deregistration on shutdown.
type MembershipManager struct {
	registry        ClusterRegistry
	nodeID          string
	interval        time.Duration
	stopCh          chan struct{}
	doneCh          chan struct{}
	startOnce       sync.Once
	stopOnce        sync.Once
	stopDone        chan struct{}
	stopErrMu       sync.RWMutex
	stopErr         error
	retries         int
	retryDelay      time.Duration
	stopTimeout     time.Duration
	heartbeatCancel context.CancelFunc
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

// WithDeregisterTimeout limits the shared deregistration operation. Each
// Stop caller may use a shorter context without cancelling the shared cleanup.
func WithDeregisterTimeout(timeout time.Duration) MembershipOption {
	return func(manager *MembershipManager) {
		if timeout > 0 {
			manager.stopTimeout = timeout
		}
	}
}

// NewMembershipManager creates a manager that heartbeats every interval.
func NewMembershipManager(registry ClusterRegistry, nodeID string, interval time.Duration, options ...MembershipOption) *MembershipManager {
	manager := &MembershipManager{
		registry:    registry,
		nodeID:      nodeID,
		interval:    interval,
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
		stopDone:    make(chan struct{}),
		retries:     3,
		retryDelay:  20 * time.Millisecond,
		stopTimeout: 5 * time.Second,
	}
	for _, option := range options {
		option(manager)
	}
	return manager
}

// Start begins sending heartbeats in a background goroutine.
func (m *MembershipManager) Start(ctx context.Context) {
	m.startOnce.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		heartbeatCtx, cancel := context.WithCancel(ctx)
		m.heartbeatCancel = cancel
		go func() {
			defer close(m.doneCh)
			m.run(heartbeatCtx)
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
		if m.heartbeatCancel != nil {
			m.heartbeatCancel()
		}
		go func() {
			stopCtx, cancel := context.WithTimeout(context.Background(), m.stopTimeout)
			defer cancel()

			workerErr := m.waitWorker(stopCtx)
			deregisterErr := m.deregisterBounded(stopCtx)
			err := errors.Join(workerErr, deregisterErr)
			m.stopErrMu.Lock()
			m.stopErr = err
			m.stopErrMu.Unlock()
			close(m.stopDone)
		}()
	})

	select {
	case <-m.stopDone:
		m.stopErrMu.RLock()
		err := m.stopErr
		m.stopErrMu.RUnlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *MembershipManager) waitWorker(stopCtx context.Context) error {
	waitBudget := m.stopTimeout / 2
	if waitBudget <= 0 {
		waitBudget = m.stopTimeout
	}
	timer := time.NewTimer(waitBudget)
	defer timer.Stop()
	select {
	case <-m.doneCh:
		return nil
	case <-timer.C:
		return errors.New("cluster membership: heartbeat worker stop timed out")
	case <-stopCtx.Done():
		return fmt.Errorf("cluster membership: heartbeat worker stop: %w", stopCtx.Err())
	}
}

func (m *MembershipManager) deregisterBounded(ctx context.Context) error {
	result := make(chan error, 1)
	go func() { result <- m.deregister(ctx) }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return fmt.Errorf("cluster membership: deregister %s: %w", m.nodeID, ctx.Err())
	}
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
