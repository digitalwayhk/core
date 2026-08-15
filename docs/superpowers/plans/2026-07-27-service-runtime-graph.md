# 多服务运行与请求聚合监控 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.  
> **本仓库约束：** `Claude.md` 禁止派发子智能体；必须由当前主 Agent 串行执行，不得使用 subagent-driven-development。

**Goal:** 在 go-zero Prometheus/zrpc/OpenTelemetry 之上补齐 Core 调用边、gRPC 服务端与组件指标，经 ServerManage Runtime API 向 Web Admin 提供全局服务运行与服务请求视图，并以示例 07 做真实多副本验收；稳定后按兼容流程废弃旧 `RouterStats`。

**Architecture:** 各业务进程只负责**低基数暴露**（go-zero 通用指标 + Core 自定义 Collector）；**Prometheus 是唯一历史指标源**。Runtime Aggregator 部署在 **ServerManage 可达边界**（通常是持有 ServerManage 路由的 Web/管理进程），从 `ClusterProvider` 取全集群拓扑，从 Prometheus 查询窗口指标，合并后返回 Admin DTO。`RuntimeMetricProvider` 只在本进程注册 Collector，Aggregator **不**在请求路径直连各实例 Provider。

**Tech Stack:** Go 1.26、go-zero v1.10.2（`http_server_*` / `rpc_client_*` / `rpc_server_*`）、prometheus client_golang、OpenTelemetry、ClusterProvider、ServiceResolver、示例 07、Ant Design Pro / Umi Web Admin、testify。

**规格：** `docs/superpowers/specs/2026-07-27-service-runtime-graph-design.md`（提交 `00e82a0`）

---

## 实现决策（审阅钉死，执行时不得改口径）

| ID | 决策 |
| --- | --- |
| D1 | **HTTP 入站**：复用 go-zero `http_server_requests_duration_ms` / `http_server_requests_code_total`（标签 `path,method,code`，单位 ms）。Core 注册路径必须是 RouterInfo 稳定模板；Aggregator 用 `path` 对齐 `RouterInfo.GetPath()`。 |
| D2 | **gRPC 入站**：go-zero/zrpc 服务端 interceptor 只能标 `method=/CoreTransport/Call`，**不足**。在身份校验与路由解析成功后，额外写 Core 指标 `core_service_request_*`（`service,route,result_class`）。 |
| D3 | **调用边**：在 `ServiceContext.CallService` / `invokePayload`/`sendPayload` 成功解析目标后记录 `core_service_call_*`；标签只用注册元数据中的 source/target/route/protocol。 |
| D4 | **进程标签**：每个 scrape target 必须带稳定 `service` 与 `service_instance_id`。优先在进程启动时通过 Core 注册 `prometheus.WrapRegistererWith(ConstLabels)` 或等价机制注入；示例 07 compose 再做 relabel 兜底，二者至少一处强制存在。 |
| D5 | **Provider 单向流**：`RuntimeMetricProvider` → 本进程 Collector → Prometheus → Aggregator。查询路径禁止直连远程实例 Provider。 |
| D6 | **Aggregator 部署**：只在 ServerManage 边界启用查询端配置 `RuntimeObservability`；业务 worker 只需 scrape 暴露，不强制配置 QueryURL。 |
| D7 | **状态映射**：`Mode=off` 或缺配置 → `not_collected`；`Mode=prometheus` 且查询失败/超时 → `unavailable`；有样本但 `now-last_sample > max(2×window, 30s)` → `stale`；部分实例/组件/保留不足 → `partial`。 |
| D8 | **单位**：Core 自定义 Histogram 与 go-zero 一致使用 **ms**；DTO 百分位字段为 `*float64` 毫秒，无样本为 `null`。 |
| D9 | **交付切分**：观测面 → 查询/API 面 → 07 UAT → Admin UI → 废弃登记；破坏性删除 `RouterStats` **不在本计划合并范围内**，仅完成登记与停用生产路径。 |
| D10 | **第一版组件 MVP**：ReliableWrite/Pending、Outbox、EventBridge；MySQL/WebSocket/Cache 接口预留，07 验收不阻塞时标 `not_collected`。 |

---

## 冻结契约

### 配置

```go
// pkg/server/config/runtime_observability.go
type RuntimeObservabilityConfig struct {
	Mode                 string        `json:",optional,default=off"` // off | prometheus
	QueryURL             string        `json:",optional"`
	QueryTimeout         time.Duration `json:",optional,default=3s"`
	MaxConcurrentQueries int           `json:",optional,default=4"`
	CacheTTL             time.Duration `json:",optional,default=5s"`
}
```

- `Mode=prometheus` 时 `QueryURL` 必填且必须是 `http`/`https` URL。
- 非法 Mode/Timeout/并发/CacheTTL 在 `Validate` 失败。
- QueryURL **不得**进入 AdminView、bootstrap 响应、日志字段。

### 指标名

| 名称 | 类型 | 标签 | 说明 |
| --- | --- | --- | --- |
| `http_server_requests_duration_ms` | histogram | path,method,code | go-zero 已有 |
| `http_server_requests_code_total` | counter | path,method,code | go-zero 已有 |
| `rpc_client_requests_duration_ms` | histogram | method | go-zero 已有（仅辅助，不作调用边权威） |
| `core_service_call_requests_total` | counter | source_service,target_service,target_route,protocol,result_class | Core 调用边 |
| `core_service_call_duration_ms` | histogram | source_service,target_service,target_route,protocol | Core 调用边耗时 |
| `core_service_request_requests_total` | counter | service,route,protocol,result_class | Core 入站（HTTP 可选用；gRPC 必须） |
| `core_service_request_duration_ms` | histogram | service,route,protocol | Core 入站耗时 |
| `core_component_*` | gauge/counter | service,component,name | 组件快照导出 |

`result_class` 枚举（闭集）：`success` | `client_error` | `server_error` | `timeout` | `unavailable` | `rejected`。

### Runtime API

```text
POST /api/servermanage/runtimetopology
POST /api/servermanage/runtimeservice
```

路径遵循现有 `ServerRouterInfo` 约定（`/api/servermanage/` + 结构体名小写），**不要**手写嵌套 REST 段。服务名通过 JSON body 传递，避免路径参数注入 PromQL。

请求：

```json
// RuntimeTopology
{"window":"15s"}

// RuntimeService
{"window":"15s","service":"shop-order"}
```

`window` 仅允许：`15s` | `5m` | `1h`。

响应公共字段：

