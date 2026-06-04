// Package cluster provides the service discovery, membership management,
// and load balancing infrastructure for horizontally scaled core services.
//
// Architecture:
//
//	ClusterProvider (local / etcd / consul)
//	  └─ ClusterRegistry  — register, deregister, list nodes
//	  └─ DiscoveryProvider — watch changes
//	  └─ MembershipManager — heartbeat, lease, fault detection
//	  └─ LoadBalancer      — local-first / round-robin / consistent-hash / weighted
//	  └─ ServiceShardRouter — shard-key based routing rules
package cluster

import (
	"context"
	"time"
)

// NodeStatus represents the lifecycle state of a cluster node.
type NodeStatus string

const (
	NodeStatusRunning NodeStatus = "running"
	NodeStatusSuspect NodeStatus = "suspect"
	NodeStatusOffline NodeStatus = "offline"
)

// NodeInfo describes a single service instance in the cluster.
type NodeInfo struct {
	// ID is globally unique, e.g. "<service>-<datacenter>-<machine>-<pid>"
	ID string `json:"id"`

	ServiceName   string     `json:"service_name"`
	DataCenterID  int64      `json:"datacenter_id"`
	MachineID     int64      `json:"machine_id"`
	Address       string     `json:"address"`
	Port          int        `json:"port"`
	SocketPort    int        `json:"socket_port,omitempty"`
	GRPCPort      int        `json:"grpc_port,omitempty"`
	Status        NodeStatus `json:"status"`
	Weight        int        `json:"weight"`       // for weighted balancing; default 1
	RegisteredAt  time.Time  `json:"registered_at"`
	LastHeartbeat time.Time  `json:"last_heartbeat"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// IsHealthy returns true when the node is running and has a recent heartbeat.
func (n *NodeInfo) IsHealthy(timeout time.Duration) bool {
	return n.Status == NodeStatusRunning && time.Since(n.LastHeartbeat) < timeout
}

// ClusterRegistry provides node registration and lookup.
type ClusterRegistry interface {
	// Register adds or refreshes a node entry. Returns an error if the slot
	// (DataCenterID + MachineID combination) is already taken by a different running node.
	Register(ctx context.Context, node *NodeInfo) error

	// Deregister marks a node as offline.
	Deregister(ctx context.Context, nodeID string) error

	// Heartbeat refreshes the node's last-seen timestamp and ensures Status=running.
	Heartbeat(ctx context.Context, nodeID string) error

	// Get returns the node with the given ID, or ErrNodeNotFound.
	Get(ctx context.Context, nodeID string) (*NodeInfo, error)

	// List returns all nodes for the given service, optionally filtered by status.
	List(ctx context.Context, serviceName string, statuses ...NodeStatus) ([]*NodeInfo, error)

	// Watch calls onChange whenever the node list for serviceName changes.
	// The returned cancel function stops watching.
	Watch(ctx context.Context, serviceName string, onChange func([]*NodeInfo)) (cancel func(), err error)

	// Close releases all resources.
	Close() error
}

// DiscoveryProvider extends ClusterRegistry with provider identity.
type DiscoveryProvider interface {
	ClusterRegistry
	Name() string
}

// ProviderSwitcher manages zero-downtime migration between two ClusterRegistry
// providers (e.g. local → etcd).
type ProviderSwitcher interface {
	// Current returns the active provider.
	Current() DiscoveryProvider

	// Begin starts the migration to the new provider.
	Begin(ctx context.Context, to DiscoveryProvider) error

	// Complete finalises the migration and shuts down the old provider.
	Complete(ctx context.Context) error

	// Rollback aborts and reverts to the old provider.
	Rollback(ctx context.Context) error
}
