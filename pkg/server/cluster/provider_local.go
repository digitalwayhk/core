package cluster

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// LocalProvider is an in-memory ClusterRegistry suitable for single-node
// development and unit testing. It supports heartbeat and fault detection
// but has no network distribution; all nodes must share the same process
// (or the registry must be passed by pointer).
type LocalProvider struct {
	mu               sync.RWMutex
	nodes            map[string]*NodeInfo // nodeID → node
	watchers         map[string][]watcherEntry
	heartbeatTimeout time.Duration
	suspectTimeout   time.Duration
	cooldown         time.Duration
	stopCh           chan struct{}
}

type watcherEntry struct {
	id       int64
	onChange func([]*NodeInfo)
}

var localWatcherSeq int64 = 0

// NewLocalProvider creates an in-memory registry.
// heartbeatTimeout: how long without heartbeat before node becomes suspect.
// suspectTimeout: how long in suspect before becoming offline.
// cooldown: minimum time offline before the MachineID slot can be reused.
func NewLocalProvider(heartbeatTimeout, suspectTimeout, cooldown time.Duration) *LocalProvider {
	return &LocalProvider{
		nodes:            make(map[string]*NodeInfo),
		watchers:         make(map[string][]watcherEntry),
		heartbeatTimeout: heartbeatTimeout,
		suspectTimeout:   suspectTimeout,
		cooldown:         cooldown,
		stopCh:           make(chan struct{}),
	}
}

func (p *LocalProvider) Name() string { return "local" }

// Start begins the background goroutine that advances suspect/offline states.
func (p *LocalProvider) Start() {
	go p.runFaultDetection()
}

// Close stops the background goroutine.
func (p *LocalProvider) Close() error {
	select {
	case <-p.stopCh:
	default:
		close(p.stopCh)
	}
	return nil
}

// Register adds or refreshes a node. If the DataCenterID+MachineID slot is
// held by a different running node, ErrSlotConflict is returned.
// If the slot is held by an offline node past its cooldown, the slot is reclaimed.
func (p *LocalProvider) Register(_ context.Context, node *NodeInfo) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()

	// Check for slot conflicts.
	for _, existing := range p.nodes {
		if existing.ID == node.ID {
			continue // same node refreshing
		}
		if existing.ServiceName == node.ServiceName && existing.DataCenterID == node.DataCenterID && existing.MachineID == node.MachineID {
			if existing.Status == NodeStatusRunning {
				return fmt.Errorf("%w: datacenter=%d machine=%d held by %s",
					ErrSlotConflict, node.DataCenterID, node.MachineID, existing.ID)
			}
			// Offline node: check cooldown.
			if existing.Status == NodeStatusOffline && now.Sub(existing.LastHeartbeat) < p.cooldown {
				return fmt.Errorf("%w: slot in cooldown (reusable in %s)",
					ErrSlotConflict, p.cooldown-now.Sub(existing.LastHeartbeat))
			}
			// Stale offline entry — remove it and allow reuse.
			delete(p.nodes, existing.ID)
		}
	}

	node.RegisteredAt = now
	node.LastHeartbeat = now
	node.Status = NodeStatusRunning
	p.nodes[node.ID] = node
	p.notifyWatchers(node.ServiceName)
	return nil
}

// Deregister marks a node as offline.
func (p *LocalProvider) Deregister(_ context.Context, nodeID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	node, ok := p.nodes[nodeID]
	if !ok {
		return ErrNodeNotFound
	}
	node.Status = NodeStatusOffline
	p.notifyWatchers(node.ServiceName)
	return nil
}

// Heartbeat refreshes a node's last-seen timestamp and resets Status to running.
func (p *LocalProvider) Heartbeat(_ context.Context, nodeID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	node, ok := p.nodes[nodeID]
	if !ok {
		return ErrNodeNotFound
	}
	node.LastHeartbeat = time.Now()
	if node.Status == NodeStatusSuspect {
		node.Status = NodeStatusRunning
		p.notifyWatchers(node.ServiceName)
	}
	return nil
}

