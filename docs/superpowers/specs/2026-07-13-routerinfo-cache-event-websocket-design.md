# RouterInfo 缓存、事件与 WebSocket 解耦设计

> 状态：已完成方案确认，等待书面规格复核
> 日期：2026-07-13
> 范围：`pkg/server/types`、`pkg/server/event`、`pkg/server/router/ServiceContext`

## 1. 背景与目标

当前 `RouterInfo` 同时承担路由元数据、请求执行、对象池、观察回调、结果缓存、WebSocket 订阅、通知调度、跨节点转发、统计与清理职责。职责耦合已经引出以下真实问题：

- WebSocket 只按 `hash % 128` 选择分片，发送时没有再次按完整 hash 隔离，不同订阅可能串消息。
- 请求观察回调异步持有即将被清理的路由对象，订阅表也存在并发读写。
- 当前缓存条目被原地并发修改，过期条目缺少主动回收，缓存键编码存在歧义。
- WebSocket 死连接清理、订阅计数、跨节点状态与组件销毁没有统一生命周期。
- 本地事件流依赖 MQ 配置才创建，无法作为每个服务稳定存在的内部事件骨架。

本设计将 `RouterInfo` 收敛为路由描述和执行编排对象，并建立三个边界清晰、由 `ServiceContext` 持有的服务级组件：

1. `RouteCacheManager`：路由结果的 L1/L2/L3 分层缓存与一致性控制。
2. `ServiceEventBridge`：默认本地事件总线，以及可选的外部可靠事件适配。
3. `RouteWebSocketHub`：精确订阅、连接生命周期、本地过滤与有界发送。

本次不重新实现 Redis、Badger、消息队列、重试或连接池。优先复用 go-zero 和项目已有成熟依赖，只保留框架需要的组装与领域契约。

## 2. 已选方案

### 2.1 方案比较

评估过三种方案：

1. **服务级管理器，由 RouterInfo 委托（采用）**：每个 `ServiceContext` 创建独立缓存、事件和 WebSocket 组件；RouterInfo 只保存轻量句柄。隔离清晰，生命周期可控，兼容现有公开方法。
2. **继续在 RouterInfo 内实现分层能力（拒绝）**：改动较小，但会继续扩大 RouterInfo，并使跨路由容量、连接和事件顺序难以统一治理。
3. **建立全局插件系统（拒绝）**：扩展性最高，但当前没有足够插件需求，生命周期和配置复杂度明显超过收益。

### 2.2 所有权关系

```text
ServiceContext
├── ServiceEventBridge     每个服务始终创建
├── RouteCacheManager      按服务配置创建
├── RouteWebSocketHub      有 WebSocket 路由时惰性创建
└── ServiceRouter
    └── RouterInfo[]
        ├── 路由元数据
        ├── 请求执行编排
        └── 对上述组件的兼容委托
```

组件不得使用进程级可变全局单例承载服务状态。进程级 shutdown hook 可以存在，但只能调用各 `ServiceContext` 的显式关闭方法。

## 3. RouterInfo 职责收敛

### 3.1 保留职责

- 路由路径、认证、方法、类型和实例工厂等元数据。
- `Parse -> Validation -> Do -> Response` 执行编排。
- 将缓存查询、观察事件、WebSocket 操作委托给服务级组件。
- 保留现有公开方法作为兼容门面，避免消费方一次性迁移。

### 3.2 移出职责

- `sync.Map` 路由结果缓存及 TTL 处理。
- `Subscriber` 回调表及裸 goroutine 通知。
- WebSocket 分片、订阅参数、客户端计数、通知 worker 和周期清理。
- 与跨节点 forwarder 的直接调用。
- 进程级 WebSocket 可变全局状态。

### 3.3 请求执行安全边界

`Exec` 是唯一 panic 恢复边界。`ExecDo` 不得吞掉 panic 后返回 nil；任何 panic 都转换成框架类型化错误响应，并遵守公开错误脱敏契约。

观察事件不得持有池化的 `IRouter`、`IRequest` 或可变 `IResponse`。事件发布前生成不可变快照；未注册观察者时，在构造或序列化快照之前直接返回。

`RouterInfo` 是所属 `ServiceContext` 内每条路由唯一的长期元数据对象，不进入请求级对象池。`IRouter` 是每次请求使用的业务实例，在高并发路径会被大量创建，因此保留每个 RouterInfo 独立的有界对象池。当前 channel pool 与 sync.Pool 没有形成同一条回收链，实施时统一为一个 `ChannelPool`：创建、获取、清理和归还都必须经过该池，不再保留未被读取的 sync.Pool。

