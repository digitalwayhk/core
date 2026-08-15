# 多服务运行与请求聚合监控设计

## 1. 文档状态

- 状态：已批准，进入实施计划
- 日期：2026-07-27
- 实施计划：`docs/superpowers/plans/2026-07-27-service-runtime-graph.md`
- 前端入口：Core Web Admin
- 验收案例：`examples/07-shop-order-scale`
- 指标基础：go-zero Prometheus、OpenTelemetry 与 zrpc 中间件

## 2. 背景

Web Admin 已有 MonitorSystem 页面和旧 `RouterStats` 类型，但当前统计链路不具备可用的真实闭环：

- `RouterStats` 通过常量关闭，运行时不会产生统计；
- Statistics Handler 的核心实现已被注释，路由也未注册；
- 前端仍请求 `/api/servermanage/statistics/{service}`；
- 旧结构只有 QPS、错误总数和最小/最大/平均耗时，没有可合并的 Histogram，不能可靠计算 p50、p95、p99；
- 旧结构只描述单服务路由，不记录跨服务调用边、目标实例或异步关系；
- Core 的 gRPC Client 已复用 zrpc 指标中间件，但自建 grpc-go Server 尚未接入 Prometheus、Duration 和 Trace 拦截器。

go-zero `v1.10.2` 已提供 HTTP、zrpc、Prometheus 和 OpenTelemetry 基础能力。本设计不恢复旧统计系统，而是在 go-zero 观测能力之上补齐 Core 特有的服务、路由、实例和内部组件语义。

## 3. 目标

1. 在 Web Admin 提供多服务运行，先展示全局服务关系，再进入单个服务查看请求聚合。
2. 全局图只展示逻辑服务；MySQL、Pending、Outbox、EventBridge 等组件进入服务内部视图。
3. 第一版只做聚合指标，不提供单次 Trace 查询或请求回放。
4. 复用 go-zero 的 HTTP、zrpc、Prometheus Histogram 和 OpenTelemetry 能力，不维护第二套通用路由统计。
5. 补齐 Core gRPC 服务端、跨服务调用边和可靠写组件的低基数指标。
6. 以示例 07 验证多副本、共享权威库、Pending、Outbox 和异步投影的运行状态。
7. 缺失、过期和不可达必须明确表达，不得把未采集数据伪装成零。
8. 完成新链路替代后，按兼容性流程废弃并移除旧 `RouterStats`。

## 4. 非目标

- 不在第一版提供 TraceID 搜索、单请求 Span 瀑布图或请求正文查看。
- 不在浏览器中直接访问 Prometheus、其他服务实例或内部 `/metrics`。
- 不把用户 ID、订单 ID、TraceID、原始 URL、Query、请求正文或 SQL 作为指标标签。
- 不让运行图成为业务健康检查或自动扩缩容的唯一权威。
- 不将 MySQL、Redis、MQ 等基础设施节点放到全局服务画布。
- 不恢复未注册的旧 Statistics HTTP 路由。
- 不在新旧两套统计实现之间建立长期双写。

## 5. 已确认的产品决策

| 项目 | 决策 |
| --- | --- |
| 主界面 | 混合运维台 |
| 第一层 | 全局服务运行 |
| 第二层 | 点击服务后按请求路由查看运行状态 |
| 默认视角 | 请求优先，不是实例优先 |
| 数据粒度 | 聚合指标，不做单次 Trace |
| 全局节点 | 只显示逻辑服务 |
| 多副本 | 全局合并为一个服务节点，服务内部再展示实例分布 |
| 同步关系 | 实线 |
| 异步关系 | 虚线 |
| 指标来源 | go-zero 通用指标 + Core 低基数自定义指标 |
| 旧统计 | 替代完成后废弃并移除 |

## 6. 总体架构

```text
业务请求
   |
   +--> go-zero REST middleware -------------------+
   |                                               |
   +--> Core ServiceResolver -> zrpc Client -------+--> Prometheus
   |              |                                |       |
   |              +--> Core call-edge metrics -----+       |
   |                                                       |
   +--> Core grpc-go Server interceptors -------------------+
   |                                                       |
   +--> Pending / MySQL / Outbox / EventBridge providers ---+
                                                           |
ClusterProvider ------------------------------------+      |
                                                   |      |
                                                   v      v
                                          Runtime Aggregator
                                                   |
                                  ServerManage Runtime API
                                                   |
                                             Web Admin
```

