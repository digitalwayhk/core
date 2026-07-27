package runtime

import (
	"context"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/observability"
)

// ClusterView 抽象集群拓扑读取。
type ClusterView interface {
	List(ctx context.Context, serviceName string, statuses ...cluster.NodeStatus) ([]*cluster.NodeInfo, error)
	// ListServices 返回已知服务名；实现可扫全量。
	ListServices(ctx context.Context) ([]string, error)
}

// SubscriptionEdge 异步边元数据。
type SubscriptionEdge struct {
	SourceSubjectFamily string
	EventType           string
	TargetService       string
}

// SubscriptionIndex 只读订阅索引。
type SubscriptionIndex interface {
	List(ctx context.Context) ([]SubscriptionEdge, error)
}

// Config Aggregator 配置。
type Config struct {
	Mode     string
	CacheTTL time.Duration
}

// PromQuerier 可测试的 Prometheus 查询接口。
type PromQuerier interface {
	Query(ctx context.Context, query string, ts time.Time) (Vector, error)
}

// Aggregator 合并集群拓扑与 Prometheus 指标。
type Aggregator struct {
	cluster ClusterView
	prom    PromQuerier
	subs    SubscriptionIndex
	cfg     Config

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	at   time.Time
	body any
}

// NewAggregator 创建聚合器。
func NewAggregator(cluster ClusterView, prom PromQuerier, cfg Config) *Aggregator {
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = 5 * time.Second
	}
	return &Aggregator{
		cluster: cluster,
		prom:    prom,
		cfg:     cfg,
		cache:   make(map[string]cacheEntry),
	}
}

// SetSubscriptions 设置异步边订阅索引。
func (a *Aggregator) SetSubscriptions(idx SubscriptionIndex) {
	a.subs = idx
}

// KnownService 判断服务是否在集群视图中。
func (a *Aggregator) KnownService(ctx context.Context, service string) bool {
	if a == nil || a.cluster == nil {
		return false
	}
	svc := observability.NormalizeServiceLabel(service)
	services, err := a.cluster.ListServices(ctx)
	if err != nil {
		return false
	}
	for _, s := range services {
		if observability.NormalizeServiceLabel(s) == svc {
			return true
		}
	}
	return false
}