简单 Router 默认使用通用反射重置；实现了 `IRouterResettable` 的复杂 Router 必须由 `Reset()` 完整恢复到可供下一次 `Parse` 使用的状态，并跳过通用反射重置。实现了 `IRouterCleanable` 的 Router 在归还前调用 `Clean()` 清除敏感数据或释放请求级引用。`IRouterFactory` 负责自定义实例创建，但最终创建出的请求实例仍由该 RouterInfo 的对象池管理。任何观察事件、WebSocket 订阅或异步任务都不得持有即将归还对象池的 Router；必须先生成不可变快照，完成同步使用后才能归还。

RouterInfo 只允许在 ServiceContext 范围内按路由唯一，不允许成为跨服务共享的进程级可变单例。进程级 ServiceContext registry 仅作为兼容查找入口：同名活动实例可以复用；配置冲突必须明确失败；已终止实例在资源关闭后必须注销，不能在下一次创建时返回失效上下文。

## 4. ServiceEventBridge

### 4.1 默认启动

每个 `ServiceContext` 创建时都初始化独立的 `ServiceEventBridge`，无论是否配置 MQ。MQ 只决定事件是否可以显式外发，不决定本地事件能力是否存在。

`ServiceEventBridge` 对内提供两类通道：

- **观察事件**：请求、响应、错误、缓存命中等非控制信息。best-effort、有界队列；无订阅者立即丢弃；队列满时允许丢弃并记录聚合指标，不逐条刷错误日志。
- **控制事件**：缓存失效、WebSocket 订阅激活/失活、跨节点通知等影响一致性的事件。必须有序、可确认、失败可观测，不允许静默丢弃。

### 4.2 本地与外部语义

- 默认事件只在当前服务实例内发布。
- 只有事件定义或调用方显式声明为外部事件时，才经过 MQ/EventBridge adapter 外发。
- 未配置外部 Provider 时，本地观察事件正常工作；要求外发的控制事件必须返回明确错误，不能伪装成功。
- 观察事件没有注册者时直接丢弃，不构造 payload。
- 控制事件按稳定聚合键串行，例如 `service + route + cache-key` 或 `service + route + websocket-hash`，保证同一实体的先后顺序。

### 4.3 事件快照

路由观察事件使用框架定义的安全 DTO，只包含允许公开的字段，例如 service、route、trace ID、阶段、耗时、公开错误码。默认不包含 token、请求体、响应体、完整路由对象或内部错误文本。

现有 `Subscribe/UnSubscribe` 暂时保留为兼容门面，内部转换为 EventBridge subscription，并返回幂等取消函数。回调 panic 必须隔离，不能中断发布循环。

## 5. RouteCacheManager

### 5.1 分层模型

- **L1 内存**：使用 go-zero `collection.Cache`，负责小容量热点、TTL、容量限制和淘汰。
- **L2 Badger**：使用独立的纯缓存适配器，扩大本机容量并支持进程重启后的短期复用。不得复用 `PrefixedBadgerDB` 的远程同步队列、pending 计数或 write-behind 语义。
- **L3 Redis**：水平扩展时作为共享事实缓存，并承担跨节点失效协调。使用 go-zero 已有 Redis 能力或当前项目成熟 Redis 客户端，不自建连接池和重试器。

读取顺序为 L1 -> L2 -> L3 -> 业务执行。下层命中后按配置回填上层。写入顺序以 L3 共享事实为优先，再写 L2/L1；单机模式没有 L3 时写 L2/L1。

### 5.2 模式与降级

- **单机模式**：允许只启用 L1，或启用 L1+L2。
- **共享严格模式**：启用水平扩展时必须配置 Redis。启动时 Redis 不可用默认启动失败。
- **显式旁路**：配置允许在 Redis 缺失时禁用整个路由缓存并继续启动，但不得静默退化为每节点独立缓存。
- **运行期 Redis 故障**：服务保持存活，清空并暂停 L1/L2，所有请求旁路缓存；readiness 标记降级，liveness 保持正常。
- **恢复条件**：Redis 连接和失效订阅都恢复后，才允许重新启用缓存，避免恢复窗口读到旧的 L1/L2 数据。

### 5.3 缓存键

新增 `IRouterCacheKey`，由业务在需要时提供稳定、无敏感信息的缓存键。兼容顺序为：

1. `IRouterCacheKey`
2. 既有 `IRouterHashKey`
3. 框架确定性字段编码