### 6.1 数据权威

- ClusterProvider 是服务、实例、状态和地址的权威来源。
- Prometheus 是请求率、错误率、Histogram 和时间窗口的指标查询来源。
- RouterInfo 是稳定路由模板、方法和认证域的元数据来源。
- Core 组件自己的 Metrics/Snapshot 是 Pending、Outbox、EventBridge 等领域运行指标的来源，并通过 Prometheus Collector 暴露。
- Runtime Aggregator 只负责查询、合并和返回 Admin DTO，不成为新的指标存储。

部署与数据流钉死：

- Runtime Aggregator 只部署在 ServerManage 可达边界；业务副本负责暴露 scrape 指标，不在请求路径被 Aggregator 直连。
- `RuntimeMetricProvider` 仅在本进程注册 Collector 并更新指标；查询一律走 Prometheus，禁止 Aggregator 在 API 请求中远程拉取各实例 Provider。
- 进程启动后必须提供稳定 scrape 标签 `service` 与 `service_instance_id`（应用侧注册优先，示例 07 scrape 配置兜底）。

当 Prometheus 未配置或不可达时，运行图仍可根据 ClusterProvider 展示服务和实例，但所有指标必须返回 `not_collected` 或 `unavailable`，不得回退到旧 `RouterStats`。

状态映射：`Mode=off` 或缺查询配置 → `not_collected`；`Mode=prometheus` 且查询失败/超时 → `unavailable`；有样本但超过新鲜度阈值 → `stale`；部分实例/组件/保留不足 → `partial`。

### 6.2 时间窗口

第一版支持：

- 实时：最近 15 秒；
- 短窗口：最近 5 分钟；
- 诊断窗口：最近 1 小时。

Runtime Aggregator 使用 Prometheus Instant Query 和 `query_range` 获取窗口数据。不存在内存历史兼容回退；Prometheus 保留不足时返回实际覆盖范围和 `partial` 状态。

### 6.3 配置边界

服务实例继续使用 go-zero 上游 `Prometheus` 配置暴露指标。Runtime Aggregator 只增加 Core 自有的查询端配置，概念结构为：

```json
{
  "RuntimeObservability": {
    "Mode": "prometheus",
    "QueryURL": "http://prometheus:9090",
    "QueryTimeout": "3s",
    "MaxConcurrentQueries": 4,
    "CacheTTL": "5s"
  }
}
```

- `Mode` 只接受 `off` 或 `prometheus`；
- 非法模式、URL、时间和并发值在配置校验阶段失败；
- Prometheus 暂时不可达不阻止业务服务启动，Runtime 页面进入 `unavailable`；
- QueryURL 和可能包含的认证信息不得进入 bootstrap、AdminView、日志或 Runtime API；
- Prometheus scrape 配置必须为目标附加稳定的 `service` 和 `service_instance_id` 标签；
- 示例 07 的部署文件负责提供可复现的 scrape target，不依赖人工在 Prometheus UI 中配置。

新增字段必须进入配置能力矩阵和 config-contract，不能以环境变量旁路正式配置契约。

## 7. 指标设计

### 7.1 复用 go-zero 指标

HTTP 入站继续使用 go-zero REST 中间件提供的请求计数和耗时 Histogram（`http_server_requests_*`，标签含注册时的 `path` 模板、`method`、`code`，单位 ms）。Aggregator 用 `path` 对齐 `RouterInfo.GetPath()`。zrpc Client 继续启用：

- Trace；
- Duration；
- Prometheus；
- Breaker；
- Timeout。

不复制 go-zero 已提供的通用 HTTP/RPC 指标。新增 Core 指标只补充 go-zero 默认 method 标签无法表达的业务调用维度：跨服务调用边，以及 gRPC 入站在 `/CoreTransport/Call` 之上的稳定 `route` 维度。

### 7.2 Core 跨服务调用边

在 ServiceResolver/TransportSelector 已经确定目标服务、目标路由、协议和实例后记录调用结果。建议指标：

```text
core_service_call_requests_total{
  source_service,
  target_service,
  target_route,
  protocol,
  result_class
}

core_service_call_duration_ms_bucket{
  source_service,
  target_service,
  target_route,
  protocol
}
```

约束：

