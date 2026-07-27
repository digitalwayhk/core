package runtime

import (
	"context"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/observability"
	"golang.org/x/sync/semaphore"
)

// ClusterView 抽象集群拓扑读取。
type ClusterView interface {
	List(ctx context.Context, serviceName string, statuses ...cluster.NodeStatus) ([]*cluster.NodeInfo, error)
	ListServices(ctx context.Context) ([]string, error)
}

// SubscriptionEdge 异步边元数据。
type SubscriptionEdge struct {
	SourceSubjectFamily string
	EventType           string
	TargetService       string
	Reliable            bool
}

// SubscriptionIndex 只读订阅索引。
type SubscriptionIndex interface {
	List(ctx context.Context) ([]SubscriptionEdge, error)
}

// Config Aggregator 配置。
type Config struct {
	Mode                 string
	CacheTTL             time.Duration
	MaxConcurrentQueries int
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
	sem     *semaphore.Weighted

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	at   time.Time
	body any
}

// NewAggregator 创建聚合器。
func NewAggregator(cluster ClusterView, prom PromQuerier, cfg Config) *Aggregator {
	cfg.Mode = NormalizeMode(cfg.Mode)
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = 5 * time.Second
	}
	if cfg.MaxConcurrentQueries <= 0 {
		cfg.MaxConcurrentQueries = 4
	}
	return &Aggregator{
		cluster: cluster,
		prom:    prom,
		cfg:     cfg,
		sem:     semaphore.NewWeighted(int64(cfg.MaxConcurrentQueries)),
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

func (a *Aggregator) query(ctx context.Context, query string, ts time.Time) (Vector, error) {
	if a.prom == nil {
		return nil, ErrPrometheusUnavailable
	}
	if a.sem != nil {
		if err := a.sem.Acquire(ctx, 1); err != nil {
			return nil, err
		}
		defer a.sem.Release(1)
	}
	return a.prom.Query(ctx, query, ts)
}

func (a *Aggregator) cacheGet(key string) (any, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.cache[key]
	if !ok || time.Since(e.at) > a.cfg.CacheTTL {
		return nil, false
	}
	return e.body, true
}

func (a *Aggregator) cacheSet(key string, body any) {
	a.mu.Lock()
	a.cache[key] = cacheEntry{at: time.Now(), body: body}
	a.mu.Unlock()
}

// Topology 构建全局拓扑。
func (a *Aggregator) Topology(ctx context.Context, window string) (*TopologyResponse, error) {
	if _, ok := ParseWindow(window); !ok {
		return nil, fmtError("invalid window")
	}
	cacheKey := "topology|" + window
	if cached, ok := a.cacheGet(cacheKey); ok {
		if resp, ok := cached.(*TopologyResponse); ok {
			return resp, nil
		}
	}

	now := time.Now().UTC()
	mode := NormalizeMode(a.cfg.Mode)
	resp := &TopologyResponse{
		GeneratedAt: now,
		Window:      window,
		Status:      StateOK,
		Services:    []ServiceNode{},
		Edges:       []ServiceEdge{},
		Warnings:    []RuntimeWarning{},
	}

	baseMetricState := MapQueryState(QueryInput{Mode: mode})
	if baseMetricState == StateNotCollected {
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

	promEnabled := IsPrometheusMode(mode) && a.prom != nil
	anySample := false
	anyQueryFail := false

	for _, name := range services {
		node := ServiceNode{
			Service:     observability.NormalizeServiceLabel(name),
			RequestRate: NullMetric(baseMetricState),
			ErrorRate:   NullMetric(baseMetricState),
			P50Ms:       NullMetric(baseMetricState),
			P95Ms:       NullMetric(baseMetricState),
			P99Ms:       NullMetric(baseMetricState),
			State:       baseMetricState,
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

		if promEnabled {
			a.fillServiceMetrics(ctx, &node, name, window, now, &anySample, &anyQueryFail, resp)
		}
		resp.Services = append(resp.Services, node)
	}

	if promEnabled {
		if q, err := ServiceCallEdgeRateQuery(window); err == nil {
			vec, qerr := a.query(ctx, q, now)
			if qerr != nil {
				anyQueryFail = true
				resp.Status = MergeStates(resp.Status, StatePartial)
				resp.Warnings = append(resp.Warnings, RuntimeWarning{
					Code: "edge_query_partial", Message: "sync edge metrics query failed", Scope: "global",
				})
			} else if len(vec) > 0 {
				anySample = true
				resp.Edges = append(resp.Edges, buildSyncEdges(vec, StateOK)...)
			}
		}
		asyncEdges, asyncWarn, asyncSample, asyncFail := a.buildAsyncEdges(ctx, window, now, baseMetricState)
		resp.Edges = append(resp.Edges, asyncEdges...)
		resp.Warnings = append(resp.Warnings, asyncWarn...)
		if asyncSample {
			anySample = true
		}
		if asyncFail {
			anyQueryFail = true
		}
	}

	if anyQueryFail {
		resp.Status = MergeStates(resp.Status, StateUnavailable)
	} else if promEnabled && !anySample && len(services) > 0 {
		// 查询可用但全无样本：全局 partial + 各节点 not_collected（已在 fill 中设置）
		resp.Status = MergeStates(resp.Status, StatePartial)
		resp.Warnings = append(resp.Warnings, RuntimeWarning{
			Code: "metrics_empty", Message: "prometheus returned no samples for this window", Scope: "global",
		})
	}

	if len(resp.Services) == 0 {
		resp.Status = MergeStates(resp.Status, StatePartial)
	}
	a.cacheSet(cacheKey, resp)
	return resp, nil
}

func (a *Aggregator) fillServiceMetrics(
	ctx context.Context,
	node *ServiceNode,
	name, window string,
	now time.Time,
	anySample *bool,
	anyQueryFail *bool,
	resp *TopologyResponse,
) {
	// 请求率：HTTP code_total 向量求和；空向量 = not_collected，不伪装 0。
	rateQ, err := ServiceHTTPRateByCodeQuery(name, window)
	if err != nil {
		return
	}
	vec, qerr := a.query(ctx, rateQ, now)
	coreQ, _ := ServiceCoreRateByResultQuery(name, window)
	coreVec, coreErr := a.query(ctx, coreQ, now)

	if qerr != nil && coreErr != nil {
		*anyQueryFail = true
		node.RequestRate = NullMetric(StateUnavailable)
		node.ErrorRate = NullMetric(StateUnavailable)
		node.State = StateUnavailable
		resp.Status = MergeStates(resp.Status, StateUnavailable)
		return
	}

	httpTotal, httpErr := sumHTTPByCode(vec)
	coreTotal, coreErrN := sumCoreByResult(coreVec)
	total := httpTotal + coreTotal
	errs := httpErr + coreErrN

	if (qerr == nil && len(vec) == 0) && (coreErr == nil && len(coreVec) == 0) {
		// 真·无样本
		node.RequestRate = NullMetric(StateNotCollected)
		node.ErrorRate = NullMetric(StateNotCollected)
		node.P50Ms = NullMetric(StateNotCollected)
		node.P95Ms = NullMetric(StateNotCollected)
		node.P99Ms = NullMetric(StateNotCollected)
		node.State = StateNotCollected
		return
	}

	*anySample = true
	node.RequestRate = ValueMetric(total, StateOK)
	if total > 0 {
		node.ErrorRate = ValueMetric(errs/total, StateOK)
	} else {
		node.ErrorRate = ValueMetric(0, StateOK)
	}
	// 百分位：有 HTTP histogram 则取；空则 not_collected（不伪造 0ms）
	node.P50Ms = a.queryQuantile(ctx, name, window, now, 0.50)
	node.P95Ms = a.queryQuantile(ctx, name, window, now, 0.95)
	node.P99Ms = a.queryQuantile(ctx, name, window, now, 0.99)
	node.State = StateOK
	if qerr != nil || coreErr != nil {
		node.State = StatePartial
		resp.Status = MergeStates(resp.Status, StatePartial)
	}
}

func (a *Aggregator) queryQuantile(ctx context.Context, name, window string, now time.Time, q float64) MetricValue {
	var query string
	var err error
	switch q {
	case 0.50:
		query, err = ServiceHTTPP50Query(name, window)
	case 0.99:
		query, err = ServiceHTTPP99Query(name, window)
	default:
		query, err = ServiceHTTPP95Query(name, window)
	}
	if err != nil {
		return NullMetric(StateNotCollected)
	}
	vec, qerr := a.query(ctx, query, now)
	if qerr != nil {
		return NullMetric(StateUnavailable)
	}
	if len(vec) == 0 || (len(vec) == 1 && (vec[0].Value != vec[0].Value)) { // NaN
		return NullMetric(StateNotCollected)
	}
	v := vec[0].Value
	if v != v { // NaN from histogram_quantile with no data
		return NullMetric(StateNotCollected)
	}
	return ValueMetric(v, StateOK)
}

func sumHTTPByCode(vec Vector) (total, errs float64) {
	for _, s := range vec {
		total += s.Value
		code := s.Metric["code"]
		if len(code) > 0 && code[0] >= '4' {
			errs += s.Value
		}
	}
	return total, errs
}

func sumCoreByResult(vec Vector) (total, errs float64) {
	for _, s := range vec {
		total += s.Value
		if s.Metric["result_class"] != "" && s.Metric["result_class"] != observability.ResultSuccess {
			errs += s.Value
		}
	}
	return total, errs
}

func (a *Aggregator) buildAsyncEdges(ctx context.Context, window string, now time.Time, baseState MetricState) ([]ServiceEdge, []RuntimeWarning, bool, bool) {
	var warnings []RuntimeWarning
	var edges []ServiceEdge
	anySample := false
	anyFail := false

	var subs []SubscriptionEdge
	if a.subs != nil {
		list, err := a.subs.List(ctx)
		if err != nil {
			warnings = append(warnings, RuntimeWarning{Code: "subscription_index_error", Message: "failed to list subscriptions", Scope: "global"})
		} else {
			subs = list
		}
	}
	if len(subs) == 0 {
		return edges, warnings, false, false
	}

	publishRates := map[string]float64{} // family|type|source -> rate
	if q, err := EventPublishRateQuery(window); err == nil {
		vec, qerr := a.query(ctx, q, now)
		if qerr != nil {
			anyFail = true
			warnings = append(warnings, RuntimeWarning{Code: "event_publish_query_partial", Message: "event publish metrics query failed", Scope: "global"})
		} else {
			for _, s := range vec {
				anySample = true
				key := s.Metric["subject_family"] + "|" + s.Metric["event_type"] + "|" + s.Metric["source_service"]
				if s.Metric["result_class"] == observability.ResultSuccess || s.Metric["result_class"] == "" {
					publishRates[key] += s.Value
				}
			}
		}
	}

	// 发布存在但无订阅：warning
	// 订阅存在：与发布 join 成异步边
	seenPublishFamily := map[string]bool{}
	for k := range publishRates {
		// k = family|type|source
		parts := split3(k)
		if parts[0] != "" {
			seenPublishFamily[parts[0]] = true
		}
	}

	subFamilies := map[string]bool{}
	for _, sub := range subs {
		subFamilies[sub.SourceSubjectFamily] = true
		// 找匹配发布
		var bestSource string
		var bestRate float64
		found := false
		for k, rate := range publishRates {
			parts := split3(k)
			if parts[0] != sub.SourceSubjectFamily {
				continue
			}
			if sub.EventType != "" && parts[1] != sub.EventType && parts[1] != "unspecified" {
				continue
			}
			if !found || rate > bestRate {
				found = true
				bestRate = rate
				bestSource = parts[2]
			}
		}
		edge := ServiceEdge{
			Source:        bestSource,
			Target:        sub.TargetService,
			Kind:          "async",
			SubjectFamily: sub.SourceSubjectFamily,
			RequestRate:   NullMetric(baseState),
			ErrorRate:     NullMetric(baseState),
			P95Ms:         NullMetric(baseState),
			State:         baseState,
		}
		if found {
			edge.RequestRate = ValueMetric(bestRate, StateOK)
			edge.State = StateOK
			if bestSource == "" {
				edge.State = StatePartial
			}
		} else {
			// 有订阅无发布样本
			edge.RequestRate = NullMetric(StateNotCollected)
			edge.State = StateNotCollected
			warnings = append(warnings, RuntimeWarning{
				Code:    "async_publish_missing",
				Message: "subscription exists but no publish samples for subject family",
				Scope:   sub.TargetService,
			})
		}
		edges = append(edges, edge)
	}

	for family := range seenPublishFamily {
		if !subFamilies[family] {
			warnings = append(warnings, RuntimeWarning{
				Code:    "async_subscription_missing",
				Message: "publish samples exist without registered subscribers",
				Scope:   family,
			})
		}
	}
	return edges, warnings, anySample, anyFail
}

func split3(k string) [3]string {
	var out [3]string
	parts := make([]string, 0, 3)
	start := 0
	for i := 0; i < len(k); i++ {
		if k[i] == '|' {
			parts = append(parts, k[start:i])
			start = i + 1
		}
	}
	parts = append(parts, k[start:])
	for i := 0; i < 3 && i < len(parts); i++ {
		out[i] = parts[i]
	}
	return out
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

	cacheKey := "service|" + window + "|" + svc
	if cached, ok := a.cacheGet(cacheKey); ok {
		if detail, ok := cached.(*ServiceDetailResponse); ok {
			return detail, nil
		}
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
	metricState := MapQueryState(QueryInput{Mode: NormalizeMode(a.cfg.Mode)})
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
			id := n.ServiceInstanceID
			if id == "" {
				id = n.ID
			}
			detail.Instances = append(detail.Instances, InstanceMetrics{
				InstanceID:  id,
				Status:      string(n.Status),
				RequestRate: NullMetric(metricState),
				P95Ms:       NullMetric(metricState),
				State:       metricState,
			})
		}
	}

	if IsPrometheusMode(a.cfg.Mode) && a.prom != nil {
		// 合并 HTTP path + Core route
		routes := map[string]*routeAcc{}
		if q, err := ServiceHTTPRouteRateQuery(svc, window); err == nil {
			if vec, qerr := a.query(ctx, q, now); qerr == nil {
				for _, s := range vec {
					path := observability.NormalizeRouteLabel(s.Metric["path"])
					if path == "invalid_route" {
						continue
					}
					acc := routes[path]
					if acc == nil {
						acc = &routeAcc{}
						routes[path] = acc
					}
					acc.total += s.Value
					code := s.Metric["code"]
					if len(code) > 0 && code[0] >= '4' {
						acc.err += s.Value
					}
				}
			}
		}
		if q, err := ServiceRouteRateQuery(svc, window); err == nil {
			if vec, qerr := a.query(ctx, q, now); qerr == nil {
				for _, s := range vec {
					path := observability.NormalizeRouteLabel(s.Metric["route"])
					acc := routes[path]
					if acc == nil {
						acc = &routeAcc{}
						routes[path] = acc
					}
					acc.total += s.Value
					if s.Metric["result_class"] != "" && s.Metric["result_class"] != observability.ResultSuccess {
						acc.err += s.Value
					}
				}
			} else {
				detail.Warnings = append(detail.Warnings, RuntimeWarning{
					Code: "route_query_partial", Message: "route metrics query failed", Scope: svc,
				})
			}
		}
		for path, acc := range routes {
			var errRate *float64
			if acc.total > 0 {
				v := acc.err / acc.total
				errRate = &v
			}
			state := StateOK
			if acc.total == 0 {
				state = StateNotCollected
			}
			detail.Routes = append(detail.Routes, RouteMetrics{
				Route:       path,
				RequestRate: metricOrNotCollected(acc.total, state),
				ErrorRate:   MetricValue{Value: errRate, State: state},
				P50Ms:       NullMetric(StateNotCollected),
				P95Ms:       NullMetric(StateNotCollected),
				P99Ms:       NullMetric(StateNotCollected),
				State:       state,
			})
		}

		compState := map[string]*ComponentSnapshot{
			"pending":     {Component: "pending", State: StateNotCollected, Gauges: map[string]*float64{}},
			"outbox":      {Component: "outbox", State: StateNotCollected, Gauges: map[string]*float64{}},
			"eventbridge": {Component: "eventbridge", State: StateNotCollected, Gauges: map[string]*float64{}},
		}
		if q, err := ComponentGaugeQuery(svc); err == nil {
			if vec, qerr := a.query(ctx, q, now); qerr == nil {
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
		for _, name := range []string{"pending", "outbox", "eventbridge"} {
			detail.Components = append(detail.Components, *compState[name])
		}
	} else {
		for _, name := range []string{"pending", "outbox", "eventbridge"} {
			detail.Components = append(detail.Components, ComponentSnapshot{Component: name, State: StateNotCollected})
		}
	}

	a.cacheSet(cacheKey, detail)
	return detail, nil
}

type routeAcc struct {
	total float64
	err   float64
}

func metricOrNotCollected(v float64, state MetricState) MetricValue {
	if state == StateNotCollected {
		return NullMetric(StateNotCollected)
	}
	return ValueMetric(v, state)
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
		out = append(out, edge)
	}
	return out
}

func nowUTC() time.Time { return time.Now().UTC() }

type simpleError string

func (e simpleError) Error() string { return string(e) }

func fmtError(msg string) error { return simpleError(msg) }
