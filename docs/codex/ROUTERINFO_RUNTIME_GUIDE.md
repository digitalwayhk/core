# RouterInfo 运行时使用指南

## 定位与所有权

`RouterInfo` 是一条路由在一个 `ServiceContext` 中唯一、长期存在的元数据对象。它由 `ServiceContext` 所有，进程级 ServiceContext registry 只提供按服务名查找和同名冲突协调，不拥有路由运行状态。

路由注册完成后，`Path`、`ServiceName`、`Auth`、`Method`、`PathType`、结构名和实例类型会被冻结。冻结后修改会被明确拒绝。`RouterInfo` 不保存当前请求、用户、trace、响应、缓存条目或 WebSocket 请求实例。

上述公开元数据字段仅为现有路由构造代码保留：它们可在注册前设置，注册冻结后必须视为只读。`ServiceRouter` 在查询和枚举路由时会校验冻结快照，检测到篡改会 fail closed。`TempStore` 已废弃，只为源码兼容保留；新代码不得写入，尤其不得用于存放请求、用户、trace 或响应状态。

同名 ServiceContext 只有在规范化配置指纹一致且原实例仍活动时才能复用。配置不同会 fail closed；服务关闭后按实例身份注销，后续可重新创建，不会取得旧缓存、事件或 WebSocket 状态。

## 可信内部调用方

内部专用路由仍使用 Public 的序列化和服务发现能力，但必须用 `router.WithInternalCallers(...)` 声明允许的逻辑服务名。注册完成后白名单随 RouterInfo 一起冻结，`GetInternalCallers()` 返回防御性副本；OpenAPI 路由扩展字段 `x-internal-callers` 用于兼容性审计，不表示浏览器可以直接调用。

```go
func (g *GetProducts) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(g,
		router.WithInternalCallers("shop-user", "shop-order"),
	)
}
```

框架在 `Parse`、`Validation`、`Do` 之前执行统一授权：

| 调用路径 | 可信身份来源 | 能否访问受限路由 |
| --- | --- | --- |
| 同进程 `req.CallService` | 发起调用的 Source `ServiceContext.ServiceName()` | 名称在白名单时允许 |
| 远程 gRPC/mTLS | 已验证客户端证书 SAN，且必须等于载荷声明的 `SourceService` | 名称在白名单时允许 |
| 普通 HTTP | 无内部身份 | 拒绝 |
| 伪造 Header、请求字段或 `SourceService` | 调用方自报值不是信任来源 | 拒绝 |

因此 `SourceService` 只是待验证声明。业务 Router 不读取或写入可信身份；只有框架同进程边界或经过 mTLS 验证的 gRPC Server 可以注入。拒绝必须发生在任何参数解析和业务副作用之前。

## IRouter 请求实例

`IRouter` 是请求级对象。每次执行依次调用：

```text
对象池取得实例 -> Reset -> Parse -> Validation -> Do -> Clean -> 归还对象池
```

默认每条路由使用独立、有界的 channel pool。池为空时通过类型工厂创建，池满时丢弃归还实例，不会阻塞请求。实现了 `IRouterFactory` 时优先使用其 `New` 创建实例。

简单 Router 可使用默认反射重置。包含嵌套指针、内部缓存、锁、连接、不可清零资源或特殊零值语义时，必须实现：

- `IRouterResettable.Reset()`：从池中取出后恢复为可供下一次 `Parse` 使用的状态。
- `IRouterCleanable.Clean()`：归还前清除 token、用户信息、大缓冲区和请求级资源引用。

`Clean` 和 `Reset` 都必须幂等。异步任务与观察事件不得持有即将归还对象池的 Router；应先生成不可变快照。WebSocket 订阅不借用请求池实例，而是独立创建 Router，由 Hub 持有到退订、断线或关闭，最后调用 `Clean` 并丢弃。

## 服务级 EventBridge

每个 ServiceContext 默认创建独立的本地 `ServiceEventBridge`。RouterInfo 的 `Subscribe/UnSubscribe` 仅委托该运行时。

- 观察事件是 best-effort：没有订阅者时不构造 payload；队列满时允许丢弃并累计 dropped 指标。
- 控制事件按 `ShardKey` 固定分片串行处理，调用方同步获得失败结果。
- 控制队列入队等待默认最多 5 秒，可通过 `ServiceEventBridgeOptions.ControlEnqueueTimeout` 调整。队列持续满时返回 `ErrControlQueueTimeout`，并由 `ControlQueueTimeouts()` 累计；该事件未入队，调用方必须按控制失败处理。已入队事件不使用此超时伪装取消。
- 调用方 context 在事件入队后取消，只表示调用方不再等待，不表示撤销已入队的控制事件；worker 仍可能完成交付。
- 默认只在本服务内发布；只有显式 `External=true` 才通过 MQ adapter 外发。
- 外发控制事件要求 MQ `Usage` 包含 `event-stream`，没有外部 provider 时明确失败。

观察事件没有注册者时直接丢弃是正常语义，不能用于缓存失效、订阅状态等控制流程。

## 路由结果缓存

