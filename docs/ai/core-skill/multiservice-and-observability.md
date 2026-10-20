# 多服务、事件、WebSocket 与可观测

## 稳定实例身份与注册恢复

需要稳定副本身份时显式设置 `Cluster.IdentityMode=stable`，提供绝对 `IdentityFile` 和独立 `IDGenerator` 配置；先用 `cmd/instance-identity` 离线初始化新权威和空身份卷，普通启动不自动 create。注册主键仅来自服务名和 InstanceID；MachineID/最终 WorkerID 由独立持久账本分配，发现 TTL 不释放编号。默认 legacy 维持旧契约。

local/Redis/etcd 提供原子 owner 能力；Consul stable 拒绝。运行期节点丢失由 MembershipManager 按同 Boot 恢复，消费方不写重注册循环。`/api/health` 表示存活，`/api/ready` 表示本进程准入；后台任务开始业务操作前检查 `ServiceContext.CheckServiceAdmission()`。硬崩溃后须先隔离旧进程，再显式 recover，不能凭 TTL 抢占。注册保护不能代替数据库提交 fencing。

完整配置、实际 WorkerID 别名/容量、时间预留、import/recover/retire、独占卷与回滚要求见 [稳定实例身份指南](../../codex/STABLE_INSTANCE_IDENTITY_GUIDE.md)。不得移动既有 pending、重写历史 ID、把复制卷当成新副本，或用无上下文 `InitRequest.NewID` 生成业务主键。

`UseOutbox` 默认每 100ms 唤醒、单次取数 100 条；取满且有发布进展时立即继续下一批，不是每秒 1000 条限流。失败记录保留重试；发布成功标记不代表消费者已 ACK。Inbox 是否是待处理队列由应用定义，示例 06 的 Inbox 是与业务投影同事务提交的已处理事件去重记录，未成功消息由可靠 MQ 订阅重投/reclaim。纯内存事件中心可注册和订阅，但内存队列自身不跨进程崩溃保留消息。07 已接回框架自适应 write-behind worker，不再使用原 100ms/100 条业务 ticker；该调整不改变 Outbox 的全局默认调度。

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
- Redis 发现和 EventBridge 使用不同 Prefix。业务服务只声明 `sc.UseOutbox(models.OutboxStore{})` 启用本服务可靠发布；`OutboxStore` 必选 `LoadPending(ctx, limit)` 和 `MarkPublished(ctx, message)`，不关心当前服务名、消费者或 MQ。当前服务名由 `ServiceContext` 写入事件 Source，Subject/EventType/Payload/TraceID 来自 Outbox 记录。高吞吐服务应再实现可选 `event.OutboxBatchMarker.MarkPublishedBatch`：框架先按现有规则逐条发布成功前缀，再一次事务确认这些 EventID；未实现时仍逐条 `MarkPublished`。空切片必须立即成功。崩溃发生在 MQ 发布后、批量确认提交前只形成 at-least-once 重复，消费者继续用 EventID/Inbox 幂等。不要为此自写 Outbox 发布循环。
- `UseOutbox` 和 `Subscription` 的零值永远保持全 subject 串行。只有业务已提供稳定 `ShardKey/OrderingKey`、Store按 key公平返回且消费者具备 Inbox/业务幂等时，才可显式使用 `UseOutboxWithOptions(..., OutboxRuntimeOptions{KeyConcurrency:N})` 与 `Subscription.KeyConcurrency=N`。同 key仍严格串行；不同 key不承诺全局完成顺序；provider不支持或同一 subject配置冲突时启动失败，禁止静默退回伪并行。
- 业务服务只用 `sc.SubscribeEvent(event.Subscription{Subject, EventType, Reliable, Handler})` 订阅内部事件，不直接注册 `SubscribeControl` 和 `SubscribeExternalControl` 两套订阅。`Subject` 决定外部通道，`EventType` 是可选过滤条件；`EventType` 为空表示订阅该 Subject 下全部事件类型。`Reliable=true` 时 Handler 返回 error 会阻止当前逻辑服务消费组 ACK。
- 低频、幂等的副本收敛控制事件（例如市场准备/激活）可显式设置 `Reliable:true, Broadcast:true`，每个稳定服务端点使用独立 consumer group 各消费一次。该选项不得用于价格、成交、结算等高频数据面；数据面仍使用逻辑服务共享组或按 key 分区。
- 控制事件的 Handler 返回 error，成功后才 ACK；失败留 pending 并允许同组 reclaim。多个服务订阅同一 Subject 时按逻辑服务消费组独立 ACK；同一服务内多个可靠 Handler 全成功才 ACK。
- 生产写路径必须同事务保存业务事实和持久投递意图（独立 Outbox 或经确认的等价业务记录）；消费方的 Inbox 或等价幂等事实必须与对应业务更新同事务提交，成功后才 ACK。具体存储方式先按下节与使用者确认；发布方的 MQ 发布确认不表示消费者处理完成。
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

