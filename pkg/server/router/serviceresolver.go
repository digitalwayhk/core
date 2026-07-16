package router

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/transport"
	"github.com/digitalwayhk/core/pkg/server/types"
)

var ErrTargetServiceUnavailable = errors.New("target service unavailable")

// ResolvedService 是一次服务解析得到的不可变目标快照。
type ResolvedService struct {
	Info      *types.TargetInfo
	Endpoints transport.TransportEndpoints
	Local     *ServiceContext
	NodeID    string
}

type resolverEntry struct {
	nodes  []*cluster.NodeInfo
	cancel func()
}

// ServiceResolver 把服务名解析为同进程 ServiceContext 或集群健康节点。
// 它不拥有 ClusterProvider，只负责维护按目标服务划分的 Watch 快照。
type ServiceResolver struct {
	mu       sync.RWMutex
	provider cluster.DiscoveryProvider
	local    func(string) *ServiceContext
	balancer cluster.LoadBalancer
	entries  map[string]*resolverEntry
	closed   bool
}

func NewServiceResolver(provider cluster.DiscoveryProvider, local func(string) *ServiceContext) *ServiceResolver {
	if local == nil {
		local = func(string) *ServiceContext { return nil }
	}
	return &ServiceResolver{
		provider: provider,
		local:    local,
		balancer: cluster.NewRoundRobinBalancer(),
		entries:  make(map[string]*resolverEntry),
	}
}

func (r *ServiceResolver) Resolve(ctx context.Context, serviceName string) (*ResolvedService, error) {
	serviceName = strings.ToLower(strings.TrimSpace(serviceName))
	if serviceName == "" {
		return nil, fmt.Errorf("%w: service name is empty", ErrTargetServiceUnavailable)
	}
	if local := r.local(serviceName); local != nil && local.Config != nil {
		address := local.Config.RunIp
		if local.Config.Cluster.AdvertiseAddress != "" {
			address = local.Config.Cluster.AdvertiseAddress
		}
		grpcPort := local.Config.Transport.GRPC.Port
		return &ResolvedService{
			Local: local,
			Info: &types.TargetInfo{
				TargetAddress: address, TargetService: serviceName,
				TargetPort: local.Config.Port, TargetSocketPort: local.Config.SocketPort,
				TargetGRPCPort: grpcPort,
			},
			Endpoints: serviceTransportEndpoints(address, local.Config.Port, grpcPort, local.Config.SocketPort),
		}, nil
	}

	entry, err := r.ensureEntry(ctx, serviceName)
	if err != nil {
		return nil, err
	}
	r.mu.RLock()
	nodes := cloneResolverNodes(entry.nodes)
	r.mu.RUnlock()
	healthy := make([]*cluster.NodeInfo, 0, len(nodes))
	for _, node := range nodes {
		if node != nil && node.Status == cluster.NodeStatusRunning && node.Address != "" && (node.GRPCPort > 0 || node.Port > 0 || node.SocketPort > 0) {
			healthy = append(healthy, node)
		}
	}
	if len(healthy) == 0 {
		return nil, fmt.Errorf("%w: service=%s", ErrTargetServiceUnavailable, serviceName)
	}
	node, err := r.balancer.Pick(ctx, healthy, cluster.BalanceHint{})
	if err != nil {
		return nil, fmt.Errorf("%w: service=%s: %v", ErrTargetServiceUnavailable, serviceName, err)
	}
	return &ResolvedService{
		NodeID: node.ID,
		Info: &types.TargetInfo{
			TargetAddress: node.Address, TargetService: serviceName,
			TargetPort: node.Port, TargetSocketPort: node.SocketPort,
			TargetGRPCPort: node.GRPCPort,
		},
		Endpoints: serviceTransportEndpoints(node.Address, node.Port, node.GRPCPort, node.SocketPort),
	}, nil
}

func serviceTransportEndpoints(address string, httpPort, grpcPort, socketPort int) transport.TransportEndpoints {
	var endpoints transport.TransportEndpoints
	if grpcPort > 0 {
		endpoints.GRPC = net.JoinHostPort(address, strconv.Itoa(grpcPort))
	}
	if httpPort > 0 {
		endpoint := &url.URL{Scheme: "http", Host: net.JoinHostPort(address, strconv.Itoa(httpPort))}
		endpoints.HTTP = endpoint.String()
	}
	if socketPort > 0 {
		endpoints.Socket = net.JoinHostPort(address, strconv.Itoa(socketPort))
	}
	return endpoints
}

func (r *ServiceResolver) ensureEntry(ctx context.Context, serviceName string) (*resolverEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, fmt.Errorf("%w: resolver closed", ErrTargetServiceUnavailable)
	}
	if entry := r.entries[serviceName]; entry != nil {
		return entry, nil
	}
	if r.provider == nil {
		return nil, fmt.Errorf("%w: no discovery provider for %s", ErrTargetServiceUnavailable, serviceName)
	}
	nodes, err := r.provider.List(ctx, serviceName, cluster.NodeStatusRunning)
	if err != nil {
		return nil, fmt.Errorf("%w: list %s: %v", ErrTargetServiceUnavailable, serviceName, err)
	}
	entry := &resolverEntry{nodes: cloneResolverNodes(nodes)}
	cancel, err := r.provider.Watch(context.Background(), serviceName, func(updated []*cluster.NodeInfo) {
		r.mu.Lock()
		if current := r.entries[serviceName]; current != nil && !r.closed {
			current.nodes = cloneResolverNodes(updated)
		}
		r.mu.Unlock()
	})
	if err != nil {
		return nil, fmt.Errorf("%w: watch %s: %v", ErrTargetServiceUnavailable, serviceName, err)
	}
	entry.cancel = cancel
	r.entries[serviceName] = entry
	return entry, nil
}

func (r *ServiceResolver) SetProvider(provider cluster.DiscoveryProvider) {
	r.mu.Lock()
	entries := r.entries
	r.entries = make(map[string]*resolverEntry)
	r.provider = provider
	r.mu.Unlock()
	for _, entry := range entries {
		if entry.cancel != nil {
			entry.cancel()
		}
	}
}

func (r *ServiceResolver) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	entries := r.entries
	r.entries = nil
	r.mu.Unlock()
	for _, entry := range entries {
		if entry.cancel != nil {
			entry.cancel()
		}
	}
}

func cloneResolverNodes(source []*cluster.NodeInfo) []*cluster.NodeInfo {
	result := make([]*cluster.NodeInfo, 0, len(source))
	for _, node := range source {
		if node == nil {
			continue
		}
		copyNode := *node
		result = append(result, &copyNode)
	}
	return result
}