框架默认编码必须包含字段名、类型、长度或结构化边界，禁止直接拼接字段值。用户身份或租户是否进入键完全由消费方的 `IRouterCacheKey` 决定，框架不额外推断。

### 5.4 一致性与保护

- 同一完整缓存键使用 `singleflight` 抑制击穿。
- TTL 加有限随机抖动，避免大量条目同时过期。
- 负缓存默认关闭，需要路由显式启用。
- L1/L2 必须有容量上限；过期条目由底层实现主动回收。
- 失效先处理共享事实，再发布可靠控制事件清理各节点 L1/L2。
- 控制事件处理必须幂等，允许重复交付。

现有 `UseCache`、`FailureCache` 保留为兼容门面，内部委托 `RouteCacheManager`。不改变默认“不启用缓存”的行为。

## 6. RouteWebSocketHub

### 6.1 精确订阅模型

WebSocket 分片只用于降低锁竞争，不表示订阅边界。每个分片内部按完整 hash 保存订阅组：

```text
shard[hash % N]
└── subscriptions[fullHash]
    ├── router snapshot
    └── clients[client] = request/session metadata
```

发送、注销、清理和统计都必须先定位分片，再定位完整 hash。任何两个不同 hash 即使落入同一分片也不能共享客户端集合。

同一客户端可以订阅多个 hash；同一客户端重复注册同一 hash 必须幂等，不重复增加计数或发布 active 事件。只有订阅组从 0 变 1 时发布 active，从 1 变 0 时发布 inactive。

### 6.2 文件与类型边界

第一阶段保持在 `pkg/server/types` 包内，避免引入包循环，但建立独立类型和聚焦文件：

- `route_websocket_hub.go`：公开组件、注册、注销和兼容门面。
- `route_websocket_shard.go`：精确 hash 分片数据结构。
- `route_websocket_delivery.go`：过滤、队列、批量发送和背压。
- `route_websocket_lifecycle.go`：清理、关闭、订阅状态变更。
- `route_websocket_stats.go`：服务级指标快照。

RouterInfo 的现有 WebSocket 方法继续存在，但只委托给 Hub。

### 6.3 发送与背压

- 每个服务拥有独立有界队列和 worker 配额，避免一个服务耗尽其他服务资源。
- 队列满时，本地普通通知可以按明确策略拒绝并返回可观测结果；控制通知不得静默丢弃。
- 不再为每个客户端和每个过滤器无限创建 goroutine。
- 新增可选的 context-aware 过滤和发送接口；旧接口通过受限兼容执行器运行。
- 超时只在底层接口支持取消时表示任务已停止；旧接口超时只能标记为超时并占用固定执行槽，避免无限泄漏。
- 通知 payload 在入队前冻结或序列化，调用方后续修改不得影响已排队消息。

### 6.4 生命周期与跨节点

- 死连接清理必须走统一注销路径，同时更新客户端集合、订阅计数和 `rArgs` 等兼容视图。
- `RouteWebSocketHub.Close(ctx)` 停止接收新任务、完成或终止有界队列、注销剩余订阅并释放资源。
- `RouterInfo.Destroy` 仅执行幂等委托；未初始化 WebSocket 时安全返回。
- 订阅 active/inactive 和跨节点 Notice 通过 `ServiceEventBridge` 控制事件处理，不再直接启动 goroutine 调用全局 forwarder。
- 未配置外部 Provider 时，本地 WebSocket 正常工作；显式跨节点操作返回明确不可用错误。

## 7. 兼容性策略

以下现有公开入口第一阶段保持签名和基本行为：

- `RouterInfo.UseCache`
- `RouterInfo.FailureCache`
- `RouterInfo.Subscribe/UnSubscribe`
- `RegisterWebSocketClient`
- `UnRegisterWebSocketClient/UnRegisterWebSocketHash`
- `NoticeWebSocket`
- `ExecuteLocalNotice`
- `GetSubscribedHashes`
- `Destroy`

新增接口采用可选能力检测，不强制现有消费方立即实现。任何公共 API、配置、JSON、路由或运行时语义变化都必须通过 `release-contract`，并登记到兼容性和弃用文档。

第一阶段不删除现有门面；旧内部实现只有在新组件回归通过后才删除。已注释的旧 WebSocket 大段代码不属于兼容表面，应直接清理。

## 8. 错误处理与可观测性