// Get returns the node with the given ID.
func (p *LocalProvider) Get(_ context.Context, nodeID string) (*NodeInfo, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	node, ok := p.nodes[nodeID]
	if !ok {
		return nil, ErrNodeNotFound
	}
	cp := *node
	return &cp, nil
}

// List returns nodes for the service, filtered by status (empty = all).
func (p *LocalProvider) List(_ context.Context, serviceName string, statuses ...NodeStatus) ([]*NodeInfo, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	statusSet := make(map[NodeStatus]struct{}, len(statuses))
	for _, s := range statuses {
		statusSet[s] = struct{}{}
	}

	var result []*NodeInfo
	for _, n := range p.nodes {
		if serviceName != "" && n.ServiceName != serviceName {
			continue
		}
		if len(statusSet) > 0 {
			if _, ok := statusSet[n.Status]; !ok {
				continue
			}
		}
		cp := *n
		result = append(result, &cp)
	}
	return result, nil
}

// Watch registers a callback that is fired whenever the node list for the
// service changes. The cancel function removes the watcher.
func (p *LocalProvider) Watch(_ context.Context, serviceName string, onChange func([]*NodeInfo)) (func(), error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	localWatcherSeq++
	id := localWatcherSeq
	p.watchers[serviceName] = append(p.watchers[serviceName], watcherEntry{id: id, onChange: onChange})

	return func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		list := p.watchers[serviceName]
		updated := make([]watcherEntry, 0, len(list))
		for _, e := range list {
			if e.id != id {
				updated = append(updated, e)
			}
		}
		p.watchers[serviceName] = updated
	}, nil
}

// notifyWatchers fires watchers for the given service (must be called under p.mu.Lock).
func (p *LocalProvider) notifyWatchers(serviceName string) {
	watchers := p.watchers[serviceName]
	if len(watchers) == 0 {
		return
	}
	// Build snapshot under lock; fire callbacks outside lock to avoid deadlock.
	var nodes []*NodeInfo
	for _, n := range p.nodes {
		if n.ServiceName == serviceName {
			cp := *n
			nodes = append(nodes, &cp)
		}
	}
	for _, w := range watchers {
		go w.onChange(nodes)
	}
}

// runFaultDetection periodically advances stale nodes from running→suspect→offline.
func (p *LocalProvider) runFaultDetection() {
	ticker := time.NewTicker(p.heartbeatTimeout / 2)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.advanceStates()
		}
	}
}

func (p *LocalProvider) advanceStates() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	changed := make(map[string]bool)

	for _, n := range p.nodes {
		switch n.Status {
		case NodeStatusRunning:
			if now.Sub(n.LastHeartbeat) > p.heartbeatTimeout {
				n.Status = NodeStatusSuspect
				changed[n.ServiceName] = true
			}
		case NodeStatusSuspect:
			if now.Sub(n.LastHeartbeat) > p.heartbeatTimeout+p.suspectTimeout {
				n.Status = NodeStatusOffline
				changed[n.ServiceName] = true
			}
		}
	}

	for svc := range changed {
		p.notifyWatchers(svc)
	}
}

// AllocateMachineID finds the lowest available MachineID for the given service
// and DataCenterID. Returns -1 if all slots are full.
func (p *LocalProvider) AllocateMachineID(serviceName string, dataCenterID int64, maxMachineID ...int64) int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	max := int64(1023)
	if len(maxMachineID) > 0 && maxMachineID[0] > 0 {
		max = maxMachineID[0]
	}

	used := make(map[int64]bool)
	for _, n := range p.nodes {
		if n.ServiceName == serviceName && n.DataCenterID == dataCenterID && n.Status == NodeStatusRunning {
			used[n.MachineID] = true
		}
	}
	for id := int64(0); id <= max; id++ {
		if !used[id] {
			return id
		}
	}
	return -1
}
