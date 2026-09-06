# 多服务、事件、WebSocket 与可观测

## 多服务调用与事件

以 `examples/06-shop-microservices` 为标准模板：

- 稳定服务名和事件名放根 `contract`；跨服务 JSON 结构放根 `dto`，不共享 Model。
- 调用方直接构造目标服务已注册的 Public API，不建保存地址的 client，也不复制 `api/call` 路由。如果 Go 目录名与 `IService.ServiceName()` 不同，目标 API 必须在 Freeze 前同时声明 `router.WithServiceName(contract.XxxServiceName)` 和稳定 `WithPath`。
- 内部专用 Public 用 `router.WithInternalCallers(...)` 声明允许服务；冻结后通过 `GetInternalCallers()` 读取。匿名 `/api/openapi` 过滤这些路由且不输出白名单；兼容快照和使用 `ServerManageAuth` 的 `/api/internal/openapi` 才记录 `x-internal-callers`。
- `req.CallService` 先查同进程 ServiceContext，再查 ClusterProvider 健康快照。新链路不读 `AttachServices`；无节点时 fail closed。
- 默认 `req.CallService` 继续在健康副本间轮询。业务数据按市场、租户等稳定 key 分片时，调用方必须检查加性接口 `types.IRequestKeyedServiceCaller`，并调用 `CallServiceWithKey(router, hashKey)`；ServiceContext 后台任务使用同名方法，并以 `OwnsServiceKey(ctx, serviceName, hashKey)` 判断本实例是否可竞争业务 lease。空 key 会 fail closed，同 key 通过 rendezvous consistent hash 按稳定服务端点固定到同一健康实例，候选发现顺序不同、容器重启也不改变 owner；keyed owner 判断不会因同进程本地路由而绕过完整成员快照。同步调用、市场事件和任务 owner 必须使用完全相同的规范化 key；成员变化时只允许迁移到新增/存活 owner，业务仍负责恢复、generation fencing 和幂等。
- 同进程调用方身份来自源 ServiceContext。同步跨进程调用默认使用 gRPC；服务端只在已验证客户端证书 SAN 等于载荷 `SourceService` 时注入可信身份。HTTP、Header、请求字段、无证书和 SAN 不匹配都不能建立内部身份，并在 Parse 前拒绝。
- 客户端按 endpoint 复用 go-zero `zrpc.Client`；Core Resolver 仍是唯一节点发现权威，不启用 zrpc 自带发现。
- 同进程模式只供调试；部署演示必须以独立进程、独立 SQLite 和 mTLS gRPC 再验收一次，并断言 HTTP 调用计数为零。
- HTTP 仅可作为显式发送前 fallback；gRPC 开始发送后不得跨协议重试。内部异步事件使用 EventBridge，WebSocket 只面向最终用户。
- Redis 发现和 EventBridge 使用不同 Prefix。业务服务只声明 `sc.UseOutbox(models.OutboxStore{})` 启用本服务可靠发布；`OutboxStore` 只实现 `LoadPending(ctx, limit)` 和 `MarkPublished(ctx, message)`，不关心当前服务名、消费者或 MQ。当前服务名由 `ServiceContext` 写入事件 Source，Subject/EventType/Payload/TraceID 来自 Outbox 记录。
- `UseOutbox` 和 `Subscription` 的零值永远保持全 subject 串行。只有业务已提供稳定 `ShardKey/OrderingKey`、Store按 key公平返回且消费者具备 Inbox/业务幂等时，才可显式使用 `UseOutboxWithOptions(..., OutboxRuntimeOptions{KeyConcurrency:N})` 与 `Subscription.KeyConcurrency=N`。同 key仍严格串行；不同 key不承诺全局完成顺序；provider不支持或同一 subject配置冲突时启动失败，禁止静默退回伪并行。
- 业务服务只用 `sc.SubscribeEvent(event.Subscription{Subject, EventType, Reliable, Handler})` 订阅内部事件，不直接注册 `SubscribeControl` 和 `SubscribeExternalControl` 两套订阅。`Subject` 决定外部通道，`EventType` 是可选过滤条件；`EventType` 为空表示订阅该 Subject 下全部事件类型。`Reliable=true` 时 Handler 返回 error 会阻止当前逻辑服务消费组 ACK。
- 低频、幂等的副本收敛控制事件（例如市场准备/激活）可显式设置 `Reliable:true, Broadcast:true`，每个稳定服务端点使用独立 consumer group 各消费一次。该选项不得用于价格、成交、结算等高频数据面；数据面仍使用逻辑服务共享组或按 key 分区。
- 控制事件的 Handler 返回 error，成功后才 ACK；失败留 pending 并允许同组 reclaim。多个服务订阅同一 Subject 时按逻辑服务消费组独立 ACK；同一服务内多个可靠 Handler 全成功才 ACK。
- 生产写路径必须同事务写业务事实和 Outbox；消费方以 EventID 写 Inbox 或等价幂等事实。发布方只负责发布事实，不知道也不等待消费者处理完成。
- User 下单必须提供业务 `requestID`；事实服务用 `{UserID}:{requestID}` 唯一约束和请求指纹收敛并发重试。
- Supplier 使用统一 Manage Hook 同时处理本人和管理员权限；Order 可靠事件按 `OrderID` 幂等写本地永久 `SupplierOrder`，删除 Hook 只查询该投影，禁止同步查询远端判断能否删除。
- WebSocket 仅把本服务已消费的订单摘要推送给当前最终用户，不承担服务间传输和离线积压。