- 配置错误和共享严格模式缺少 Redis：启动失败，并给出稳定事件名和非敏感字段。
- Redis 运行期故障：进入 cache bypass，readiness 降级，记录状态转换而不是逐请求报错。
- 观察事件队列满：聚合 dropped 指标，限频日志。
- 控制事件发布失败：向调用方返回错误，并暴露 pending/failed 指标。
- WebSocket 过滤、发送、注册、注销 panic：隔离、计数、结构化日志，不带 payload。
- 指标至少覆盖缓存各层命中、旁路、失效延迟、队列深度、事件丢弃、活跃订阅、发送失败和关闭耗时。

日志遵守 `LOGGING_AUDIT_AND_STANDARD.md`，禁止打印 token、请求/响应正文、WebSocket payload、完整对象或内部错误堆栈到普通业务日志。堆栈只用于受控的 panic 日志。

## 9. 测试与验收

### 9.1 RouterInfo 与事件

- panic 始终返回非 nil 的安全类型化响应。
- 无观察者时不构造事件快照。
- 异步回调读取不可变快照，不受路由对象清理影响。
- 并发订阅、取消和发布通过 race 测试。
- 观察事件队列满可丢弃；控制事件不静默丢弃且保持同键顺序。

### 9.2 缓存

- L1 TTL、容量淘汰、singleflight 和确定性键测试。
- L2 过期、容量、重启和损坏隔离测试，不产生远程同步队列。
- L3 共享命中和 EventBridge 失效测试。
- Redis 启动缺失、运行期中断、旁路、订阅恢复后重新启用测试。
- 多节点测试证明失效后不继续返回旧 L1/L2 数据。

### 9.3 WebSocket

- 两个满足 `hashA % 128 == hashB % 128` 的不同 hash 不串消息。
- 同一客户端多 hash 订阅互不覆盖。
- 重复注册和重复注销幂等，计数不漂移。
- 死连接清理同步移除订阅组并只发布一次 inactive。
- 快速 active/inactive 控制事件保持顺序。
- 有界队列、慢客户端、过滤超时和关闭过程不产生无界 goroutine。
- 两个 ServiceContext 的队列、统计和生命周期相互隔离。
- 本地模式不要求 Redis/MQ；跨节点显式开启后验证外部控制事件。

### 9.4 门禁

每个实施小节先写失败测试，再做最小实现，并至少运行：

```bash
go test ./pkg/server/types ./pkg/server/event ./pkg/server/router -count=1
go test -race ./pkg/server/types ./pkg/server/event ./pkg/server/router -count=1
./scripts/check-logging.sh
./scripts/test.sh release-contract
```

涉及 Redis/Badger 多节点行为的测试使用显式 integration gate 和 Docker Compose，默认单元测试不得依赖外部服务。

## 10. 分阶段交付边界

该设计按以下顺序实施，每节单独提交、测试并接受外部审查：

1. P0 回归测试与 RouterInfo panic/事件快照修复。
2. 每服务 `ServiceEventBridge` 及观察/控制事件语义。
3. `RouteWebSocketHub` 精确 hash 模型与生命周期抽离。
4. `RouteCacheManager` L1 与缓存键契约。
5. L2 Badger 纯缓存适配器。
6. L3 Redis、可靠失效与严格共享降级。
7. 兼容门面迁移、对象池和旧代码清理、统计与文档收口。

任务不得在同一提交同时重写三大组件。每节只有在定向测试、race、日志门禁和外部审查通过后才能进入下一节。

## 11. 非目标

- 不把 `PrefixedBadgerDB` 改造成 RouterInfo 缓存。
- 不内建新的 Redis、NATS、Kafka 或 JetStream 客户端。
- 不自动推断用户、租户或权限是否应进入缓存键。
- 不保证普通观察事件可靠投递。
- 不在没有外部 Provider 时伪造跨节点一致性。
- 不在第一阶段删除现有公共 RouterInfo 门面。
- 不以重试、sleep 或扩大无界队列掩盖并发与生命周期问题。

## 12. 完成定义

- RouterInfo 不再直接拥有缓存条目、观察者 map、WebSocket 分片或全局通知 worker。
- WebSocket 对完整 hash 精确隔离，重复注册、清理和关闭保持一致。
- 每个 ServiceContext 默认拥有可工作的本地 EventBridge。
- 单机缓存无需 Redis；共享严格模式不能静默退化为节点本地缓存。
- L1/L2/L3 的容量、TTL、失效、故障和恢复行为有确定性测试。
- 现有公共入口保持兼容，新增行为和配置有中文文档。
- 定向测试、race、日志检查和发布契约全部通过，并获得外部审查批准。