- `target_route` 必须来自冻结后的路由模板，不使用原始请求 URL；
- `result_class` 只使用 `success`、`client_error`、`server_error`、`timeout`、`unavailable` 等稳定枚举；
- 不把 endpoint 完整地址作为标签；
- 实例分布优先使用 Prometheus scrape target 的 `instance` 和稳定 `service_instance_id` 标签；
- 写请求的重试、fallback 和 breaker 结果单独计数，但不能因观测改变现有发送语义。

### 7.3 Core gRPC 服务端

当前自建 grpc-go Server 必须增加：

- go-zero 或等价的 Prometheus Unary Server Interceptor；
- Duration 统计；
- OpenTelemetry Trace 提取与 Span；
- Core Router 维度记录器。

CoreTransport 只有一个 gRPC method，不能只按 `/CoreTransport/Call` 聚合。服务端在可信身份校验后读取 payload 中的 `SourceService`、`TargetService` 和 `TargetPath`，但：

- 指标只接受服务端已经验证或解析出的稳定值；
- 不信任普通调用方自报的服务身份；
- 目标路由必须匹配当前 ServiceContext 已注册 RouterInfo；
- 身份或路由校验失败只记录稳定错误类别，不记录 payload。

### 7.4 异步服务关系

事件发布方不知道所有消费者，不能在发布路径中硬编码目标服务。异步边由 Runtime Aggregator 连接两类事实：

1. EventBridge/MQ 发布指标：`source_service`、规范化 `subject_family`、`event_type`、rate、error。
2. 服务启动时已注册的订阅元数据：`target_service`、`subject_family`、`event_type`、reliable。

只有真实注册的订阅才能形成异步边。一个 Subject 被多个逻辑服务订阅时，生成多条来源相同、目标不同的虚线边。动态 Subject 必须先归一为有界 family；EventID、TraceID、消息 ID 和业务聚合 ID 都不得成为标签。

消费 rate、ack/nack 和 lag 由目标服务的 consumer 指标提供。发布存在但没有订阅时只显示服务内部 warning，不凭猜测创建目标节点。

### 7.5 服务内部组件

通过小型 Collector/Provider 接口接入现有运行指标，不要求业务重新实现存储逻辑：

```go
type RuntimeMetricProvider interface {
	RuntimeMetricSnapshot(context.Context) RuntimeComponentSnapshot
}
```

第一版组件：

- ReliableWrite/Pending：pending 数、接纳拒绝、磁盘字节、同步成功/失败、同步延迟；
- MySQL：连接池使用/等待、操作耗时和错误；不输出 SQL 或参数；
- Outbox：待发布数、发布成功/失败、最老消息年龄；
- EventBridge/MQ：publish/consume、ack/nack、drop、queue lag；
- WebSocket：连接数、队列使用、drop 和错误；
- Cache：hit/miss、状态和降级次数。

Provider 缺失时返回 `not_collected`，不能自动反射或猜测业务对象。

## 8. Runtime API

### 8.1 认证边界

- 浏览器只访问 `ServerManageAuth` 保护的 Runtime API。
- Runtime Aggregator 由 WebServer/ServerManage 边界持有 Prometheus 和 ClusterProvider 访问能力。
- 浏览器不获得 Prometheus 地址、服务实例地址、mTLS 材料或内部 Token。
- API 响应不得包含 payload、SQL、Header、Token、Claims 或业务记录。

### 8.2 全局拓扑

建议入口：

```text
POST /api/servermanage/runtime/topology
```

请求：

```json
{"window":"15s"}
```

响应概念结构：

```json
{
  "generated_at": "2026-07-27T08:00:00Z",
  "window": "15s",
  "status": "ok",
  "services": [],
  "edges": [],
  "warnings": []
}
```

服务节点包含：

- service name；
- registered/running/unavailable instance count；
- request rate；
- error rate；
- p50/p95/p99；
- metric state；
- last sample time。

调用边包含：

- source/target service；
- sync/async；
- protocol；
- request/event rate；
- error rate；
- p95；
- metric state。

### 8.3 服务详情

建议入口：

```text
POST /api/servermanage/runtime/service/{service}
```

返回：

- 服务汇总；
- 路由列表；
- 每条路由的 QPS、成功率、p50/p95/p99；
- 路由到实例的流量和延迟分布；
- Pending、MySQL、Outbox、EventBridge 等内部组件；
- 采集状态和警告。

服务名必须从 ClusterProvider/ServiceContext 的已知集合解析，不能把路径参数直接拼接进 PromQL。

## 9. 状态与错误语义