典型声明：

```go
func (g *GetProducts) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(g,
		router.WithServiceName(contract.SupplierServiceName),
		router.WithPath("/api/"+contract.SupplierServiceName+"/getproducts"),
		router.WithInternalCallers(contract.UserServiceName, contract.OrderServiceName),
	)
}
```

负向测试必须覆盖普通 HTTP、缺少可信身份、错误服务、伪造 `SourceService`、无客户端证书和 SAN 不匹配，并断言 `Parse/Validation/Do` 均未执行。兼容性变更同时运行 `go test ./internal/compat`、`./scripts/test.sh api-compat` 和 `./scripts/test.sh release-contract`。

验收必须同时运行 `examples/integration/06-shop-microservices` 和 `examples/integration/06-shop-microservices-three-process`。

反向代理必须配置 `ServerConfig.TrustedProxies` 的 IP/CIDR。默认空表示忽略 XFF/X-Real-IP；本地/private peer 携带 forwarding header 且没有信任策略时 fail closed。

## WebSocket 最终用户订阅

WebSocket 只面向最终外部用户。内部服务之间不使用 WebSocket，内部请求使用 TransportSelector，内部事件使用每个 ServiceContext 所属的 EventBridge。

订阅使用真实路由路径：

```text
/api/shop/getorders
```

private WebSocket 路由至少需要以下职责：

- `IWebSocketUserIdentity`：`SetUserID` 接收 WebSocket 登录会话解析出的可信身份，`GetUserID` 返回已绑定身份。private 路由使用 WebSocket 时必须实现。
- `IRouterHashKey`：按 UserID 生成稳定 hash，将不同用户放入不同订阅组。
- `IWebSocketRouterNotice`：校验消息 DTO，并只向消息所属用户投递。

框架为每次订阅直接创建并持有独立路由实例，直到退订或连接关闭；WebSocket 订阅实例不进入普通请求对象池。路由没有额外启动/停止资源时，不要为了形式实现空的 `IWebSocketRouter` 生命周期回调。

通知应复用 HTTP DTO：

```json
{
  "action": "created",
  "id": "123",
  "productID": 1,
  "productName": "示例商品",
  "unitPrice": "39.8",
  "quantity": 2,
  "userID": "user-a",
  "createdAt": "2026-07-14T10:00:00Z"
}
```

不要再包一层 `order`，也不要把 action 写回 HTTP DTO 原对象。通知过滤失败、类型不匹配或用户不匹配时直接不投递。