### Outbox/Inbox 接入前的业务确认

新增或改变业务可靠消息接入时，**以下属于应用业务决策，必须与使用者确认后再实施，Core 和 AI 不得根据模型字段、服务数量或示例自行猜测**。先核对已有需求、项目文档和当前会话中的明确决定；已有明确结论直接沿用，不重复询问。缺失项应列出具体选项、推荐及代价，等待使用者明确回复；未决项只记录为待确认，不据此新增表、字段、状态机、目标或清理策略。确认结论记录在具体项目的设计文档/README 中。

| 确认项 | 必须明确的业务含义 |
| --- | --- |
| 事件语义与版本 | 每次业务变化是否都必须投递，还是只同步最新状态、允许合并中间版本；事件身份、版本及重试时使用的内容如何保存或从不可变事实重建 |
| 投递目标 | 是一次发布 MQ 后由多个逻辑消费组订阅，还是生产方需执行多个独立目标的投递；必需目标及后续变更、历史补投如何定义 |
| 完成状态 | 主状态和子状态分别表示 Broker 接管、接收方持久接收还是业务完成；是否需要业务回执，失败后如何重试/处置 |
| 生产方记录复用 | 复用业务记录的投递状态/业务流水，还是独立 Outbox；单目标是否省去投递子表，多目标是否需要子状态及其完成度汇总 |
| 消费方幂等 | 复用已有业务唯一键/流水，还是独立 Inbox；以什么稳定消费身份及 EventID/业务版本防重，是否跨副本有效 |
| 事务与保留 | 业务事实、投递意图和消费幂等依据各由谁在什么事务内提交；消息正文、投递明细及去重依据何时可回收，需覆盖哪些重试/重放范围 |

确认方案时保留以下技术边界，不把可选存储形式写成统一业务默认：

- **事件数量与目标数量分开判断。** 一条不可变业务记录对应一个事件、一个发布目标时，可以直接保存投递状态；需要多个独立目标时，可按需增加目标子状态。同一订单若有创建、支付、取消等必须保留的多个事件，即使只有一个目标，也要保留各事件身份、内容与状态，可复用既有业务流水。只有使用者确认允许合并时，才可只保留最新状态。
- **MQ 扇出不自动产生生产方子状态。** 一次发布到一个 Subject、多个消费组各自处理，通常仍只有一个发布目标；是否需要逐服务业务完成度由使用者定义。`published` 不能被解释为所有服务已处理，也不得据此删除 Broker 消息。发布确认等级须满足已声明的 Provider 能力要求。
- **复用业务记录仍须守住事务与版本。** 投递意图随业务变化同事务提交；确认更新匹配具体事件/版本及目标，避免旧确认覆盖新 pending。Inbox 或复用的业务幂等事实随消费业务同事务提交，持久唯一约束等机制保证并发与跨副本防重。业务写 MySQL、消息标记单独写 Badger 不构成同一事务。
- **复用存储不改变框架入口。** 已确认的业务记录可通过适配 `OutboxStore` 接入现有 `UseOutbox`，可靠消费仍走 `SubscribeEvent`；不得为省表另写发布轮询或重复订阅。现有接口不会自动识别业务状态，应用须实现对应存储适配；不能把本节建议描述为 Core 已自动完成。