统一状态：

| 状态 | 含义 |
| --- | --- |
| `ok` | 指标完整且在允许的新鲜度内 |
| `partial` | 部分实例、部分时间范围或部分组件缺失 |
| `stale` | 有历史样本，但超过新鲜度阈值 |
| `unavailable` | 权威数据源当前不可访问 |
| `not_collected` | 当前部署未启用该指标 |

规则：

1. 未采集使用 `null + not_collected`。
2. 实例不可达使用 `null + unavailable`。
3. 历史覆盖不足使用实际 coverage 和 `partial`。
4. 某个实例失败不让整张图失败；其他节点和边继续返回。
5. Prometheus 查询错误返回安全错误类别和 warning，不向浏览器返回 PromQL、地址或凭据。
6. 所有百分位数必须由 Histogram 计算；样本不足时返回 `null`。
7. 数值零只表示权威采集结果确实为零。

## 10. Web Admin 交互

### 10.1 全局服务运行

- 顶部显示健康服务数、运行实例数、总请求率和全局 p95。
- 服务节点显示逻辑服务名、健康实例数、QPS、错误率和 p95。
- 同步调用使用实线，异步事件使用虚线。
- 线宽表达流量，颜色表达错误或延迟状态。
- 多副本服务合并为 `service × N`。
- 点击服务进入服务详情，不在全局画布展开数据库和队列。

### 10.2 服务请求视图

- 默认按请求路由排序；
- 支持按 QPS、错误率、p95 排序；
- 点击路由后展示实例流量分布；
- 右侧展示该服务内部组件状态；
- 顶部保留 15 秒、5 分钟、1 小时时间窗口；
- `partial/stale/unavailable/not_collected` 使用文字和图标共同表达，不能只依赖颜色；
- 支持返回全局图，并保留时间窗口选择。

第一版不展示单次请求列表，避免把聚合指标误导为 Trace 数据。

## 11. 示例 07 验收案例

示例 07 的运行图至少展示：

```text
shop-user  --sync-->  shop-order ×2  --async-->  shop-user projection
                                      `--async-->  supplier projection
