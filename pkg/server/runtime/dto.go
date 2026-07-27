package runtime

import "time"

// MetricState 统一指标状态。
type MetricState string

const (
	StateOK           MetricState = "ok"
	StatePartial      MetricState = "partial"
	StateStale        MetricState = "stale"
	StateUnavailable  MetricState = "unavailable"
	StateNotCollected MetricState = "not_collected"
)

// MetricValue 表示可空数值 + 状态。
type MetricValue struct {
	Value       *float64    `json:"value"`
	State       MetricState `json:"state"`
	LastSample  *time.Time  `json:"last_sample,omitempty"`
	CoverageSec *float64    `json:"coverage_sec,omitempty"`
}

// RuntimeWarning 安全可展示的告警。
type RuntimeWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Scope   string `json:"scope,omitempty"`
}

// ServiceNode 全局图服务节点。
type ServiceNode struct {
	Service              string      `json:"service"`
	RegisteredInstances  int         `json:"registered_instances"`
	RunningInstances     int         `json:"running_instances"`
	UnavailableInstances int         `json:"unavailable_instances"`
	RequestRate          MetricValue `json:"request_rate"`
	ErrorRate            MetricValue `json:"error_rate"`
	P50Ms                MetricValue `json:"p50_ms"`
	P95Ms                MetricValue `json:"p95_ms"`
	P99Ms                MetricValue `json:"p99_ms"`
	State                MetricState `json:"state"`
}

// ServiceEdge 同步/异步调用边。
type ServiceEdge struct {
	Source        string      `json:"source"`
	Target        string      `json:"target"`
	Kind          string      `json:"kind"` // sync|async
	Protocol      string      `json:"protocol,omitempty"`
	SubjectFamily string      `json:"subject_family,omitempty"`
	RequestRate   MetricValue `json:"request_rate"`
	ErrorRate     MetricValue `json:"error_rate"`
	P95Ms         MetricValue `json:"p95_ms"`
	State         MetricState `json:"state"`
}

// TopologyResponse 全局拓扑响应。
type TopologyResponse struct {
	GeneratedAt time.Time        `json:"generated_at"`
	Window      string           `json:"window"`
	Status      MetricState      `json:"status"`
	Services    []ServiceNode    `json:"services"`
	Edges       []ServiceEdge    `json:"edges"`
	Warnings    []RuntimeWarning `json:"warnings"`
}

// RouteMetrics 服务内路由指标。
type RouteMetrics struct {
	Route       string      `json:"route"`
	Method      string      `json:"method,omitempty"`
	RequestRate MetricValue `json:"request_rate"`
	ErrorRate   MetricValue `json:"error_rate"`
	P50Ms       MetricValue `json:"p50_ms"`
	P95Ms       MetricValue `json:"p95_ms"`
	P99Ms       MetricValue `json:"p99_ms"`
	State       MetricState `json:"state"`
}

// InstanceMetrics 实例分布。
type InstanceMetrics struct {
	InstanceID  string      `json:"instance_id"`
	Status      string      `json:"status"`
	RequestRate MetricValue `json:"request_rate"`
	P95Ms       MetricValue `json:"p95_ms"`
	State       MetricState `json:"state"`
}

// ComponentSnapshot 服务内部组件。
type ComponentSnapshot struct {
	Component string             `json:"component"`
	State     MetricState        `json:"state"`
	Gauges    map[string]*float64 `json:"gauges,omitempty"`
}

// ServiceDetailResponse 服务详情。
type ServiceDetailResponse struct {
	GeneratedAt time.Time            `json:"generated_at"`
	Window      string               `json:"window"`
	Service     ServiceNode          `json:"service"`
	Routes      []RouteMetrics       `json:"routes"`
	Instances   []InstanceMetrics    `json:"instances"`
	Components  []ComponentSnapshot  `json:"components"`
	Warnings    []RuntimeWarning     `json:"warnings"`
}