```go
type MetricState string // ok|partial|stale|unavailable|not_collected

type MetricValue struct {
	Value       *float64    `json:"value"` // null = 非数值
	State       MetricState `json:"state"`
	LastSample  *time.Time  `json:"last_sample,omitempty"`
	CoverageSec *float64    `json:"coverage_sec,omitempty"`
}

type RuntimeWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Scope   string `json:"scope,omitempty"` // service|edge|component|global
}

type TopologyResponse struct {
	GeneratedAt time.Time       `json:"generated_at"`
	Window      string          `json:"window"`
	Status      MetricState     `json:"status"`
	Services    []ServiceNode   `json:"services"`
	Edges       []ServiceEdge   `json:"edges"`
	Warnings    []RuntimeWarning`json:"warnings"`
}

type ServiceNode struct {
	Service            string      `json:"service"`
	RegisteredInstances int        `json:"registered_instances"`
	RunningInstances    int        `json:"running_instances"`
	UnavailableInstances int       `json:"unavailable_instances"`
	RequestRate        MetricValue `json:"request_rate"`
	ErrorRate          MetricValue `json:"error_rate"`
	P50Ms              MetricValue `json:"p50_ms"`
	P95Ms              MetricValue `json:"p95_ms"`
	P99Ms              MetricValue `json:"p99_ms"`
	State              MetricState `json:"state"`
}

type ServiceEdge struct {
	Source      string      `json:"source"`
	Target      string      `json:"target"`
	Kind        string      `json:"kind"` // sync|async
	Protocol    string      `json:"protocol,omitempty"`
	SubjectFamily string    `json:"subject_family,omitempty"`
	RequestRate MetricValue `json:"request_rate"`
	ErrorRate   MetricValue `json:"error_rate"`
	P95Ms       MetricValue `json:"p95_ms"`
	State       MetricState `json:"state"`
}

type ServiceDetailResponse struct {
	GeneratedAt time.Time            `json:"generated_at"`
	Window      string               `json:"window"`
	Service     ServiceNode          `json:"service"`
	Routes      []RouteMetrics       `json:"routes"`
	Instances   []InstanceMetrics    `json:"instances"`
	Components  []ComponentSnapshot  `json:"components"`
	Warnings    []RuntimeWarning     `json:"warnings"`
}
```

---

## 文件结构

```text
pkg/server/config/
  runtime_observability.go          # 配置 + defaults/validate
  runtime_observability_test.go
  serverconfig.go                   # 嵌入 RuntimeObservability
docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md  # 新字段

pkg/server/observability/
  labels.go                         # 标签规范化、result_class
  labels_test.go
  metrics.go                        # Core counter/histogram 定义
  metrics_test.go
  process_labels.go                 # service/service_instance_id 注册
  calledge.go                       # RecordCall
  request.go                        # RecordInboundRequest
  provider.go                       # RuntimeMetricProvider + registry
  collector.go                      # Provider → Prometheus Collector
  collector_test.go

pkg/server/runtime/
  dto.go                            # Admin DTO（上表）
  state.go                          # 状态合并/stale 阈值
  state_test.go
  promql.go                         # 查询模板（无用户输入拼接）
  promql_test.go
  promclient.go                     # Prometheus HTTP API 客户端
  promclient_test.go
  aggregator.go                     # 拓扑/服务详情合并
  aggregator_test.go
  cache.go                          # 短缓存

pkg/server/transport/grpc/
  server.go                         # 挂 Unary interceptors
  server_metrics.go                 # 拦截器装配
  server_metrics_test.go

pkg/server/router/
  servicecontext.go                 # CallService 记录调用边；注册 collector
  servicecontext_call_metrics_test.go

pkg/server/api/public/
  runtimetopology.go
  runtimetopology_test.go
  runtimeservice.go
  runtimeservice_test.go
  release/routes.go                 # 注册 Runtime 路由；保持 Statistics 不注册

examples/07-shop-order-scale/deploy/
  docker-compose.yml                # + prometheus
  prometheus.yml
  # 各服务配置 RuntimeObservability / Prometheus scrape

web/admin/src/
  services/runtime.ts
  pages/MonitorSystem/              # 替换旧 statistics 数据源
  pages/MonitorSystem/Graph.tsx
  pages/MonitorSystem/ServiceView.tsx
  pages/MonitorSystem/types.ts

docs/codex/DEPRECATION_REGISTER.md  # RouterStats 废弃登记
docs/codex/API_COMPATIBILITY_SURFACE.md
```

---

### Task 1: 标签规范化与 result_class

**Files:**
- Create: `pkg/server/observability/labels.go`
- Create: `pkg/server/observability/labels_test.go`

- [ ] **Step 1: 写失败测试**

```go
package observability_test

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/server/observability"
	"github.com/stretchr/testify/require"
)

func TestNormalizeServiceLabel(t *testing.T) {
	require.Equal(t, "shop-order", observability.NormalizeServiceLabel(" Shop-Order "))
	require.Equal(t, "unknown", observability.NormalizeServiceLabel(""))
	require.Equal(t, "unknown", observability.NormalizeServiceLabel("shop order")) // 拒绝空白服务名片段
}

func TestNormalizeRouteLabel(t *testing.T) {
	require.Equal(t, "/api/shop-order/addorder", observability.NormalizeRouteLabel("/api/shop-order/addorder"))
	require.Equal(t, "invalid_route", observability.NormalizeRouteLabel("/api/x?id=1"))
	require.Equal(t, "invalid_route", observability.NormalizeRouteLabel(""))
}

func TestClassifyResult(t *testing.T) {
	require.Equal(t, observability.ResultSuccess, observability.ClassifyHTTPStatus(200))
	require.Equal(t, observability.ResultClientError, observability.ClassifyHTTPStatus(404))
	require.Equal(t, observability.ResultServerError, observability.ClassifyHTTPStatus(500))
	require.Equal(t, observability.ResultTimeout, observability.ClassifyError(context.DeadlineExceeded))
}
```

- [ ] **Step 2: 运行确认 RED**

```bash
GOCACHE=/private/tmp/core-codex-gocache go test ./pkg/server/observability -count=1
```

Expected: FAIL，包不存在。

- [ ] **Step 3: 最小实现**

```go
package observability

import (
	"context"
	"errors"
	"net"
	"strings"
	"unicode"
)

const (
	ResultSuccess     = "success"
	ResultClientError = "client_error"
	ResultServerError = "server_error"
	ResultTimeout     = "timeout"
	ResultUnavailable = "unavailable"
	ResultRejected    = "rejected"
)

func NormalizeServiceLabel(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return "unknown"
	}
	for _, r := range v {
		if unicode.IsSpace(r) {
			return "unknown"
		}
	}
	return v
}

func NormalizeRouteLabel(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.ContainsAny(v, "?#") || !strings.HasPrefix(v, "/") {
		return "invalid_route"
	}
	return v
}

func ClassifyHTTPStatus(code int) string {
	switch {
	case code >= 200 && code < 400:
		return ResultSuccess
	case code >= 400 && code < 500:
		return ResultClientError
	default:
		return ResultServerError
	}
}

func ClassifyError(err error) string {
	if err == nil {
		return ResultSuccess
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ResultTimeout
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return ResultTimeout
	}
	return ResultUnavailable
}
```

