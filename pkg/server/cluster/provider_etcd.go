package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

const etcdTTL = 10 // lease TTL in seconds

// EtcdProvider implements DiscoveryProvider using etcd v3.
// Nodes are stored as JSON values under the key prefix:
//
//	/core/cluster/<serviceName>/<nodeID>
type EtcdProvider struct {
	client  *clientv3.Client
	prefix  string
	leases  map[string]clientv3.LeaseID
}

// NewEtcdProvider creates a provider connected to the given etcd endpoints.
func NewEtcdProvider(endpoints []string) (*EtcdProvider, error) {
	if len(endpoints) == 0 {
		return nil, errors.New("etcd: no endpoints provided")
	}
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("etcd: connect: %w", err)
	}
	return &EtcdProvider{
		client: cli,
		prefix: "/core/cluster",
		leases: make(map[string]clientv3.LeaseID),
	}, nil
}

func (e *EtcdProvider) Name() string { return "etcd" }

func (e *EtcdProvider) Close() error {
	return e.client.Close()
}

// Register stores the node in etcd with a lease. If the DC+MachineID slot
// is already taken by a running node with a different ID, returns ErrSlotConflict.
func (e *EtcdProvider) Register(ctx context.Context, node *NodeInfo) error {
	// Check for slot conflicts.
	existing, err := e.List(ctx, node.ServiceName, NodeStatusRunning)
	if err != nil {
		return err
	}
	for _, ex := range existing {
		if ex.ID != node.ID && ex.DataCenterID == node.DataCenterID && ex.MachineID == node.MachineID {
			return fmt.Errorf("%w: datacenter=%d machine=%d held by %s",
				ErrSlotConflict, node.DataCenterID, node.MachineID, ex.ID)
		}
	}

	// Grant lease for TTL-based expiry.
	lease, err := e.client.Grant(ctx, etcdTTL)
	if err != nil {
		return fmt.Errorf("etcd: grant lease: %w", err)
	}

	node.RegisteredAt = time.Now()
	node.LastHeartbeat = time.Now()
	node.Status = NodeStatusRunning

	data, err := json.Marshal(node)
	if err != nil {
		return err
	}
	key := e.nodeKey(node.ServiceName, node.ID)
	_, err = e.client.Put(ctx, key, string(data), clientv3.WithLease(lease.ID))
	if err != nil {
		return fmt.Errorf("etcd: put node: %w", err)
	}
	e.leases[node.ID] = lease.ID
	return nil
}

// Deregister marks a node offline and revokes its lease.
func (e *EtcdProvider) Deregister(ctx context.Context, nodeID string) error {
	// Find and update the node to offline status.
	resp, err := e.client.Get(ctx, e.nodeKeyByID(nodeID), clientv3.WithPrefix())
	if err != nil {
		return err
	}
	if len(resp.Kvs) == 0 {
		return ErrNodeNotFound
	}
	var node NodeInfo
	if err := json.Unmarshal(resp.Kvs[0].Value, &node); err != nil {
		return err
	}
	node.Status = NodeStatusOffline
	data, _ := json.Marshal(node)
	_, err = e.client.Put(ctx, string(resp.Kvs[0].Key), string(data))
	if err != nil {
		return err
	}
	// Revoke lease so the key will be cleaned up.
	if leaseID, ok := e.leases[nodeID]; ok {
		e.client.Revoke(ctx, leaseID)
		delete(e.leases, nodeID)
	}
	return nil
}

// Heartbeat keeps alive the node's etcd lease.
func (e *EtcdProvider) Heartbeat(ctx context.Context, nodeID string) error {
	leaseID, ok := e.leases[nodeID]
	if !ok {
		return ErrNodeNotFound
	}
	_, err := e.client.KeepAliveOnce(ctx, leaseID)
	if err != nil {
		return fmt.Errorf("etcd: keepalive %s: %w", nodeID, err)
	}
	return nil
}

// Get returns the node with the given ID.
func (e *EtcdProvider) Get(ctx context.Context, nodeID string) (*NodeInfo, error) {
	resp, err := e.client.Get(ctx, e.nodeKeyByID(nodeID), clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}
	if len(resp.Kvs) == 0 {
		return nil, ErrNodeNotFound
	}
	var node NodeInfo
	if err := json.Unmarshal(resp.Kvs[0].Value, &node); err != nil {
		return nil, err
	}
	return &node, nil
}

// List returns all nodes for the given service, optionally filtered by status.
func (e *EtcdProvider) List(ctx context.Context, serviceName string, statuses ...NodeStatus) ([]*NodeInfo, error) {
	prefix := fmt.Sprintf("%s/%s/", e.prefix, serviceName)
	resp, err := e.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("etcd: list nodes: %w", err)
	}
	statusSet := make(map[NodeStatus]struct{}, len(statuses))
	for _, s := range statuses {
		statusSet[s] = struct{}{}
	}
	var result []*NodeInfo
	for _, kv := range resp.Kvs {
		var node NodeInfo
		if err := json.Unmarshal(kv.Value, &node); err != nil {
			continue
		}
		if len(statusSet) > 0 {
			if _, ok := statusSet[node.Status]; !ok {
				continue
			}
		}
		result = append(result, &node)
	}
	return result, nil
}

// Watch calls onChange whenever the node list for serviceName changes.
func (e *EtcdProvider) Watch(ctx context.Context, serviceName string, onChange func([]*NodeInfo)) (func(), error) {
	prefix := fmt.Sprintf("%s/%s/", e.prefix, serviceName)
	watchCh := e.client.Watch(ctx, prefix, clientv3.WithPrefix())
	watchCtx, cancel := context.WithCancel(ctx)
	go func() {
		for {
			select {
			case <-watchCtx.Done():
				return
			case resp, ok := <-watchCh:
				if !ok {
					return
				}
				if resp.Err() != nil {
					continue
				}
				nodes, err := e.List(context.Background(), serviceName)
				if err == nil {
					onChange(nodes)
				}
			}
		}
	}()
	return cancel, nil
}

// AllocateMachineID finds the lowest available MachineID (0–1023) not held by
// a running node for the given DataCenterID.
func (e *EtcdProvider) AllocateMachineID(ctx context.Context, serviceName string, dataCenterID int64) (int64, error) {
	running, err := e.List(ctx, serviceName, NodeStatusRunning)
	if err != nil {
		return -1, err
	}
	used := make(map[int64]bool)
	for _, n := range running {
		if n.DataCenterID == dataCenterID {
			used[n.MachineID] = true
		}
	}
	for id := int64(0); id <= 1023; id++ {
		if !used[id] {
			return id, nil
		}
	}
	return -1, fmt.Errorf("etcd: all MachineID slots full for DataCenterID=%d", dataCenterID)
}

func (e *EtcdProvider) nodeKey(serviceName, nodeID string) string {
	return fmt.Sprintf("%s/%s/%s", e.prefix, serviceName, nodeID)
}

func (e *EtcdProvider) nodeKeyByID(nodeID string) string {
	// Search all services for the node (prefix search).
	return fmt.Sprintf("%s/", e.prefix)
}