跨节点模式要求 ClusterProvider 和 CrossNodeNoticeBroker 已由 ServiceContext 启动。forwarder 按服务名隔离；IPv6 地址通过 `net.JoinHostPort`；非 2xx 转发视为错误。

worker 生命周期由通知系统持有；队列满、filter timeout、panic 和 shutdown timeout 是 error，worker 启停是 debug。不得记录消息体。

## 多服务运行图（Runtime API）

运维与 Admin 监控使用框架 Runtime 链路，**不要**恢复旧 `RouterStats` 或未注册的 `/api/servermanage/statistics`。

| 入口 | 路径 | 认证 | 用途 |
| --- | --- | --- | --- |
| 全局拓扑 | `POST /api/servermanage/runtimetopology` | ServerManageAuth | 逻辑服务节点、同步/异步边、窗口聚合 |
| 单服务详情 | `POST /api/servermanage/runtimeservice` | ServerManageAuth | 路由请求聚合、实例分布、组件指标 |

契约要点：

- 请求体/查询支持 `window`：仅 `15s`、`5m`、`1h`；`runtimeservice` 另需 `service`。
- **ClusterProvider** 提供服务实例与地址；**Prometheus** 提供 rate/error/histogram 历史；RouterInfo 提供稳定路由元数据；Pending/Outbox/EventBridge 等通过本进程 Collector 暴露后由 Prom 查询。
- Runtime Aggregator 只部署在 ServerManage 可达边界：业务副本暴露 scrape 指标，**禁止** Aggregator 在 API 请求中直连各实例 `/metrics` 或 Provider。
- 指标诚实状态：`ok` / `partial` / `stale` / `unavailable` / `no_traffic` / `not_collected`。缺失时数值为 `null` + `state`，不得把未采集写成 0。
- 同步边：跨服务 gRPC/内部调用；异步边：Outbox 发布与订阅索引汇合及低基数 gauge。全局图画逻辑服务，组件进入服务内部视图。
- 异步订阅在当前窗口没有匹配发布序列时属于空闲/未采集路径：保留 `source` 为空、数值为 `null`、状态为 `not_collected` 的虚线边，不产生 `async_publish_missing`。启动即注册的低频事件和失败路径不得因此持续告警。
- 发布 counter 序列存在但窗口 rate 为 `0` 时，异步边保留已知发布方并标记 `no_traffic`；发布查询失败使用 `event_publish_query_partial` 与 `unavailable`/`partial`，不得伪装成 missing。
- 只有“有发布样本、无注册订阅”产生 `async_subscription_missing`；不猜测目标节点。warning 的 `message` 包含 `subject_family` 和已知 `event_type`，`scope` 使用 family 语义，响应按 `(code, scope, message)` 去重。
- 当前窗口查询无法区分“从未发布”和“历史发布后停止”，因此 Core 不输出 `async_publish_missing`。若未来增加长保留存在性证据，只能在确认曾有发布而当前窗口消失时使用该 code。
- 标签低基数：禁止 userId、orderId、TraceID、原始 URL、SQL 等作为指标标签。
- 兼容级别见 `docs/codex/API_COMPATIBILITY_SURFACE.md`（Experimental → 趋向 Stable）与 `docs/codex/DEPRECATION_REGISTER.md`。
- Web Admin 页面为 `MonitorSystem`，前端经 ServerManage 调 Runtime API，浏览器不直连 Prometheus。

实现位置：`pkg/server/runtime`、`pkg/server/api/public/runtimetopology.go`、`pkg/server/observability`；验收可参考示例 07 的 scrape 配置与 multi-process 集成测试。

## Cluster、Transport、MQ 与事件

