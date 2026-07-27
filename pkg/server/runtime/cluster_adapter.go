package runtime

import (
	"context"

	"github.com/digitalwayhk/core/pkg/server/cluster"
)

// ProviderClusterView 将 ClusterProvider 适配为 Aggregator 所需视图。
type ProviderClusterView struct {
	Provider cluster.ClusterRegistry
}

// List 委托 Provider.List。
func (v ProviderClusterView) List(ctx context.Context, serviceName string, statuses ...cluster.NodeStatus) ([]*cluster.NodeInfo, error) {
	if v.Provider == nil {
		return nil, nil
	}
	return v.Provider.List(ctx, serviceName, statuses...)
}

// ListServices 通过 List("", ) 收集唯一服务名。
func (v ProviderClusterView) ListServices(ctx context.Context) ([]string, error) {
	if v.Provider == nil {
		return nil, nil
	}
	nodes, err := v.Provider.List(ctx, "")
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, n := range nodes {
		if n == nil || n.ServiceName == "" {
			continue
		}
		if _, ok := seen[n.ServiceName]; ok {
			continue
		}
		seen[n.ServiceName] = struct{}{}
		out = append(out, n.ServiceName)
	}
	return out, nil
}
