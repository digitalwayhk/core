package manage

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// ---- ClusterStatus ----

// ClusterStatus returns the active cluster provider and node list.
type ClusterStatus struct {
	ServiceName  string              `json:"service_name"`
	ProviderName string              `json:"provider_name,omitempty"`
	Nodes        []*cluster.NodeInfo `json:"nodes,omitempty"`
}

func (c *ClusterStatus) Parse(req types.IRequest) error    { return req.Bind(c) }
func (c *ClusterStatus) Validation(_ types.IRequest) error { return nil }

func (c *ClusterStatus) Do(req types.IRequest) (interface{}, error) {
	sc := router.GetContext(req.ServiceName())
	if sc == nil {
		return &ClusterStatus{ProviderName: "none"}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	providerName, nodes, err := sc.ClusterProviderSnapshot(ctx, c.ServiceName)
	if err != nil {
		return nil, err
	}
	return &ClusterStatus{
		ServiceName:  c.ServiceName,
		ProviderName: providerName,
		Nodes:        nodes,
	}, nil
}

func (c *ClusterStatus) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(c,
		router.WithMethod(http.MethodPost),
		router.WithPath("/api/servermanage/cluster/status"),
		router.WithAuth(true),
	)
}

// ---- ClusterNodes ----

// ClusterNodes returns nodes for a service, optionally filtered by status.
type ClusterNodes struct {
	ServiceName string              `json:"service_name"`
	Status      string              `json:"status,omitempty"`
	Nodes       []*cluster.NodeInfo `json:"nodes,omitempty"`
}

func (c *ClusterNodes) Parse(req types.IRequest) error    { return req.Bind(c) }
func (c *ClusterNodes) Validation(_ types.IRequest) error { return nil }

func (c *ClusterNodes) Do(req types.IRequest) (interface{}, error) {
	sc := router.GetContext(req.ServiceName())
	if sc == nil {
		return &ClusterNodes{Nodes: []*cluster.NodeInfo{}}, nil
	}
	var statuses []cluster.NodeStatus
	if c.Status != "" {
		statuses = []cluster.NodeStatus{cluster.NodeStatus(c.Status)}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, nodes, err := sc.ClusterProviderSnapshot(ctx, c.ServiceName, statuses...)
	if err != nil {
		return nil, err
	}
	return &ClusterNodes{ServiceName: c.ServiceName, Status: c.Status, Nodes: nodes}, nil
}

func (c *ClusterNodes) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(c,
		router.WithMethod(http.MethodPost),
		router.WithPath("/api/servermanage/cluster/nodes"),
		router.WithAuth(true),
	)
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

func (c *ClusterSwitchProvider) Do(req types.IRequest) (interface{}, error) {
	sc := router.GetContext(req.ServiceName())
	if sc == nil || sc.ClusterSwitcher == nil {
		return &ClusterSwitchProvider{
			Action: c.Action,
			Result: "cluster switcher not initialised",
		}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var err error
	switch c.Action {
	case "begin":
		to, buildErr := buildTargetProvider(c.TargetProvider, c.Endpoints)
		if buildErr != nil {
			return nil, buildErr
		}
		err = sc.BeginProviderSwitch(ctx, to)
	case "complete":
		err = sc.CompleteProviderSwitch(ctx)
	case "rollback":
		err = sc.RollbackProviderSwitch(ctx)
	default:
		return nil, fmt.Errorf("unknown action: %s", c.Action)
	}
	if err != nil {
		return nil, err
	}
	return &ClusterSwitchProvider{Action: c.Action, Result: "ok"}, nil
}

func buildTargetProvider(name string, endpoints []string) (cluster.DiscoveryProvider, error) {
	switch name {
	case "local":
		p := cluster.NewLocalProvider(30*time.Second, 10*time.Second, 30*time.Second)
		p.Start()
		return p, nil
	case "etcd":
		return cluster.NewEtcdProvider(endpoints, 0)
	case "consul":
		addr := ""
		if len(endpoints) > 0 {
			addr = endpoints[0]
		}
		return cluster.NewConsulProvider(addr)
	default:
		return nil, fmt.Errorf("unsupported target provider: %s", name)
	}
}

func (c *ClusterSwitchProvider) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(c,
		router.WithMethod(http.MethodPost),
		router.WithPath("/api/servermanage/cluster/switchprovider"),
		router.WithAuth(true),
	)
}