#### 可选事务消息工具

`pkg/persistence/message` 提供加性工具，不持有数据库连接配置，不预设消息表；业务方案仍须按上表确认：

- `message.Run(ctx, base, operation)`：从支持 `Clone` 的 `IDataAction` 创建独立事务；错误、取消及 panic 回滚，成功才提交，不自动重放提交结果未知的写入。建表必须在启动阶段完成，不在回调中 EnsureStorage。
- `message.ConsumeInboxWith(ctx, action, receipt, identity, apply)`：加入调用方已经开启的 SQL 事务。应用提供消费依据模型、完整幂等查询条件及数据库唯一约束，工具查重后执行业务并插入依据。它也支持把依据保存为业务流水；提交成功后才 ACK。并发冲突必须回滚并返回，由可靠订阅重投；回调中的 HTTP/WebSocket 不在事务保证内。
- `message.ConfirmPublishedWith(ctx, action, model, ids, fields)`：通过显式 `PublishedFields` 指定布尔发布字段及可选时间字段，锁定并核对完整主键集合后，分批执行状态更新；空批次不访问数据库。仅支持每行一个不可变事件且无逐行业务更新 Hook 的模型。可变业务行的版本条件、目标子状态和业务完成度仍由项目适配，不能省略版本比较后直接调用。
- SQLite/MySQL action 提供可选 `types.ITransactionDB.GetTransactionDB()`，只返回已经开启的事务连接，未开启/未完成懒初始化则拒绝。不要用 `GetRunDB()` 作为事务连接；其原有基础连接语义保持不变。现有 `IDataAction` 实现无需新增必选方法，未支持显式 SQL 事务能力的适配器须保留原逐条路径或明确拒绝，不得悄悄使用基础连接。

### Outbox 批量确认边界

`OutboxBatchMarker` 的成功必须表示全部请求记录在同一事务内持久确认；重复 ID 去重，已确认记录成功，缺失/零主键返回错误并回滚，不得静默部分成功。它确认的是发布状态，不是消费者 ACK，不改变 MQ 生命周期或授权删除 Broker 消息。确认失败或提交结果未知时可能重放整个已发布前缀；同 key 的首次发布顺序保持不变，但重复消息可能出现在较新消息之后，消费者仍须按 EventID 幂等。批量确认减少事务次数，不等于一条批量 UPDATE，也不保证低流量下减少 fsync。

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

跨节点模式要求 ClusterProvider 和 CrossNodeNoticeBroker 已由 ServiceContext 启动。forwarder 按服务名隔离；通知节点必须合并本地订阅 hash 与 `ICrossNodeSubscriptionIndex` 提供的对等节点订阅 hash，不能因本节点没有同 hash 订阅而丢弃远端用户通知。IPv6 地址通过 `net.JoinHostPort`；非 2xx 转发视为错误。

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
- 运行图的 QPS、错误率、p50/p95/p99、路由排行与同步调用边只表示业务数据面，统一排除 `/api/manage/*` 与 `/api/servermanage/*` 控制面路由。控制面仍保留安全访问日志与原始 Prometheus 时序，但不得污染业务容量和延迟判断。
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

- MQ 未配置时默认 `Mode=off`，不创建 Provider，也不探测 Redis；需要跨节点 EventBridge 的服务在自己的 `etc/<service>.json` 显式设置 `MQ.Mode=on/auto` 和 Broker 地址。`etc/server.json` 是系统管理服务配置，不负责替业务服务启用 MQ。已有配置中的显式 `auto/on` 不会被新默认值覆盖，详见配置能力矩阵的迁移说明。