路由必须显式调用 `RouterInfo.UseCache(ttl)` 才启用结果缓存。缓存键按以下顺序选择：

1. `IRouterCacheKey.GetCacheKey()`
2. `IRouterHashKey.GetHashKey()` 兼容回退
3. 类型名和 JSON 值的确定性摘要

框架不会自动推断用户、租户、权限或区域维度。需要隔离时必须在 `IRouterCacheKey` 中显式加入；认证信息不得依赖进程地址或随机值。

缓存层级：

| 模式 | 行为 |
| --- | --- |
| `off` | 不自动启用路由；显式 `UseCache` 保留本地 L1 兼容行为 |
| `local` | 使用 go-zero L1，可选启用服务隔离的纯 Badger L2 |
| `shared` | Redis L3 是共享事实缓存，L1/L2 只是本地副本；失效通过 EventBridge 外部控制事件传播 |

所有 L1/L2/L3 命中都返回 `json.RawMessage`，以保证同一缓存键不会因命中层级改变类型。直接调用 `RouteCacheManager.Get/Take` 的消费方应将缓存数据视为序列化结果；需要具体 Go 类型时必须显式反序列化，不得对原始业务类型做直接断言。

共享模式启动要求 Redis Ping 成功，并且 EventBridge 外部失效订阅建立成功。默认 `OnUnavailable=fail` 阻止启动；显式 `bypass` 会关闭 L1/L2/L3 全部层后继续启动，不会退化为各节点独立缓存。

运行期 Redis 或失效发布失败时，Manager 进入 `degraded`，清空并暂停 L1/L2，业务请求继续执行但缓存旁路。调用 `Recover(ctx)` 时，只有 Redis Ping 与外部失效订阅都恢复才重新启用。当前 ServiceContext 不包含周期恢复调度；部署层应根据 `Manager.State()` 驱动恢复和 readiness，不能把 `degraded` 伪装为共享缓存健康。

Redis 配置只支持 DB 0，因为当前 go-zero Redis adapter 不消费 DB 选择；非 0 会在配置校验阶段失败。

## WebSocket

每个 ServiceContext 独占一个 `RouteWebSocketHub`。RouterInfo 的注册、注销、通知、清理和统计方法都是兼容门面，不保存连接集合。

`RouteWebSocketHub` 只服务于最终外部客户端的长连接订阅和服务端推送，不是内部服务通信方式。内部同步调用使用 `TransportSelector`（gRPC/HTTP/socket 等），内部异步事件和控制使用 `ServiceEventBridge` 及显式配置的 MQ adapter。跨节点 WebSocket Notice 也只是将外部订阅者的通知转发到拥有该订阅的节点；节点之间实际使用 EventBridge/MQ/Transport，不建立服务间 WebSocket。

- 订阅按完整业务 hash 隔离，即使两个 hash 落在同一分片也不会串消息。
- 同一客户端可订阅多个 hash；重复注册和重复注销幂等。
- `RegisterWebSocketClient` 只接收通过 `NewSubscription/ParseSubscription` 独立创建的 Router；成功后由 Hub 接管，调用方不得自行清理。每个客户退订时清理自己的实例，canonical Router 保留到该 hash 最后一个客户退订；注册失败和 Hub 关闭也会执行 `Clean`。订阅实例始终丢弃，不进入请求对象池。
- 认证 WebSocket 订阅必须实现 `IWebSocketUserIdentity`。传输层只从已认证会话注入用户身份；缺少该接口时 fail closed，订阅 payload 中的用户字段不会作为兼容回退。
- 0 到 1、1 到 0 的订阅变化通过 ServiceEventBridge 控制事件处理。
- 本地通知不要求 MQ；跨节点通知要求服务级 CrossNode forwarder 和可用的外部控制通道。
- 旧 `WebSocketNotificationSystem`、`StartPeriodicCleanup` 和 `StopPeriodicCleanup` 仅保留无状态兼容入口，新代码不得使用。

进程级 `SetCrossNodeForwarder/GetCrossNodeForwarder` 同样仅为旧调用方保留。服务级 Hub 只查询 `SetCrossNodeForwarderForService` 注册值，不回退到全局值。

## 生命周期顺序

ServiceContext 创建顺序：本地 EventBridge、WebSocket Hub、Cluster/Transport、MQ 外部 adapter、RouteCacheManager、路由注册。

关闭顺序：停止 WebSocket Hub、关闭 RouteCacheManager、关闭 ServiceEventBridge、停止跨节点 broker 和 membership、关闭 MQManager，最后从 ServiceContext registry 注销。

默认单元测试不连接 Redis、NATS 或其他 Docker 服务。真实 Redis L3 测试必须显式运行：

```bash
CORE_TEST_REDIS=1 ./scripts/test.sh integration
```

## 日志与安全

路由、缓存、事件和 WebSocket 日志只记录稳定事件名、service、route、状态、计数和错误，不记录请求/响应正文、缓存值、WebSocket payload、token、密码或 Redis 密码。逐请求缓存旁路不应重复打印错误；状态转换由拥有恢复或降级决策的边界记录。