```

全局图：

- `shop-user`、`shop-order`、`supplier` 三个逻辑服务；
- `shop-order` 显示两个运行实例；
- 同步下单调用使用实线；
- Order 事件投影使用虚线；
- 任一 order 副本停止后，节点变为部分降级而不是从图中消失。

进入 `shop-order`：

- 展示 AddOrder、GetOrders、CancelOrder 等稳定路由模板；
- 展示两个副本的流量和延迟分布；
- 展示本地可靠 Pending；
- 展示共享 MySQL 汇合状态；
- 展示 Outbox 待发布数与最老年龄；
- 展示 EventBridge 发布速率、错误和 lag。

故障场景：

1. 停止一个 order 副本：健康实例 `1/2`，其他指标仍可查询。
2. 暂停 MySQL：Pending 上升、同步失败增加，API 本地可靠接纳语义保持不变。
3. 暂停 MQ：Outbox/事件 lag 上升，异步边进入降级。
4. 停止 Prometheus：拓扑仍显示注册节点，指标显示 `unavailable`。
5. 未启用某组件 Collector：该组件显示 `not_collected`。

## 12. 旧 RouterStats 迁移与移除

### 12.1 决策

新运行图不复用、不恢复旧 `RouterStats`。旧实现与 go-zero 指标并存会产生不同时间窗口、错误分类和延迟口径，因此只能作为迁移对象，不能成为 fallback。

### 12.2 迁移阶段

阶段一：建立替代。

- 完成 go-zero/Prometheus 数据闭环；
- 完成 Core gRPC 服务端和调用边指标；
- 完成 Runtime API 与 Admin 切换；
- 为仍有外部源码消费价值的旧方法提供临时兼容适配，内部不再读取旧字段。

阶段二：正式废弃。

- 在 `DEPRECATION_REGISTER.md` 登记旧导出类型和方法；
- 更新 `API_COMPATIBILITY_SURFACE.md`；
- 扫描 futures 等登记消费方；
- 不再为旧 Statistics 增加功能。

阶段三：删除。

- 删除 `pkg/server/types/routerstats.go`；
- 删除 `pkg/server/router/statsmanager.go`；
- 删除 `RouterInfo.stats` 和旧请求/缓存/WebSocket 记录钩子；
- 删除 `ServiceContext.GetAllRouterStats` 等旧导出方法；
- 删除未注册的旧 Statistics Handler；
- 删除前端旧请求、mock 和仅服务于旧结构的组件；
- 更新 public API baseline、CHANGELOG 和迁移说明。

删除只能进入批准的破坏性版本，并通过 public-api、api-compat、release-contract 和消费方 smoke；不得在替代实现尚未验证时提前删除。

## 13. 安全与容量

- 指标标签必须保持低基数；路由使用模板，服务名来自注册元数据。
- Runtime API 必须限制允许的窗口、步长、排序和服务名。
- PromQL 由服务端模板生成，不接受浏览器提交任意查询。
- 聚合查询设置并发上限、超时和最大返回点数。
- 页面轮询可见时默认 15 秒一次；后台标签页暂停轮询。
- Runtime API 使用短缓存合并相同窗口请求，避免每个浏览器重复压测 Prometheus。
- 日志只记录稳定事件名、数据源、状态和耗时，不记录 PromQL、凭据和业务数据。
- 指标采集不得改变请求错误、重试、fallback、ACK 或可靠写语义。

## 14. 测试与验收

### 14.1 Go 单元测试

- HTTP 路由模板、状态类别和 Histogram 标签。
- gRPC Client 调用边记录 source/target/route/protocol。
- gRPC Server Prometheus、Duration、Trace 和可信身份顺序。
- 非法 payload 服务名不能污染指标标签。
- Runtime Aggregator 的 Prometheus 查询模板、合并和百分位计算。
- `ok/partial/stale/unavailable/not_collected` 状态表。
- Prometheus 超时、部分实例缺失和样本不足。
- Provider 注册、快照和关闭并发。

### 14.2 前端测试

- 全局节点、同步边和异步边渲染。
- 服务点击、返回和时间窗口保持。
- 请求排序与实例分布。
- 无数据、部分数据、过期和不可用状态。
- 轮询取消、页面卸载和重复请求。
- 键盘操作、可访问名称和非颜色状态表达。

### 14.3 集成与 UAT

- 启动真实 Prometheus 和示例 07 多进程/多副本环境。
- 产生真实下单、查询、撤单和事件流量。
- 通过 Runtime API 验证服务、边、路由、实例和组件指标。
- 执行单副本停止、MySQL 暂停、MQ 暂停和 Prometheus 停止场景。
- 验证 ServerManage 未认证拒绝，响应不泄露内部地址和凭据。
- 验证旧 Statistics 路由不会被重新暴露。

### 14.4 发布门禁

至少包含：

```bash
go test ./pkg/server/router ./pkg/server/trans/rest ./pkg/server/transport/grpc -count=1
go test -race ./pkg/server/router ./pkg/server/transport/grpc -count=1
./scripts/test.sh api-compat
./scripts/test.sh public-api
./scripts/test.sh config-contract
./scripts/test.sh release-contract
```

前端执行定向 Jest、TypeScript 检查和生产构建；示例 07 执行真实多进程 UAT。没有 Prometheus/外部依赖的普通单元测试不得静默 Skip 后宣称完整验收。

## 15. 实施顺序

1. 定义低基数指标名、标签和 Runtime DTO 契约。
2. 补齐 Core gRPC Server 观测拦截器。
3. 在 ServiceResolver/TransportSelector 增加调用边指标。
4. 接入 Pending、Outbox、EventBridge 等 RuntimeMetricProvider。
5. 建立 Prometheus 查询与 Runtime Aggregator。
6. 提供 ServerManage Runtime API。
7. 用示例 07 建立真实指标 UAT。
8. 实现 Web Admin 全局图和服务请求视图。
9. 将旧 RouterStats 标记废弃并完成消费方扫描。
10. 在替代链路稳定后单独批准和执行破坏性删除。

## 16. 完成标准

只有同时满足以下条件，第一版才算完成：

1. 示例 07 的真实多进程流量能生成服务节点、调用边、路由、实例和组件指标。
2. HTTP 与 gRPC 都进入同一 Runtime API 口径。
3. 单实例、数据源和基础设施故障均显示诚实降级状态。
4. Admin 不依赖 mock，不直接访问 Prometheus 或业务实例。
5. 未认证用户不能访问 Runtime API。
6. 指标和日志不包含高基数业务标识或敏感数据。
7. 旧 RouterStats 不再承担任何生产数据路径，并已进入明确的废弃流程。