// Topology 构建全局拓扑。
func (a *Aggregator) Topology(ctx context.Context, window string) (*TopologyResponse, error) {
	if _, ok := ParseWindow(window); !ok {
		return nil, fmtError("invalid window")
	}
	now := time.Now().UTC()
	resp := &TopologyResponse{
		GeneratedAt: now,
		Window:      window,
		Status:      StateOK,
		Services:    []ServiceNode{},
		Edges:       []ServiceEdge{},
		Warnings:    []RuntimeWarning{},
	}

	metricState := MapQueryState(QueryInput{Mode: a.cfg.Mode})
	if metricState == StateNotCollected {
		resp.Status = StateNotCollected
	}

	services, err := a.cluster.ListServices(ctx)
	if err != nil {
		resp.Status = StateUnavailable
		resp.Warnings = append(resp.Warnings, RuntimeWarning{
			Code: "cluster_unavailable", Message: "cluster provider is unavailable", Scope: "global",
		})
		return resp, nil
	}

	for _, name := range services {
		node := ServiceNode{
			Service:     observability.NormalizeServiceLabel(name),
			RequestRate: NullMetric(metricState),
			ErrorRate:   NullMetric(metricState),
			P50Ms:       NullMetric(metricState),
			P95Ms:       NullMetric(metricState),
			P99Ms:       NullMetric(metricState),
			State:       metricState,
		}
		nodes, listErr := a.cluster.List(ctx, name)
		if listErr != nil {
			resp.Status = MergeStates(resp.Status, StatePartial)
			resp.Warnings = append(resp.Warnings, RuntimeWarning{
				Code: "cluster_partial", Message: "failed to list some instances", Scope: name,
			})
		} else {
			for _, n := range nodes {
				node.RegisteredInstances++
				if n == nil {
					continue
				}
				switch n.Status {
				case cluster.NodeStatusRunning:
					node.RunningInstances++
				case cluster.NodeStatusOffline, cluster.NodeStatusSuspect:
					node.UnavailableInstances++
				}
			}
		}

		if a.cfg.Mode == "prometheus" && a.prom != nil {
			q, qerr := ServiceRequestRateQuery(name, window)
			if qerr == nil {
				vec, perr := a.prom.Query(ctx, q, now)
				if perr != nil {
					metricState = StateUnavailable
					node.RequestRate = NullMetric(StateUnavailable)
					node.State = StateUnavailable
					resp.Status = MergeStates(resp.Status, StateUnavailable)
					resp.Warnings = append(resp.Warnings, RuntimeWarning{
						Code: "prometheus_unavailable", Message: "metrics query failed", Scope: "global",
					})
				} else if len(vec) > 0 {
					node.RequestRate = ValueMetric(vec[0].Value, StateOK)
					node.State = StateOK
					metricState = StateOK
				} else {
					// 有查询能力但无样本：诚实为零，状态 ok
					node.RequestRate = ValueMetric(0, StateOK)
					node.State = StateOK
				}
			}
		}
		resp.Services = append(resp.Services, node)
	}

	// 同步边：从 call metrics 聚合（若 Prometheus 可用）。
	if a.cfg.Mode == "prometheus" && a.prom != nil && metricState != StateUnavailable {
		if q, err := ServiceCallEdgeRateQuery(window); err == nil {
			if _, err := a.prom.Query(ctx, q, now); err != nil {
				resp.Status = MergeStates(resp.Status, StatePartial)
			}
			// 完整向量标签解析留待后续增强；此处保留接口与状态路径。
		}
	}

	// 异步边：仅真实订阅。
	if a.subs != nil {
		if edges, err := a.subs.List(ctx); err == nil {
			for _, e := range edges {
				resp.Edges = append(resp.Edges, ServiceEdge{
					Source:        "", // 发布方由事件指标补齐；元数据边先标 target
					Target:        observability.NormalizeServiceLabel(e.TargetService),
					Kind:          "async",
					SubjectFamily: e.SourceSubjectFamily,
					RequestRate:   NullMetric(metricState),
					ErrorRate:     NullMetric(metricState),
					P95Ms:         NullMetric(metricState),
					State:         metricState,
				})
			}
		}
	}

	if len(resp.Services) == 0 {
		resp.Status = MergeStates(resp.Status, StatePartial)
	}
	return resp, nil
}

// ServiceDetail 构建服务详情。
func (a *Aggregator) ServiceDetail(ctx context.Context, window, service string) (*ServiceDetailResponse, error) {
	if _, ok := ParseWindow(window); !ok {
		return nil, fmtError("invalid window")
	}
	svc := observability.NormalizeServiceLabel(service)
	if svc == "unknown" || !observability.IsSafePromLabel(svc) {
		return nil, fmtError("unknown service")
	}
	if !a.KnownService(ctx, svc) {
		return nil, fmtError("unknown service")
	}

	topo, err := a.Topology(ctx, window)
	if err != nil {
		return nil, err
	}
	var node ServiceNode
	for _, s := range topo.Services {
		if s.Service == svc {
			node = s
			break
		}
	}
	metricState := MapQueryState(QueryInput{Mode: a.cfg.Mode})
	detail := &ServiceDetailResponse{
		GeneratedAt: time.Now().UTC(),
		Window:      window,
		Service:     node,
		Routes:      []RouteMetrics{},
		Instances:   []InstanceMetrics{},
		Components:  []ComponentSnapshot{},
		Warnings:    append([]RuntimeWarning{}, topo.Warnings...),
	}

	nodes, listErr := a.cluster.List(ctx, svc)
	if listErr == nil {
		for _, n := range nodes {
			if n == nil {
				continue
			}
			detail.Instances = append(detail.Instances, InstanceMetrics{
				InstanceID:  n.ID,
				Status:      string(n.Status),
				RequestRate: NullMetric(metricState),
				P95Ms:       NullMetric(metricState),
				State:       metricState,
			})
		}
	}

	// 组件默认 not_collected，直到 Prom 有 core_component_gauge。
	for _, name := range []string{"pending", "outbox", "eventbridge"} {
		detail.Components = append(detail.Components, ComponentSnapshot{
			Component: name,
			State:     StateNotCollected,
		})
	}
	return detail, nil
}

type simpleError string

func (e simpleError) Error() string { return string(e) }

func fmtError(msg string) error { return simpleError(msg) }