- [ ] **Step 4: 运行确认 GREEN**

```bash
GOCACHE=/private/tmp/core-codex-gocache go test ./pkg/server/observability -count=1
```

- [ ] **Step 5: Commit**

```bash
git add pkg/server/observability/labels.go pkg/server/observability/labels_test.go
git commit -m "$(cat <<'EOF'
feat(observability): add low-cardinality label helpers

EOF
)"
```

---

### Task 2: Core 指标定义（call + request）

**Files:**
- Create: `pkg/server/observability/metrics.go`
- Create: `pkg/server/observability/metrics_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestRecordCallIncrementsCounter(t *testing.T) {
	observability.ResetMetricsForTest(t)
	observability.RecordCall(observability.CallLabels{
		SourceService: "shop-user",
		TargetService: "shop-order",
		TargetRoute:   "/api/shop-order/addorder",
		Protocol:      "grpc",
		ResultClass:   observability.ResultSuccess,
	}, 12*time.Millisecond)

	// 使用 prometheus testutil 或自定义 Gather 断言
	count := observability.TestGatherCallTotal(t, "shop-user", "shop-order", "/api/shop-order/addorder", "grpc", "success")
	require.Equal(t, 1.0, count)
}

func TestRecordCallRejectsHighCardinalityRoute(t *testing.T) {
	observability.ResetMetricsForTest(t)
	observability.RecordCall(observability.CallLabels{
		SourceService: "shop-user",
		TargetService: "shop-order",
		TargetRoute:   "/api/x?id=1",
		Protocol:      "grpc",
		ResultClass:   observability.ResultSuccess,
	}, time.Millisecond)
	count := observability.TestGatherCallTotal(t, "shop-user", "shop-order", "invalid_route", "grpc", "success")
	require.Equal(t, 1.0, count)
}
```

- [ ] **Step 2: RED**

```bash
GOCACHE=/private/tmp/core-codex-gocache go test ./pkg/server/observability -run RecordCall -count=1
```

- [ ] **Step 3: 用 go-zero `core/metric` 定义向量**

```go
package observability

import (
	"time"

	"github.com/zeromicro/go-zero/core/metric"
)

var (
	callRequests = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "core",
		Subsystem: "service_call",
		Name:      "requests_total",
		Help:      "core inter-service call results",
		Labels:    []string{"source_service", "target_service", "target_route", "protocol", "result_class"},
	})
	callDuration = metric.NewHistogramVec(&metric.HistogramVecOpts{
		Namespace: "core",
		Subsystem: "service_call",
		Name:      "duration_ms",
		Help:      "core inter-service call duration(ms)",
		Labels:    []string{"source_service", "target_service", "target_route", "protocol"},
		Buckets:   []float64{1, 2, 5, 10, 25, 50, 100, 250, 500, 1000, 2000, 5000},
	})
	requestTotal = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "core",
		Subsystem: "service_request",
		Name:      "requests_total",
		Help:      "core inbound requests by stable route",
		Labels:    []string{"service", "route", "protocol", "result_class"},
	})
	requestDuration = metric.NewHistogramVec(&metric.HistogramVecOpts{
		Namespace: "core",
		Subsystem: "service_request",
		Name:      "duration_ms",
		Help:      "core inbound request duration(ms)",
		Labels:    []string{"service", "route", "protocol"},
		Buckets:   []float64{1, 2, 5, 10, 25, 50, 100, 250, 500, 1000, 2000, 5000},
	})
)

type CallLabels struct {
	SourceService, TargetService, TargetRoute, Protocol, ResultClass string
}

func RecordCall(l CallLabels, d time.Duration) {
	src := NormalizeServiceLabel(l.SourceService)
	tgt := NormalizeServiceLabel(l.TargetService)
	route := NormalizeRouteLabel(l.TargetRoute)
	proto := normalizeProtocol(l.Protocol)
	result := normalizeResult(l.ResultClass)
	callRequests.Inc(src, tgt, route, proto, result)
	callDuration.Observe(float64(d.Milliseconds()), src, tgt, route, proto)
}

func RecordInboundRequest(service, route, protocol, result string, d time.Duration) {
	svc := NormalizeServiceLabel(service)
	r := NormalizeRouteLabel(route)
	p := normalizeProtocol(protocol)
	rc := normalizeResult(result)
	requestTotal.Inc(svc, r, p, rc)
	requestDuration.Observe(float64(d.Milliseconds()), svc, r, p)
}
```

提供 `ResetMetricsForTest` / `TestGather*` 仅测试可见（`//go:build` 不必；用 `t.Helper` + registry gather 即可）。

- [ ] **Step 4: GREEN + commit**

```bash
GOCACHE=/private/tmp/core-codex-gocache go test ./pkg/server/observability -count=1
git add pkg/server/observability
git commit -m "$(cat <<'EOF'
feat(observability): define core service call and request metrics

EOF
)"
```

---

### Task 3: RuntimeObservability 配置契约

**Files:**
- Create: `pkg/server/config/runtime_observability.go`
- Create: `pkg/server/config/runtime_observability_test.go`
- Modify: `pkg/server/config/serverconfig.go`
- Modify: `docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`
- Modify: `pkg/server/config/capability_matrix_test.go`（若矩阵断言需更新）

- [ ] **Step 1: 写失败测试**

```go
func TestRuntimeObservabilityDefaults(t *testing.T) {
	var c config.RuntimeObservabilityConfig
	c.ApplyDefaults()
	require.Equal(t, "off", c.Mode)
	require.Equal(t, 3*time.Second, c.QueryTimeout)
	require.Equal(t, 4, c.MaxConcurrentQueries)
	require.Equal(t, 5*time.Second, c.CacheTTL)
}

func TestRuntimeObservabilityValidatePrometheusRequiresURL(t *testing.T) {
	c := config.RuntimeObservabilityConfig{Mode: "prometheus"}
	c.ApplyDefaults()
	require.ErrorContains(t, c.Validate(), "QueryURL")
}

func TestRuntimeObservabilityValidateRejectsBadMode(t *testing.T) {
	c := config.RuntimeObservabilityConfig{Mode: "memory"}
	require.ErrorContains(t, c.Validate(), "Mode")
}

func TestServerConfigValidateIncludesRuntimeObservability(t *testing.T) {
	cfg := config.NewServiceDefaultConfig("demo", 8080) // 使用仓库现有构造器
	cfg.RuntimeObservability.Mode = "prometheus"
	cfg.RuntimeObservability.QueryURL = "://bad"
	require.Error(t, cfg.Validate())
}
```

