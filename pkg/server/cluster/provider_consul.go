package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	consulapi "github.com/hashicorp/consul/api"
)

const consulServicePrefix = "core-cluster"
const consulCheckInterval = "10s"
const consulCheckTimeout = "3s"
const consulCheckDeregisterCritical = "30s"

// ConsulProvider implements DiscoveryProvider using HashiCorp Consul.
// Each node is registered as a Consul service entry:
//
//	ServiceID: "<serviceName>-<nodeID>"
//	Tags: ["core-cluster", "<serviceName>"]
//	Meta: JSON-encoded extra NodeInfo fields
type ConsulProvider struct {
	client *consulapi.Client
}

// NewConsulProvider creates a provider connected to the given Consul address.
func NewConsulProvider(address string) (*ConsulProvider, error) {
	cfg := consulapi.DefaultConfig()
	if address != "" {
		cfg.Address = address
	}
	cli, err := consulapi.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("consul: new client: %w", err)
	}
	return &ConsulProvider{client: cli}, nil
}

func (c *ConsulProvider) Name() string { return "consul" }

func (c *ConsulProvider) Close() error { return nil }

// Register registers the node in Consul with a TTL health check.
func (c *ConsulProvider) Register(ctx context.Context, node *NodeInfo) error {
	// Conflict check.
	existing, err := c.List(ctx, node.ServiceName, NodeStatusRunning)
	if err != nil {
		return err
	}
	for _, ex := range existing {
		if ex.ID != node.ID && ex.DataCenterID == node.DataCenterID && ex.MachineID == node.MachineID {
			return fmt.Errorf("%w: datacenter=%d machine=%d held by %s",
				ErrSlotConflict, node.DataCenterID, node.MachineID, ex.ID)
		}
	}

	extra, _ := json.Marshal(map[string]interface{}{
		"datacenter_id":  node.DataCenterID,
		"machine_id":     node.MachineID,
		"socket_port":    node.SocketPort,
		"grpc_port":      node.GRPCPort,
		"weight":         node.Weight,
		"registered_at":  node.RegisteredAt.Format(time.RFC3339),
		"last_heartbeat": node.LastHeartbeat.Format(time.RFC3339),
	})

	svc := &consulapi.AgentServiceRegistration{
		ID:      consulServiceID(node.ServiceName, node.ID),
		Name:    node.ServiceName,
		Tags:    []string{consulServicePrefix, node.ServiceName},
		Address: node.Address,
		Port:    node.Port,
		Meta: map[string]string{
			"node_id": node.ID,
			"extra":   string(extra),
		},
		Check: &consulapi.AgentServiceCheck{
			TTL:                            consulCheckInterval,
			DeregisterCriticalServiceAfter: consulCheckDeregisterCritical,
		},
	}
	if err := c.client.Agent().ServiceRegister(svc); err != nil {
		return fmt.Errorf("consul: register service: %w", err)
	}
	// Mark passing immediately.
	checkID := "service:" + svc.ID
	return c.client.Agent().UpdateTTL(checkID, "registered", consulapi.HealthPassing)
}

// Deregister removes the node from Consul.
func (c *ConsulProvider) Deregister(_ context.Context, nodeID string) error {
	// We need the serviceName to build the service ID; scan registrations to find it.
	entries, _, err := c.client.Health().Service("", "", false, nil)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Service.Meta["node_id"] == nodeID {
			return c.client.Agent().ServiceDeregister(e.Service.ID)
		}
	}
	return ErrNodeNotFound
}

// Heartbeat updates the TTL health check to passing.
func (c *ConsulProvider) Heartbeat(ctx context.Context, nodeID string) error {
	entries, _, err := c.client.Health().Service("", "", false, nil)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Service.Meta["node_id"] == nodeID {
			checkID := "service:" + e.Service.ID
			return c.client.Agent().UpdateTTL(checkID, "heartbeat", consulapi.HealthPassing)
		}
	}
	return ErrNodeNotFound
}

// Get returns the NodeInfo for the given nodeID.
func (c *ConsulProvider) Get(_ context.Context, nodeID string) (*NodeInfo, error) {
	entries, _, err := c.client.Health().Service("", "", false, nil)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Service.Meta["node_id"] == nodeID {
			return consulEntryToNode(e), nil
		}
	}
	return nil, ErrNodeNotFound
}

// List returns nodes for the given service, optionally filtered by status.
func (c *ConsulProvider) List(_ context.Context, serviceName string, statuses ...NodeStatus) ([]*NodeInfo, error) {
	entries, _, err := c.client.Health().Service(serviceName, "", false, nil)
	if err != nil {
		return nil, fmt.Errorf("consul: list service %s: %w", serviceName, err)
	}
	statusSet := make(map[NodeStatus]struct{}, len(statuses))
	for _, s := range statuses {
		statusSet[s] = struct{}{}
	}
	var result []*NodeInfo
	for _, e := range entries {
		node := consulEntryToNode(e)
		if len(statusSet) > 0 {
			if _, ok := statusSet[node.Status]; !ok {
				continue
			}
		}
		result = append(result, node)
	}
	return result, nil
}

// Watch calls onChange whenever the service catalog changes.
func (c *ConsulProvider) Watch(ctx context.Context, serviceName string, onChange func([]*NodeInfo)) (func(), error) {
	done := make(chan struct{})
	go func() {
		var lastIndex uint64
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			default:
			}
			opts := &consulapi.QueryOptions{WaitIndex: lastIndex, WaitTime: 30 * time.Second}
			opts = opts.WithContext(ctx)
			entries, meta, err := c.client.Health().Service(serviceName, "", false, opts)
			if err != nil {
				time.Sleep(time.Second)
				continue
			}
			if meta.LastIndex > lastIndex {
				lastIndex = meta.LastIndex
				var nodes []*NodeInfo
				for _, e := range entries {
					nodes = append(nodes, consulEntryToNode(e))
				}
				onChange(nodes)
			}
		}
	}()
	return func() { close(done) }, nil
}

func consulEntryToNode(e *consulapi.ServiceEntry) *NodeInfo {
	node := &NodeInfo{
		ID:          e.Service.Meta["node_id"],
		ServiceName: e.Service.Service,
		Address:     e.Service.Address,
		Port:        e.Service.Port,
		Status:      NodeStatusRunning,
	}
	// Check health status.
	for _, check := range e.Checks {
		if check.Status == consulapi.HealthCritical {
			node.Status = NodeStatusOffline
		} else if check.Status == consulapi.HealthWarning {
			node.Status = NodeStatusSuspect
		}
	}
	// Decode extra metadata.
	if extra, ok := e.Service.Meta["extra"]; ok {
		var m map[string]interface{}
		if json.Unmarshal([]byte(extra), &m) == nil {
			if v, ok := m["datacenter_id"].(float64); ok {
				node.DataCenterID = int64(v)
			}
			if v, ok := m["machine_id"].(float64); ok {
				node.MachineID = int64(v)
			}
			if v, ok := m["weight"].(float64); ok {
				node.Weight = int(v)
			}
		}
	}
	return node
}

func consulServiceID(serviceName, nodeID string) string {
	return fmt.Sprintf("%s-%s", serviceName, nodeID)
}

// AllocateMachineID finds the lowest available MachineID (0–1023) not held by
// a running node for the given service and DataCenterID.
func (c *ConsulProvider) AllocateMachineID(ctx context.Context, serviceName string, dataCenterID int64) (int64, error) {
	running, err := c.List(ctx, serviceName, NodeStatusRunning)
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
	return -1, fmt.Errorf("consul: all MachineID slots full for DataCenterID=%d", dataCenterID)
}