- Local cluster：`Stable`。
- etcd/Consul：`Conditional`，需要显式配置和外部依赖。
- 内部同步传输默认 gRPC，HTTP 只作为显式备用；自定义 Socket 已删除，迁移见 `docs/codex/GRPC_TRANSPORT_MIGRATION.md`。
- gRPC Client 复用 zrpc，Server 因 go-zero v1.10.2 无法独立停止单 listener 而保留薄 grpc-go 生命周期适配；跨主机生产使用 mTLS，已有双向身份的服务网格使用 mesh。Client 侧可复用 zrpc 指标中间件；服务端与跨服务 call-edge、Pending/Outbox 等低基数指标经 Core Collector 进入 Prometheus，供 Runtime API 聚合。
- QUIC 和 MQ transport：`Unsupported`，配置校验拒绝。
- MQ/EventBridge：Redis Streams、NATS JetStream 为 `Conditional`。
- 对需要可靠保留、重放或安全回收的 Subject，应用必须在组合根通过 `ServiceContext.RequireMessageLifecycle(ctx, policy)` 声明真实必需逻辑消费组、起点、保留、重试、DLQ、容量与回收预算。完整状态机、Provider 能力矩阵、迁移和扩展标准见 [MQ 消息生命周期与 Provider 扩展标准](../../codex/MQ_MESSAGE_LIFECYCLE_GUIDE.md)。
- 生命周期策略属于应用 contract manifest，不属于 Manage/API，也不由通用 MQ 配置猜测。`RequiredGroups` 只能来自实际 `Reliable` 订阅注册点；离线组仍阻断回收，单组 ACK 不代表全部业务完成。无声明时保持旧行为：不自动回收、无限重试，不静默删除历史。
- Redis publish 只声明 `broker-accepted`；要求落盘确认的应用必须选择提供 `broker-persisted` 的 Provider。Redis 回收只允许 fenced Lua 校验后的有界 `XDEL`；NATS 使用全部 durable `AckFloor`、KV 前沿和有界 purge。禁止应用自行 `MAXLEN`、TTL、单组 ACK 后删除、删离线组或重置游标。
- 策略先以 `observe` 上线核对 pending/lag/oldest/容量，再通过重启切换 `enforce`。Provider capability 不满足、既有组 enrollment 未知、自动淘汰策略不安全、必需组缺失或前沿回退时必须 fail closed。
- 有序可靠投递为加性契约：`mq.PublishOptions.OrderingKey`、`OrderedReliableMQProvider`、`MQManager.RequireOrderedReliable`、EventBridge透传与 Outbox earliest-first / 可选 `OutboxStoreSkipBlocked` 等以 `docs/codex/API_COMPATIBILITY_SURFACE.md` 与当前测试为准；未声明 requirement 时零值兼容。分键并发另需 `KeyedReliableMQProvider` 能力和 `VerifyKeyedReliableConcurrency`；Redis仍只有一个 active owner，只在 owner 内并行不同 key。
- Runtime 低基数指标公开 Outbox 的配置并发/实际峰值/active lanes/blocked keys/batch，以及 MQ可靠订阅的配置并发/handler in-flight/active/blocked/pending keys；缺少 provider指标时状态为 `not_collected`，禁止伪造零值。
- JetStream 可靠数据库写路径先阅读 `docs/codex/NATS_JETSTREAM_WRITE_PATH_GUIDE.md`；当前 Provider 已支持 lifecycle manifest、有界重试、DLQ、`MaxAckPending` 和安全前沿，但业务数据库幂等、事务 Outbox、容量值和 Broker 生产拓扑仍由应用负责。
- Kafka/RabbitMQ/RocketMQ：无内建 Provider；应用可在 `MQProvider` 后注册自定义 `ProviderFactory`。

go-zero `core/queue` 只用于进程内队列，不能替代 Broker。

## 日志与错误

- `logx.Infow`：生命周期、切换、成功降级。
- `logx.Debugw`：重试、路由注册、worker 和高频细节。
- `logx.Errorw`：最终失败、数据风险、panic、关闭失败。
- `logx.Sloww`：测量超阈值。

请求/跨服务失败携带 `trace_id`、service、route/target、operation 和 error。错误由拥有重试、降级、响应或终止决策的边界记录一次。

禁止记录凭据、token、cookie、TOTP、完整 payload/body/response、DSN、SQL、参数和对象 dump。