- [ ] **Step 2: RED**

```bash
GOCACHE=/private/tmp/core-codex-gocache go test ./pkg/server/config -run RuntimeObservability -count=1
```

- [ ] **Step 3: 实现配置并挂到 ServerConfig**

在 `ServerConfig` 增加：

```go
RuntimeObservability RuntimeObservabilityConfig `json:",optional"`
```

`ApplyDefaults`/`Validate` 链式调用子配置。矩阵新增行：

| path | status | owner | evidence |
| --- | --- | --- | --- |
| `ServerConfig.RuntimeObservability` | supported | runtime aggregator | ApplyDefaults/Validate；Mode off/prometheus |
| `ServerConfig.RuntimeObservability.Mode` | supported | runtime aggregator | off 默认；prometheus 需 QueryURL |
| `ServerConfig.RuntimeObservability.QueryURL` | supported | runtime aggregator | 仅查询端；不进 AdminView |
| `ServerConfig.RuntimeObservability.QueryTimeout` | supported | runtime aggregator | 默认 3s |
| `ServerConfig.RuntimeObservability.MaxConcurrentQueries` | supported | runtime aggregator | 默认 4 |
| `ServerConfig.RuntimeObservability.CacheTTL` | supported | runtime aggregator | 默认 5s |

- [ ] **Step 4: GREEN**

```bash
GOCACHE=/private/tmp/core-codex-gocache go test ./pkg/server/config -count=1
./scripts/test.sh config-contract
```

- [ ] **Step 5: Commit**

```bash
git add pkg/server/config docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md
git commit -m "$(cat <<'EOF'
feat(config): add RuntimeObservability query-side settings

EOF
)"
```

---

### Task 4: 进程级 `service` / `service_instance_id` 标签

**Files:**
- Create: `pkg/server/observability/process_labels.go`
- Create: `pkg/server/observability/process_labels_test.go`
- Modify: `pkg/server/router/servicecontext.go`（启动时注册一次）

- [ ] **Step 1: 测试**

```go
func TestProcessLabelsRegisteredOnce(t *testing.T) {
	observability.RegisterProcessLabels("shop-order", "shop-order-dc1-m2")
	// 再次注册相同值应幂等；不同值应 error 或忽略后到（锁定一种：返回 error）
	err := observability.RegisterProcessLabels("shop-order", "other")
	require.Error(t, err)
}
```

- [ ] **Step 2: 实现**

使用 `sync.Once` + 校验；导出函数供 ServiceContext 在身份（ServiceName + ServiceInstanceID）就绪后调用。  
同时在文档注释写明：Prometheus scrape 应保留这两个 label（示例 07 `prometheus.yml` 用 `static_configs.labels` 与进程自暴露一致）。

- [ ] **Step 3: 在 ServiceContext 启动路径调用（MachineID/InstanceID 已确定之后）**

找到现有设置 `ServiceInstanceID` 的位置（AutoMachineID claim 之后），追加：

```go
_ = observability.RegisterProcessLabels(own.Service.Name, own.ServiceInstanceID)
```

- [ ] **Step 4: 测试 + commit**

```bash
GOCACHE=/private/tmp/core-codex-gocache go test ./pkg/server/observability ./pkg/server/router -count=1 -run ProcessLabels
git commit -am "feat(observability): register process service labels"
```

---

### Task 5: CallService 调用边埋点

**Files:**
- Modify: `pkg/server/router/servicecontext.go`（`CallService` / `invokePayload` / `sendPayload` / `dispatchLocal`）
- Create: `pkg/server/router/servicecontext_call_metrics_test.go`

- [ ] **Step 1: 写失败集成测试**

使用现有 router 测试辅助启动两个 ServiceContext（或 mock Transport），发起 `CallService`：

```go
func TestCallServiceRecordsEdgeMetrics(t *testing.T) {
	observability.ResetMetricsForTest(t)
	// 准备 source sc + local target 路由 /api/demo/ping
	_, err := source.CallService(&types.PayLoad{
		TargetService: "demo-b",
		TargetPath:    "/api/demo/ping",
	})
	require.NoError(t, err)
	total := observability.TestGatherCallTotal(t, "demo-a", "demo-b", "/api/demo/ping", "local", "success")
	require.Equal(t, 1.0, total)
}

func TestCallServiceRecordsUnavailable(t *testing.T) {
	observability.ResetMetricsForTest(t)
	_, err := source.CallService(&types.PayLoad{
		TargetService: "missing",
		TargetPath:    "/api/x/y",
	})
	require.Error(t, err)
	total := observability.TestGatherCallTotal(t, "demo-a", "missing", "/api/x/y", "grpc", "unavailable")
	require.Equal(t, 1.0, total)
}
```

协议判定：

- `dispatchLocal` → `protocol=local`
- gRPC transport → `grpc`
- HTTP transport → `http`

- [ ] **Step 2: RED**

```bash
GOCACHE=/private/tmp/core-codex-gocache go test ./pkg/server/router -run CallServiceRecords -count=1
```

- [ ] **Step 3: 在 `invokePayload` 统一 defer 记录**

```go
start := time.Now()
protocol := "grpc"
// 在 local 分支设 protocol=local；sendPayload 根据 selector 实际协议覆盖
var callErr error
defer func() {
	result := observability.ResultSuccess
	if callErr != nil {
		result = observability.ClassifyError(callErr)
	}
	src := ""
	if own != nil && own.Service != nil {
		src = own.Service.Name
	}
	observability.RecordCall(observability.CallLabels{
		SourceService: src,
		TargetService: payload.TargetService,
		TargetRoute:   payload.TargetPath,
		Protocol:      protocol,
		ResultClass:   result,
	}, time.Since(start))
}()
```

**禁止**因观测改变错误返回值、重试次数或 fallback 顺序。

- [ ] **Step 4: GREEN + race**

```bash
GOCACHE=/private/tmp/core-codex-gocache go test ./pkg/server/router -run CallServiceRecords -count=1
GOCACHE=/private/tmp/core-codex-gocache go test -race ./pkg/server/router -run CallServiceRecords -count=1
```

- [ ] **Step 5: Commit**

```bash
git commit -am "feat(router): record core service call edge metrics"
```

---

### Task 6: gRPC Server 拦截器 + Core 入站路由指标

**Files:**
- Create: `pkg/server/transport/grpc/server_metrics.go`
- Create: `pkg/server/transport/grpc/server_metrics_test.go`
- Modify: `pkg/server/transport/grpc/server.go`（`NewServer` 挂链）
- Modify: `pkg/server/router/servicecontext_grpc_server_test.go` 或新建

- [ ] **Step 1: 测试拦截器顺序与标签**