- 框架自己的 Redis 发现唤醒流使用统一 `MAXLEN ~ 10000`；不是业务事实，节点 TTL 状态和 `ttl/2` 对账保持权威。Local/etcd/Consul 不另建同类消息流，不能强行套用 Stream 删除。
- **框架内部例外**：shared 路由缓存失效与 Casdoor 身份变更由 ServiceContext 专用桥使用 Redis Pub/Sub / Core NATS 每副本广播，并用权威对账补偿；不创建持久组，不向业务开放通用瞬时 API。业务 EventBridge、ACK、必需组和 `NoRequiredGroups` 不变。新 Provider 必须独立验证内部广播能力，不能靠注册工厂假装支持。
- 内部协议升级须同逻辑服务维护窗口切换；不自动删除旧内部 Stream，不保证新旧协议混跑。权限、容量、故障预算和精确旧资源核对见 [Core 内部通知生命周期标准](../../codex/CORE_INTERNAL_NOTIFICATION_LIFECYCLE_GUIDE.md)。业务项目不得自行日常 XTRIM。

- Local cluster：`Stable`。
- etcd/Consul：`Conditional`，需要显式配置和外部依赖。
- 内部同步传输默认 gRPC，HTTP 只作为显式备用；自定义 Socket 已删除，迁移见 `docs/codex/GRPC_TRANSPORT_MIGRATION.md`。
- gRPC Client 复用 zrpc，Server 因 go-zero v1.10.3 的 zrpc.RpcServer.Stop 仍只关闭日志、不能独立停止单 listener 而保留薄 grpc-go 生命周期适配；跨主机生产使用 mTLS，已有双向身份的服务网格使用 mesh。Client 侧可复用 zrpc 指标中间件；服务端与跨服务 call-edge、Pending/Outbox 等低基数指标经 Core Collector 进入 Prometheus，供 Runtime API 聚合。
- QUIC 和 MQ transport：`Unsupported`，配置校验拒绝。
- MQ/EventBridge：Redis Streams、NATS JetStream 为 `Conditional`。
- 对需要可靠保留、重放或安全回收的 Subject，应用必须在组合根通过 `ServiceContext.RequireMessageLifecycle(ctx, policy)` 声明真实必需逻辑消费组、起点、保留、重试、DLQ、容量与回收预算。完整状态机、Provider 能力矩阵、迁移和扩展标准见 [MQ 消息生命周期与 Provider 扩展标准](../../codex/MQ_MESSAGE_LIFECYCLE_GUIDE.md)。
- 生命周期策略属于应用 contract manifest，不属于 Manage/API，也不由通用 MQ 配置猜测。`RequiredGroups` 只能来自实际 `Reliable` 订阅注册点；离线组仍阻断回收，单组 ACK 不代表全部业务完成。无声明时保持旧行为：不自动回收、无限重试，不静默删除历史。
- **纯保留主题**必须显式设置 `NoRequiredGroups:true`、空 `RequiredGroups`、正数 `Retention.MinAge`，且 `Retry` 必须为零值；空 slice 本身不是授权。只有应用确认主题无 Broker 消费组时才允许该声明，不能从本进程未注册订阅推断集群无必需组。当前普通 `Subscribe` 也创建组，因此无组主题的普通和可靠订阅均被拒绝；发现既有/意外组后保守停止回收。到期回收不代表业务处理完成，不得用该模式绕过成交等事实事件的可靠消费；后续增加消费者必须先停回收、走 manifest 迁移和重放维护窗口。
- `LifecycleCapabilities.NoRequiredGroups` 是独立能力，旧自定义 Provider 的零值会拒绝无组声明。Redis 无组检查与删除在同一 Lua 内完成；NATS 在 purge 前复核消费者数量，但不提供 check-and-purge 原子事务，部署必须禁止未协调的 consumer 创建/重建。统一策略和 Broker 管理面隔离是前提，不得把定向 race 通过当作跨 Broker 管理事务证明。
- Redis publish 只声明 `broker-accepted`；要求落盘确认的应用必须选择提供 `broker-persisted` 的 Provider。Redis 回收只允许 fenced Lua 校验后的有界 `XDEL`；NATS 使用全部 durable `AckFloor`、KV 前沿和有界 purge。禁止应用自行 `MAXLEN`、TTL、单组 ACK 后删除、删离线组或重置游标。
- 策略先以 `observe` 上线核对 pending/lag/oldest/容量，再通过重启切换 `enforce`。Provider capability 不满足、既有组 enrollment 未知、自动淘汰策略不安全、必需组缺失或前沿回退时必须 fail closed。
- 内置 Redis/NATS 生命周期 worker 在完整 Inspect 前竞争跨周期 subject 租约；standby 只刷新轻量容量，不重复扫描全部组。`TimeBudget` 仍是整轮上限（观测最多 70%，回收使用独立 context），启动和周期有界抖动。不得通过延长业务 policy、清数据、改 observe 或忽略 deadline 掩盖 worker 故障；v1.1.1 的 fingerprint/metadata 原地兼容，混跑旧版期间不保证命令去重收益。诊断同时查看 `reclaim_fail_total` 和固定 `deadline|owner_lost|policy_fence|provider` 原因指标，不能只看某一实例；完整 pending/lag 未采集或过期不得冒充零。详见生命周期指南的多实例 worker 章节。
- 有序可靠投递为加性契约：`mq.PublishOptions.OrderingKey`、`OrderedReliableMQProvider`、`MQManager.RequireOrderedReliable`、EventBridge透传与 Outbox earliest-first / 可选 `OutboxStoreSkipBlocked`、可选 `OutboxBatchMarker` 等以 `docs/codex/API_COMPATIBILITY_SURFACE.md` 与当前测试为准；未声明 requirement 时零值兼容。分键并发另需 `KeyedReliableMQProvider` 能力和 `VerifyKeyedReliableConcurrency`；Redis仍只有一个 active owner，只在 owner 内并行不同 key。同 key 仍串行 Publish；批量确认只作用于已发布前缀。
- Runtime 低基数指标公开 Outbox 的配置并发/实际峰值/active lanes/blocked keys/batch，以及 MQ可靠订阅的配置并发/handler in-flight/active/blocked/pending keys；缺少 provider指标时状态为 `not_collected`，禁止伪造零值。
- Redis 显式 `KeyConcurrency>1` 使用 owner 会话级固定 worker，读取窗口不再等待整批 handler。每 key 只准入最早未完成消息；成功或复核已 ACK 后回扫 PEL，失败仅阻断本 key。`Count` 默认 `KeyConcurrency*4`、有效下限为并发度，同时限制分页和本 owner 新预取的 PEL；还受生命周期 `MaxAckPending` 限制。满预算时背压新读取，不能承诺穿过任意数量热 key 消息找到后方 key；接管超限旧 PEL 时只分页恢复，不清空或重置消费组。`pending_keys` 是本地跟踪 key 数，`prefetched_messages` 是组 PEL 消息数，两者不等价。
- Redis 可靠订阅的 ACK 由 provider 在 handler 成功后执行，handler 不得调用 `Message.Ack` 提前确认。分 key 超时/取消后仍等待原 handler 实际退出，再结束会话或重试；Go 不能强杀不合作的 handler，它会占住有限 worker 并延迟 `Close`，业务 I/O 仍须有界。不要增加 goroutine 绕过失败屏障。发布/读取/调度/handler 的 OTel span 通过 Redis message ID 关联，该 ID 只作 span 属性，不作指标标签；PEL/reclaim 与新读取分开标记，接管前首次读取未知不得补造时钟。当前修复的命令、观测口径、背压和升级边界见[验收记录](../../codex/REDIS_RELIABLE_CROSS_WINDOW_FIX_2026-10-20.md)。
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
