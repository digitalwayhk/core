package manage

import (
	"context"
	"net/http"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// ---- ClusterStatus ----

// ClusterStatus returns the active cluster provider and node list.
type ClusterStatus struct {
	ServiceName  string            `json:"service_name"`
	ProviderName string            `json:"provider_name,omitempty"`
	Nodes        []*cluster.NodeInfo `json:"nodes,omitempty"`
}

func (c *ClusterStatus) Parse(req types.IRequest) error    { return req.Bind(c) }
func (c *ClusterStatus) Validation(_ types.IRequest) error { return nil }

func (c *ClusterStatus) Do(req types.IRequest) (interface{}, error) {
	sc := router.GetContext(req.ServiceName())
	if sc == nil || sc.ClusterProvider == nil {
		return &ClusterStatus{ProviderName: "none"}, nil
	}
	nodes, err := sc.ClusterProvider.List(context.Background(), c.ServiceName)
	if err != nil {
		return nil, err
	}
	return &ClusterStatus{
		ServiceName:  c.ServiceName,
		ProviderName: sc.ClusterProvider.Name(),
		Nodes:        nodes,
	}, nil
}

func (c *ClusterStatus) RouterInfo() *types.RouterInfo {
	info := router.DefaultRouterInfo(c)
	info.Method = http.MethodPost
	info.Path = "/api/servermanage/cluster/status"
	info.Auth = true
	return info
}

// ---- ClusterNodes ----

// ClusterNodes returns nodes for a service, optionally filtered by status.
type ClusterNodes struct {
	ServiceName string `json:"service_name"`
	Status      string `json:"status,omitempty"`
	Nodes       []*cluster.NodeInfo `json:"nodes,omitempty"`
}

func (c *ClusterNodes) Parse(req types.IRequest) error    { return req.Bind(c) }
func (c *ClusterNodes) Validation(_ types.IRequest) error { return nil }

func (c *ClusterNodes) Do(req types.IRequest) (interface{}, error) {
	sc := router.GetContext(req.ServiceName())
	if sc == nil || sc.ClusterProvider == nil {
		return &ClusterNodes{Nodes: []*cluster.NodeInfo{}}, nil
	}
	var statuses []cluster.NodeStatus
	if c.Status != "" {
		statuses = []cluster.NodeStatus{cluster.NodeStatus(c.Status)}
	}
	nodes, err := sc.ClusterProvider.List(context.Background(), c.ServiceName, statuses...)
	if err != nil {
		return nil, err
	}
	return &ClusterNodes{ServiceName: c.ServiceName, Status: c.Status, Nodes: nodes}, nil
}

func (c *ClusterNodes) RouterInfo() *types.RouterInfo {
	info := router.DefaultRouterInfo(c)
	info.Method = http.MethodPost
	info.Path = "/api/servermanage/cluster/nodes"
	info.Auth = true
	return info
}

// ---- ClusterSwitchProvider ----

// ClusterSwitchProvider triggers a live provider migration.
type ClusterSwitchProvider struct {
	TargetProvider string   `json:"target_provider"` // "etcd" | "consul"
	Endpoints      []string `json:"endpoints,omitempty"`
	Action         string   `json:"action"` // "begin" | "complete" | "rollback"
	Result         string   `json:"result,omitempty"`
}

func (c *ClusterSwitchProvider) Parse(req types.IRequest) error    { return req.Bind(c) }
func (c *ClusterSwitchProvider) Validation(_ types.IRequest) error { return nil }

func (c *ClusterSwitchProvider) Do(_ types.IRequest) (interface{}, error) {
	// Placeholder: full ProviderSwitcher wiring requires a ClusterSwitcher on
	// the ServiceContext — to be completed when Phase 5 is fully deployed.
	return &ClusterSwitchProvider{
		Action: c.Action,
		Result: "provider switch scheduled; ClusterSwitcher must be initialised via service startup options",
	}, nil
}

func (c *ClusterSwitchProvider) RouterInfo() *types.RouterInfo {
	info := router.DefaultRouterInfo(c)
	info.Method = http.MethodPost
	info.Path = "/api/servermanage/cluster/switchprovider"
	info.Auth = true
	return info
}