```go
func TestGRPCServerRecordsCoreRouteAfterAuth(t *testing.T) {
	observability.ResetMetricsForTest(t)
	// NewServer with handler that succeeds for registered route
	// Dial CoreTransport.Call with TargetService/TargetPath
	// Assert core_service_request_requests_total{route=...}=1
}

func TestGRPCServerInvalidRouteDoesNotPolluteLabels(t *testing.T) {
	observability.ResetMetricsForTest(t)
	// Call unknown path
	// Assert route label is invalid_route or rejected class without raw payload
}
```

- [ ] **Step 2: 实现**

`NewServer` 增加 unary chain（顺序固定）：

1. go-zero `serverinterceptors.UnaryPrometheusInterceptor`（method 级，保留）
2. go-zero Duration/Trace 等价物（`go.opentelemetry.io` 已有则提取；无则接 `otelgrpc` 或项目现有 trace 提取）
3. **不在 interceptor 里读未校验 payload 当标签**

Core 路由维度在 **handler 入口**（`Server.Call` 解出 payload 且 ServiceContext 校验路由之后）调用：

```go
observability.RecordInboundRequest(serviceName, targetPath, "grpc", result, duration)
```

身份失败：`result_class=rejected`，`route=invalid_route`。

参考装配：

```go
options = append(options,
	grpc.ChainUnaryInterceptor(
		serverinterceptors.UnaryPrometheusInterceptor,
		// duration/trace if available
	),
)
```

- [ ] **Step 3: GREEN**

```bash
GOCACHE=/private/tmp/core-codex-gocache go test ./pkg/server/transport/grpc -count=1
GOCACHE=/private/tmp/core-codex-gocache go test -race ./pkg/server/transport/grpc -count=1
```

- [ ] **Step 4: Commit**

```bash
git commit -am "feat(grpc): add server prometheus interceptors and core route metrics"
```

---

### Task 7: RuntimeMetricProvider + Collector（Pending/Outbox/EventBridge MVP）

**Files:**
- Create: `pkg/server/observability/provider.go`
- Create: `pkg/server/observability/collector.go`
- Create: `pkg/server/observability/collector_test.go`
- Modify: ReliableWrite / Outbox / EventBridge 挂接（优先 adapter，不改业务语义）

- [ ] **Step 1: 接口与测试**

```go
type RuntimeComponentSnapshot struct {
	Component string
	State     string // ok|not_collected|unavailable
	Gauges    map[string]float64
	Counters  map[string]float64
}

type RuntimeMetricProvider interface {
	ComponentName() string
	RuntimeMetricSnapshot(ctx context.Context) RuntimeComponentSnapshot
}

func TestCollectorExportsProviderGauges(t *testing.T) {
	reg := prometheus.NewRegistry()
	p := &fakeProvider{name: "pending", gauges: map[string]float64{"depth": 3}}
	c := observability.NewComponentCollector("shop-order", []observability.RuntimeMetricProvider{p})
	reg.MustRegister(c)
	// gather core_component_gauge{component="pending",name="depth"} == 3
}
```

- [ ] **Step 2: 实现 Collector**

- 每次 `Collect` 调 `RuntimeMetricSnapshot`；Provider 缺失则不注册该 component。
- Gauge 名白名单：`depth`、`disk_bytes`、`sync_fail_total`、`oldest_age_sec`、`publish_fail_total`、`lag` 等（闭集）。
- 禁止把 SQL、payload、subject 原文当 label。

- [ ] **Step 3: Pending adapter**

基于 `nosql.ReliableWriteMetrics`：

```go
type ReliableWriteProvider struct {
	Snapshot func() nosql.ReliableWriteMetrics
}

func (p ReliableWriteProvider) ComponentName() string { return "pending" }
func (p ReliableWriteProvider) RuntimeMetricSnapshot(context.Context) RuntimeComponentSnapshot {
	m := p.Snapshot()
	return RuntimeComponentSnapshot{
		Component: "pending",
		State:     "ok",
		Gauges: map[string]float64{
			"depth":            float64(m.Pending),
			"disk_bytes":       float64(m.BadgerLSMBytes + m.BadgerVLogBytes),
			// 映射 Admission/Sync 中已有字段；没有则省略
		},
	}
}
```

Outbox/EventBridge 若暂无统一 Metrics 结构：先提供可测试的 adapter 接口 + 07 能填的最小字段；否则组件状态 `not_collected`。

- [ ] **Step 4: ServiceContext 注册 collector（进程内）**

- [ ] **Step 5: Commit**

```bash
git commit -am "feat(observability): export component metrics via prometheus collector"
```

---

### Task 8: Prometheus 客户端、PromQL 模板与状态机

**Files:**
- Create: `pkg/server/runtime/dto.go`
- Create: `pkg/server/runtime/state.go`
- Create: `pkg/server/runtime/state_test.go`
- Create: `pkg/server/runtime/promql.go`
- Create: `pkg/server/runtime/promql_test.go`
- Create: `pkg/server/runtime/promclient.go`
- Create: `pkg/server/runtime/promclient_test.go`
- Create: `pkg/server/runtime/cache.go`

- [ ] **Step 1: 状态测试**

```go
func TestMapStateModeOff(t *testing.T) {
	require.Equal(t, runtime.StateNotCollected, runtime.MapQueryState(runtime.QueryInput{Mode: "off"}))
}

func TestMapStatePrometheusTimeout(t *testing.T) {
	require.Equal(t, runtime.StateUnavailable, runtime.MapQueryState(runtime.QueryInput{
		Mode: "prometheus", Err: context.DeadlineExceeded,
	}))
}

func TestStaleThreshold(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	last := now.Add(-40 * time.Second)
	require.Equal(t, runtime.StateStale, runtime.Freshness("15s", now, &last))
}
```

- [ ] **Step 2: PromQL 模板测试（禁止字符串拼接用户输入）**

```go
func TestPromQLServiceRateUsesAllowlistedWindow(t *testing.T) {
	q, err := runtime.ServiceRequestRateQuery("shop-order", "15s")
	require.NoError(t, err)
	require.Contains(t, q, `service="shop-order"`)
	require.Contains(t, q, `[15s]`)
}

func TestPromQLRejectsUnknownWindow(t *testing.T) {
	_, err := runtime.ServiceRequestRateQuery("shop-order", "7d")
	require.Error(t, err)
}

func TestPromQLRejectsUnsafeServiceName(t *testing.T) {
	_, err := runtime.ServiceRequestRateQuery(`shop-order",on`, "15s")
	require.Error(t, err)
}
```

窗口映射：

```go
var allowedWindows = map[string]time.Duration{
	"15s": 15 * time.Second,
	"5m":  5 * time.Minute,
	"1h":  time.Hour,
}
```

服务名校验：与 `NormalizeServiceLabel` 一致且拒绝 `"`、`{`、`}`、`,`、空格。

