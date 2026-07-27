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

	// 同步边：从 call metrics 标签向量聚合。
	if a.cfg.Mode == "prometheus" && a.prom != nil && metricState != StateUnavailable {
		if q, err := ServiceCallEdgeRateQuery(window); err == nil {
			vec, qerr := a.prom.Query(ctx, q, now)
			if qerr != nil {
				resp.Status = MergeStates(resp.Status, StatePartial)
				resp.Warnings = append(resp.Warnings, RuntimeWarning{
					Code: "edge_query_partial", Message: "sync edge metrics query failed", Scope: "global",
				})
			} else {
				resp.Edges = append(resp.Edges, buildSyncEdges(vec, metricState)...)
			}
		}
	}

	// 异步边：仅真实订阅元数据。
	if a.subs != nil {
		if edges, err := a.subs.List(ctx); err == nil {
			for _, e := range edges {
				resp.Edges = append(resp.Edges, ServiceEdge{
					Source:        "", // 发布方由事件指标补齐；无发布指标时 source 为空
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
	now := nowUTC()
	detail := &ServiceDetailResponse{
		GeneratedAt: now,
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

	// 路由指标
	if a.cfg.Mode == "prometheus" && a.prom != nil {
		if q, err := ServiceRouteRateQuery(svc, window); err == nil {
			if vec, qerr := a.prom.Query(ctx, q, now); qerr == nil {
				detail.Routes = buildRouteMetrics(vec, metricState)
			} else {
				detail.Warnings = append(detail.Warnings, RuntimeWarning{
					Code: "route_query_partial", Message: "route metrics query failed", Scope: svc,
				})
			}
		}
	}

	// 组件：默认 not_collected；有 Prom 样本则填充。
	compState := map[string]*ComponentSnapshot{
		"pending":     {Component: "pending", State: StateNotCollected, Gauges: map[string]*float64{}},
		"outbox":      {Component: "outbox", State: StateNotCollected, Gauges: map[string]*float64{}},
		"eventbridge": {Component: "eventbridge", State: StateNotCollected, Gauges: map[string]*float64{}},
	}
	if a.cfg.Mode == "prometheus" && a.prom != nil {
		if q, err := ComponentGaugeQuery(svc); err == nil {
			if vec, qerr := a.prom.Query(ctx, q, now); qerr == nil {
				for _, sample := range vec {
					comp := observability.NormalizeServiceLabel(sample.Metric["component"])
					name := sample.Metric["name"]
					if snap, ok := compState[comp]; ok {
						v := sample.Value
						if snap.Gauges == nil {
							snap.Gauges = map[string]*float64{}
						}
						snap.Gauges[name] = &v
						snap.State = StateOK
					}
				}
			}
		}
	}
	for _, name := range []string{"pending", "outbox", "eventbridge"} {
		detail.Components = append(detail.Components, *compState[name])
	}
	return detail, nil
}

func buildSyncEdges(vec Vector, metricState MetricState) []ServiceEdge {
	type key struct{ src, tgt, proto string }
	type acc struct {
		total float64
		err   float64
	}
	bucket := map[key]*acc{}
	for _, s := range vec {
		k := key{
			src:   observability.NormalizeServiceLabel(s.Metric["source_service"]),
			tgt:   observability.NormalizeServiceLabel(s.Metric["target_service"]),
			proto: observability.NormalizeProtocol(s.Metric["protocol"]),
		}
		if k.src == "unknown" || k.tgt == "unknown" {
			continue
		}
		a := bucket[k]
		if a == nil {
			a = &acc{}
			bucket[k] = a
		}
		a.total += s.Value
		rc := s.Metric["result_class"]
		if rc != "" && rc != observability.ResultSuccess {
			a.err += s.Value
		}
	}
	out := make([]ServiceEdge, 0, len(bucket))
	for k, a := range bucket {
		var errRate *float64
		if a.total > 0 {
			v := a.err / a.total
			errRate = &v
		}
		edge := ServiceEdge{
			Source:      k.src,
			Target:      k.tgt,
			Kind:        "sync",
			Protocol:    k.proto,
			RequestRate: ValueMetric(a.total, StateOK),
			ErrorRate:   MetricValue{Value: errRate, State: StateOK},
			P95Ms:       NullMetric(metricState),
			State:       StateOK,
		}
		if metricState != StateOK && metricState != StateNotCollected {
			edge.State = metricState
			edge.RequestRate.State = metricState
			edge.ErrorRate.State = metricState
		}
		out = append(out, edge)
	}
	return out
}

func buildRouteMetrics(vec Vector, metricState MetricState) []RouteMetrics {
	type acc struct {
		total float64
		err   float64
	}
	bucket := map[string]*acc{}
	for _, s := range vec {
		route := observability.NormalizeRouteLabel(s.Metric["route"])
		a := bucket[route]
		if a == nil {
			a = &acc{}
			bucket[route] = a
		}
		a.total += s.Value
		if s.Metric["result_class"] != "" && s.Metric["result_class"] != observability.ResultSuccess {
			a.err += s.Value
		}
	}
	out := make([]RouteMetrics, 0, len(bucket))
	for route, a := range bucket {
		var errRate *float64
		if a.total > 0 {
			v := a.err / a.total
			errRate = &v
		}
		out = append(out, RouteMetrics{
			Route:       route,
			RequestRate: ValueMetric(a.total, StateOK),
			ErrorRate:   MetricValue{Value: errRate, State: StateOK},
			P50Ms:       NullMetric(metricState),
			P95Ms:       NullMetric(metricState),
			P99Ms:       NullMetric(metricState),
			State:       StateOK,
		})
	}
	return out
}

func nowUTC() time.Time { return time.Now().UTC() }

type simpleError string

func (e simpleError) Error() string { return string(e) }

func fmtError(msg string) error { return simpleError(msg) }
