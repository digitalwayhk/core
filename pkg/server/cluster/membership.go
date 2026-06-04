package cluster

import (
	"context"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// MembershipManager sends heartbeats for a registered node and handles
// graceful deregistration on shutdown.
type MembershipManager struct {
	registry  ClusterRegistry
	nodeID    string
	interval  time.Duration
	stopCh    chan struct{}
}

// NewMembershipManager creates a manager that heartbeats every interval.
func NewMembershipManager(registry ClusterRegistry, nodeID string, interval time.Duration) *MembershipManager {
	return &MembershipManager{
		registry: registry,
		nodeID:   nodeID,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start begins sending heartbeats in a background goroutine.
func (m *MembershipManager) Start(ctx context.Context) {
	go m.run(ctx)
}

// Stop gracefully deregisters the node and stops heartbeating.
func (m *MembershipManager) Stop(ctx context.Context) {
	select {
	case <-m.stopCh:
	default:
		close(m.stopCh)
	}
	if err := m.registry.Deregister(ctx, m.nodeID); err != nil && err != ErrNodeNotFound {
		logx.Errorf("cluster membership: deregister %s: %v", m.nodeID, err)
	}
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