示例查询（实现时以 go-zero 真实指标为准）：

```text
# HTTP 入站 QPS by service (scrape label)
sum(rate(http_server_requests_code_total{service="shop-order"}[15s]))

# 调用边
sum(rate(core_service_call_requests_total{source_service="shop-user",target_service="shop-order"}[15s])) by (source_service,target_service,protocol)

# p95
histogram_quantile(0.95, sum(rate(core_service_call_duration_ms_bucket{target_service="shop-order"}[15s])) by (le,target_route))
```

- [ ] **Step 3: promclient**

```go
type PromClient interface {
	Query(ctx context.Context, query string, ts time.Time) (Vector, error)
	QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) (Matrix, error)
}
```

- 使用 `net/http` + 超时来自配置。
- 错误包装为 `ErrPrometheusUnavailable`，**日志不含 QueryURL 完整凭据**（可 log host only 或 redacted）。
- httptest 测试覆盖 200/5xx/超时。

- [ ] **Step 4: 短缓存**

```go
type ResponseCache struct {
	ttl time.Duration
	// key = window + "|" + kind + "|" + service
}
```

- [ ] **Step 5: Commit**

```bash
git commit -am "feat(runtime): add promql templates, prom client, and metric state mapping"
```

---

### Task 9: Runtime Aggregator

**Files:**
- Create: `pkg/server/runtime/aggregator.go`
- Create: `pkg/server/runtime/aggregator_test.go`

- [ ] **Step 1: 表驱动测试**

```go
func TestAggregatorTopologyMergesClusterAndMetrics(t *testing.T) {
	cluster := fakeCluster{
		nodes: []Node{
			{Service: "shop-user", Status: "running"},
			{Service: "shop-order", Status: "running", Instance: "a"},
			{Service: "shop-order", Status: "running", Instance: "b"},
		},
	}
	prom := fakeProm{vectors: map[string]float64{
		"rate:shop-user":  10,
		"rate:shop-order": 20,
		"edge:shop-user>shop-order": 8,
	}}
	agg := runtime.NewAggregator(cluster, prom, runtime.Config{Mode: "prometheus"})
	resp, err := agg.Topology(context.Background(), "15s")
	require.NoError(t, err)
	require.Len(t, resp.Services, 2)
	order := findService(resp, "shop-order")
	require.Equal(t, 2, order.RunningInstances)
	require.Equal(t, runtime.StateOK, order.State)
}

func TestAggregatorModeOffReturnsNotCollectedMetrics(t *testing.T) {
	agg := runtime.NewAggregator(fakeCluster{nodes: []Node{{Service: "shop-user", Status: "running"}}}, nil, runtime.Config{Mode: "off"})
	resp, err := agg.Topology(context.Background(), "15s")
	require.NoError(t, err)
	require.Equal(t, runtime.StateNotCollected, resp.Services[0].RequestRate.State)
	require.Nil(t, resp.Services[0].RequestRate.Value)
}

func TestAggregatorPrometheusDownKeepsTopology(t *testing.T) {
	agg := runtime.NewAggregator(fakeCluster{...}, failingProm{}, runtime.Config{Mode: "prometheus"})
	resp, err := agg.Topology(context.Background(), "15s")
	require.NoError(t, err)
	require.Equal(t, runtime.StateUnavailable, resp.Status)
	require.NotEmpty(t, resp.Services)
	require.Nil(t, resp.Services[0].RequestRate.Value)
}

func TestAggregatorAsyncEdgesFromSubscriptions(t *testing.T) {
	// publish metrics source_service=shop-order subject_family=order
	// subscription meta target=shop-user, shop-supplier
	// expect two async edges
}
```

- [ ] **Step 2: 实现合并规则**

1. `ClusterProvider.List` 全服务/实例 → 节点。
2. 同步边：来自 `core_service_call_*` 聚合（source/target）。
3. 异步边：`publish` 指标 ⋈ 订阅注册表（`SubscribeRouters` / EventBridge 订阅元数据导出的只读索引）。
4. 单实例失败不删除服务节点；`RunningInstances` 来自集群状态。
5. 百分位仅 Histogram；样本不足 → `value=null`。

订阅索引接口（最小）：

```go
type SubscriptionIndex interface {
	// List returns stable (source_subject_family, event_type, target_service)
	List(ctx context.Context) ([]SubscriptionEdge, error)
}
```

第一版可从本进程已注册订阅 + 配置/服务发现的静态视图构建；若跨进程拿不到全部订阅，async edge 允许 `partial` + warning，**禁止猜测消费者**。

- [ ] **Step 3: GREEN + commit**

```bash
GOCACHE=/private/tmp/core-codex-gocache go test ./pkg/server/runtime -count=1
git commit -am "feat(runtime): aggregate cluster topology with prometheus metrics"
```

---

### Task 10: ServerManage Runtime API

**Files:**
- Create: `pkg/server/api/public/runtimetopology.go`
- Create: `pkg/server/api/public/runtimetopology_test.go`
- Create: `pkg/server/api/public/runtimeservice.go`
- Create: `pkg/server/api/public/runtimeservice_test.go`
- Modify: `pkg/server/api/release/routes.go`
- Modify: `pkg/server/api/release/routes_test.go`（若有注册闭集）

- [ ] **Step 1: 路由与鉴权测试**

```go
func TestRuntimeTopologyRouterInfoIsServerManage(t *testing.T) {
	info := (&public.RuntimeTopology{}).RouterInfo()
	require.Equal(t, "/api/servermanage/runtimetopology", info.GetPath())
	require.Equal(t, types.ServerManagerType, info.GetPathType()) // 使用实际枚举名
}

func TestRuntimeTopologyRejectsBadWindow(t *testing.T) {
	h := &public.RuntimeTopology{Window: "7d"}
	err := h.Validation(fakeReq())
	require.Error(t, err)
}

func TestRuntimeServiceRejectsUnknownService(t *testing.T) {
	h := &public.RuntimeService{Window: "15s", Service: "no-such"}
	// Validation or Do must not interpolate into promql; return safe error
}

func TestStatisticsStillUnregistered(t *testing.T) {
	// release routes 不得包含 Statistics
}
```

- [ ] **Step 2: Handler 实现**

```go
type RuntimeTopology struct {
	api.ServerArgs
	Window string `json:"window"`
}

func (own *RuntimeTopology) Parse(req types.IRequest) error {
	// 绑定 window；沿用项目 JSON 绑定方式
	return nil
}

func (own *RuntimeTopology) Validation(req types.IRequest) error {
	if err := own.ServerArgs.Validation(req); err != nil {
		return err
	}
	if _, ok := runtime.ParseWindow(own.Window); !ok {
		return errors.New("window must be one of: 15s, 5m, 1h")
	}
	return nil
}

func (own *RuntimeTopology) Do(req types.IRequest) (interface{}, error) {
	agg := runtime.AggregatorFromRequest(req) // 从 ServiceContext 取
	if agg == nil {
		return &runtime.TopologyResponse{
			GeneratedAt: time.Now().UTC(),
			Window:      own.Window,
			Status:      runtime.StateNotCollected,
			Services:    []runtime.ServiceNode{},
			Edges:       []runtime.ServiceEdge{},
			Warnings: []runtime.RuntimeWarning{{
				Code: "aggregator_unavailable", Message: "runtime aggregator is not configured",
			}},
		}, nil
	}
	return agg.Topology(req.GetContext(), own.Window)
}

func (own *RuntimeTopology) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfoWithOptions(own, withSystemEndpointRateLimit())
}
```

`RuntimeService` 同理，body 含 `service`；`Do` 内：

```go
svc := observability.NormalizeServiceLabel(own.Service)
if !agg.KnownService(svc) {
	return nil, errors.New("unknown service")
}
return agg.ServiceDetail(ctx, own.Window, svc)
```

- [ ] **Step 3: 注册**

`release/routes.go` 追加 `&public.RuntimeTopology{}`, `&public.RuntimeService{}`。  
**保持** `Statistics` 注释不注册。

- [ ] **Step 4: 把 Aggregator 挂到 ServiceContext**

仅当 `RuntimeObservability.Mode=prometheus` 或需要拓扑（Mode=off 也可提供无指标拓扑）时构造。推荐：**始终可建 Aggregator**；Mode 只影响指标查询。

- [ ] **Step 5: Commit**

```bash
git commit -am "feat(api): add servermanage runtime topology and service endpoints"
```

---

### Task 11: 示例 07 Prometheus 与 scrape

**Files:**
- Modify: `examples/07-shop-order-scale/deploy/docker-compose.yml`
- Create: `examples/07-shop-order-scale/deploy/prometheus.yml`
- Modify: `examples/07-shop-order-scale/bootstrap/config.go`（暴露 Prometheus agent + 可选 QueryURL）
- Modify: `examples/07-shop-order-scale/deploy/README.md`

- [ ] **Step 1: compose 增加 prometheus 服务**

```yaml
  prometheus:
    image: prom/prometheus:v2.54.1
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
    ports: ["19090:9090"]
    depends_on: [shop-user, shop-supplier, shop-order-a, shop-order-b]
```

- [ ] **Step 2: prometheus.yml 静态 target + labels**

```yaml
scrape_configs:
  - job_name: shop
    scrape_interval: 5s
    static_configs:
      - targets: ["shop-user:9101"]
        labels: {service: shop-user, service_instance_id: shop-user-1}
      - targets: ["shop-order-a:9101"]
        labels: {service: shop-order, service_instance_id: shop-order-a}
      - targets: ["shop-order-b:9101"]
        labels: {service: shop-order, service_instance_id: shop-order-b}
      - targets: ["shop-supplier:9101"]
        labels: {service: shop-supplier, service_instance_id: shop-supplier-1}
```

各服务启用 go-zero `Prometheus.Host/Port`（RestConf 内嵌字段）。管理入口（shop-user）配置：

```json
"RuntimeObservability": {
  "Mode": "prometheus",
  "QueryURL": "http://prometheus:9090"
}
```

- [ ] **Step 3: 静态校验测试**

扩展 `examples/integration/07-shop-order-scale-multi-process/docker_compose_test.go`：

```go
func TestComposeDefinesPrometheusScrape(t *testing.T) {
	// 读 prometheus.yml / compose，断言 shop-order 两个 target 同 service 不同 instance id
}
```

- [ ] **Step 4: Commit**

```bash
git commit -am "feat(examples/07): add prometheus scrape for runtime graph"
```

---

### Task 12: 示例 07 Runtime UAT

**Files:**
- Create: `examples/integration/07-shop-order-scale-multi-process/runtime_graph_uat_test.go`

- [ ] **Step 1: 写 UAT（显式 env 启用）**

```go
func TestRuntimeGraphUAT(t *testing.T) {
	if os.Getenv("SHOP_RUN_RUNTIME_UAT") != "1" {
		t.Skip("set SHOP_RUN_RUNTIME_UAT=1")
	}
	// 1. 确保 compose up + 产生下单流量
	// 2. ServerManage token
	// 3. POST runtimetopology window=15s
	// 4. assert services include shop-user, shop-order, shop-supplier
	// 5. assert shop-order running_instances >= 1
	// 6. assert sync edge shop-user -> shop-order
	// 7. POST runtimeservice service=shop-order
	// 8. assert routes/components states are not fake zeros when unavailable
}
```

故障场景（可分测试）：

1. stop one order → instances 反映降级  
2. stop prometheus → topology 仍在，metrics `unavailable`  
3. 未注册 collector 组件 → `not_collected`

- [ ] **Step 2: 本地可跑通后提交**

```bash
# 普通 CI 不强制外部依赖
GOCACHE=/private/tmp/core-codex-gocache go test ./examples/integration/07-shop-order-scale-multi-process -run TestComposeDefinesPrometheusScrape -count=1
```

- [ ] **Step 3: Commit**

```bash
git commit -am "test(07): add runtime graph UAT hooks and compose assertions"
```

---

### Task 13: Web Admin 数据层切换

**Files:**
- Create: `web/admin/src/pages/MonitorSystem/types.ts`
- Create: `web/admin/src/services/runtime.ts`
- Modify: `web/admin/src/components/WayPlus/request.ts`（新增 runtime API，保留旧函数但标记废弃）
- Modify: `web/admin/mock/monitorsystem.mock.ts`

- [ ] **Step 1: 类型与请求**

```ts
export type MetricState = 'ok' | 'partial' | 'stale' | 'unavailable' | 'not_collected';

export interface MetricValue {
  value: number | null;
  state: MetricState;
  last_sample?: string;
  coverage_sec?: number;
}

export async function fetchRuntimeTopology(window: '15s' | '5m' | '1h') {
  return request('/api/servermanage/runtimetopology', {
    method: 'POST',
    data: { window },
  });
}

export async function fetchRuntimeService(service: string, window: '15s' | '5m' | '1h') {
  return request('/api/servermanage/runtimeservice', {
    method: 'POST',
    data: { window, service },
  });
}
```

- [ ] **Step 2: 单元测试（Jest）**

- mock 返回 `value: null, state: 'unavailable'` 时 UI 格式化不为 `0`
- window 切换保留

- [ ] **Step 3: Commit**

```bash
git commit -am "feat(admin): add runtime graph API client and types"
```

---

### Task 14: Web Admin 全局图 + 服务请求视图

**Files:**
- Modify: `web/admin/src/pages/MonitorSystem/index.tsx`
- Create: `web/admin/src/pages/MonitorSystem/Graph.tsx`
- Create: `web/admin/src/pages/MonitorSystem/ServiceView.tsx`
- Create: `web/admin/src/pages/MonitorSystem/StateBadge.tsx`
- 测试：`web/admin/src/pages/MonitorSystem/*.test.tsx`（若项目 Jest 已配置页面测）

- [ ] **Step 1: StateBadge**

文字 + 图标表达 `partial/stale/unavailable/not_collected`，**不能只靠颜色**。

- [ ] **Step 2: Graph**

- 节点：逻辑服务，多副本显示 `service × N`
- 边：sync 实线 / async 虚线；线宽~rate；颜色~error/p95 状态
- 点击节点 → ServiceView
- 顶部：健康服务数、运行实例、总 QPS、全局 p95、window 选择
- 轮询 15s；`document.visibilityState=hidden` 时暂停

- [ ] **Step 3: ServiceView**

- 路由表：QPS、成功率、p50/p95/p99，可排序
- 实例分布
- 右侧组件：pending/outbox/eventbridge
- 返回全局图时保留 window

- [ ] **Step 4: 删除/停用对旧 `getServiceStats` 的默认依赖**

旧 mock 仅用于组件故事或删除；生产路径只走 runtime API。

- [ ] **Step 5: 前端检查**

```bash
cd web/admin && yarn tsc --noEmit
cd web/admin && yarn test --watchAll=false --testPathPattern=MonitorSystem
```

- [ ] **Step 6: Commit**

```bash
git commit -am "feat(admin): render service runtime graph and request view"
```

---

### Task 15: 旧 RouterStats 废弃登记（不删除）

**Files:**
- Modify: `docs/codex/DEPRECATION_REGISTER.md`
- Modify: `docs/codex/API_COMPATIBILITY_SURFACE.md`
- Modify: `CHANGELOG.md`
- 可选：`pkg/server/types/routerstats.go` 注释 `Deprecated:`

- [ ] **Step 1: 登记内容**

| 符号 | 替代 | 最早删除版本 | 消费方 |
| --- | --- | --- | --- |
| `types.RouterStats` | Runtime API + Prometheus | 待批准破坏性版本 | futures / 内源扫描 |
| `ServiceContext.GetAllRouterStats` | `POST /api/servermanage/runtimeservice` | 同上 | |
| `public.Statistics`（未注册） | `RuntimeTopology`/`RuntimeService` | 同上 | |

明确：**无 fallback**；Statistics 保持不注册。

- [ ] **Step 2: 扫描消费方**

```bash
rg -n "GetAllRouterStats|RouterStatsSnapshot|getServiceStats|/statistics" --glob '!web/admin/node_modules/**' --glob '!pkg/server/run/dist/**'
```

结果写入兼容矩阵。

- [ ] **Step 3: 门禁**

```bash
./scripts/test.sh api-compat
./scripts/test.sh public-api
./scripts/test.sh release-contract
```

- [ ] **Step 4: Commit**

```bash
git commit -am "docs: deprecate RouterStats in favor of runtime observability"
```

---

### Task 16: 全量验收清单（完成标准）

- [ ] **Step 1: 单元/竞态**

```bash
GOCACHE=/private/tmp/core-codex-gocache go test ./pkg/server/observability ./pkg/server/runtime ./pkg/server/transport/grpc ./pkg/server/router ./pkg/server/api/public ./pkg/server/config -count=1
GOCACHE=/private/tmp/core-codex-gocache go test -race ./pkg/server/observability ./pkg/server/runtime ./pkg/server/transport/grpc ./pkg/server/router -count=1
```

- [ ] **Step 2: 契约**

```bash
./scripts/test.sh config-contract
./scripts/test.sh api-compat
./scripts/test.sh public-api
./scripts/test.sh release-contract
```

- [ ] **Step 3: 前端**

```bash
cd web/admin && yarn tsc --noEmit && yarn test --watchAll=false --testPathPattern=MonitorSystem
```

- [ ] **Step 4: 07 真实 UAT（手工/夜间）**

```bash
SHOP_RUN_RUNTIME_UAT=1 go test ./examples/integration/07-shop-order-scale-multi-process -run RuntimeGraph -count=1 -timeout 30m
```

核对规格 §16 七条完成标准。

- [ ] **Step 5: 最终说明提交（若有遗留文档）**

```bash
git commit -am "docs: record service runtime graph verification results"
```

---

## 规格覆盖自检

| 规格章节 | 对应任务 |
| --- | --- |
| §3 目标 / §4 非目标 | 全文决策 D1–D10；Statistics 不恢复 |
| §6 架构与权威源 | Task 8–10 |
| §6.2 时间窗口 | Task 8/10 |
| §6.3 配置 | Task 3 |
| §7.1 go-zero 复用 | D1；Task 6 服务端补齐 |
| §7.2 调用边 | Task 2/5 |
| §7.3 gRPC Server | Task 6 |
| §7.4 异步边 | Task 9 |
| §7.5 组件 | Task 7（MVP） |
| §8 Runtime API | Task 10 |
| §9 状态语义 | Task 8 |
| §10 Web Admin | Task 13–14 |
| §11 示例 07 | Task 11–12 |
| §12 废弃 | Task 15（删除另里程碑） |
| §13 安全容量 | PromQL 白名单、缓存、限流、标签闭集 |
| §14 测试门禁 | Task 16 |
| §15 实施顺序 | Task 1→16 |
| §16 完成标准 | Task 16 |

## 占位符扫描

- 无 TBD/TODO 实现步骤。
- 破坏性删除 RouterStats 明确排除在本计划外。
- MySQL/WebSocket/Cache 组件允许 `not_collected`，不假装完成。

## 类型一致性

- API 路径：`/api/servermanage/runtimetopology`、`/api/servermanage/runtimeservice`
- 状态枚举：`ok|partial|stale|unavailable|not_collected`
- 窗口：`15s|5m|1h`
- 指标前缀：`core_service_call_*`、`core_service_request_*`
- 配置：`ServerConfig.RuntimeObservability`

---

## 执行说明

本仓库禁止子智能体。执行时请使用：

1. **Inline Execution（本会话串行）** — `superpowers:executing-plans`，按 Task 1→16 推进，每任务提交。  
2. **人工分段** — 每次只做观测面 / API 面 / UI 面中的一段，合并前跑 Task 16 对应子集。

**不要**启动 subagent-driven-development。

计划路径：`docs/superpowers/plans/2026-07-27-service-runtime-graph.md`
