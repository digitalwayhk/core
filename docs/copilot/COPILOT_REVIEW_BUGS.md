# Copilot Review Bugs

本文件记录 Codex 对 `codex/optimize-code-cleanup` 当前实现的复查结果。目标是让 Copilot 继续修复实现和补齐测试。

## P0: Cluster / Transport / MQ 没有接入服务启动链路

### 现象

Phase 3-6 新增了 cluster provider、ProviderSwitcher、transport selector、MQ manager、WebSocket CrossNodeNoticeBroker 等类型，但 `ServiceContext` 启动流程没有根据 `ServerConfig.Cluster` / `ServerConfig.Transport` / `ServerConfig.MQ` 初始化这些能力。

当前可见问题：

- `ServiceContext.ClusterProvider` 只定义字段，没有初始化赋值；`ClusterStatus` / `ClusterNodes` 会长期返回 provider none 或空节点。
- `ServiceContext.membership` 没有创建和启动，节点不会向 provider 注册、心跳、下线。
- `ServiceContext.TransportSelector` 没有根据配置创建，`sendPayload` 仍然只在外部手动赋值 selector 且 payload 已指定地址时才使用新 transport。
- `MQManager`、`RedisStreamProvider`、`NATSJetStreamProvider` 没有根据 `ServerConfig.MQ` 初始化，也没有作为 event-stream / internal transport provider 接入服务间通讯。
- `CrossNodeNoticeBroker` 依赖 `ClusterProvider != nil` 才会在 `SetRunState(true)` 初始化，因此当前跨节点 WebSocket 通知默认不会启用。

### 定位

- `pkg/server/router/servicecontext.go:45-48` 定义了 `TransportSelector`、`ClusterProvider`、`membership`、`CrossNodeBroker`
- `pkg/server/router/servicecontext.go:195-202` 只执行本地 MachineID claim，没有创建配置指定的 provider
- `pkg/server/router/servicecontext.go:286-296` 只有在 `ClusterProvider != nil` 时才创建 `CrossNodeNoticeBroker`
- `pkg/server/router/servicecontext.go:622-630` 只在 `TransportSelector != nil && payload.TargetAddress != ""` 时使用新 transport
- `pkg/server/api/manage/clustermanage.go:24-37` provider 为空时直接返回 none
- `pkg/server/mq/manager.go:16-71` 只是手动注册/切换 provider，没有配置接线

### 期望修复

- 在服务启动时根据 `ServerConfig.Cluster.Mode` / `Provider` 创建 provider：
  - `off`: 不进入 cluster 注册发现，仅保留现有单机行为。
  - `auto`: 默认使用 local provider；如配置 etcd/consul 且连接可用则使用外部 provider，连接失败应降级或明确记录。
  - `on`: 强制使用配置 provider，外部 provider 缺配置或连接失败应启动失败。
- 注册当前服务实例并启动 `MembershipManager`，停止服务时 deregister/drain。
- 根据 `ServerConfig.Transport` 创建统一 `TransportSelector`，服务发现拿到节点后才能按 HTTP / Socket / QUIC / gRPC / MQ 选择内部传输。
- 根据 `ServerConfig.MQ` 创建 `MQManager`，默认开发配置为 Redis Streams；NATS JetStream 等生产 provider 可通过配置切换，不改业务代码。
- ProviderSwitcher / MQ Switcher 的管理接口必须真的操作当前 `ServiceContext` 上的 switcher，不应只返回 scheduled placeholder。

### 必补测试

- 新增服务启动级单元或集成测试：构造 `ServiceContext` 后，`Cluster.Mode=auto` 时 `ClusterProvider` 非空，当前服务节点可在 `List(serviceName, running)` 查到。
- 新增管理 API 测试：`ClusterStatus` 能返回真实 provider 和当前节点。
- 新增 MQ 配置接线测试：`MQ.Mode=auto/on` 时 manager 当前 provider 与配置一致；`on` 缺少必需连接配置时返回错误。
- 新增 transport selector 接线测试：配置启用 gRPC/socket/http fallback 后，`ServiceContext.TransportSelector` 非空。

## P1: LocalProvider 的 MachineID slot 冲突没有按 ServiceName 隔离

### 现象

需求约定实例身份应以 `types.IService.ServiceName()` 为服务域，再结合 `DataCenterID + MachineID` 确认同一服务下的一个实例。也就是说：

- 同一服务的两个运行实例不能占用同一 `DataCenterID + MachineID`。
- 不同服务应允许复用相同的 `DataCenterID + MachineID`，因为 Snowflake 只要求同一服务实例域内避免冲突，服务发现和水平扩展也按服务名取节点。

当前 `LocalProvider` 在全局所有节点范围内检查 `DataCenterID + MachineID`，没有判断 `ServiceName`，会导致 `funds` 和 `orders` 这类不同服务无法同时使用同一配置 slot，Docker 自动扩展也会被错误挤占 MachineID。

### 定位

- `pkg/server/cluster/provider_local.go:63-89` 注册冲突检查只比较 `DataCenterID` 和 `MachineID`
- `pkg/server/cluster/provider_local.go:251-269` `AllocateMachineID(dataCenterID)` 只按数据中心统计 used，没有传入或筛选 `serviceName`
- `pkg/server/router/servicecontext.go:657-664` 自动 claim 调用 `processLocalRegistry.AllocateMachineID(dc)`，同样缺少服务名

### 期望修复

- `LocalProvider.Register` 冲突判断应增加 `existing.ServiceName == node.ServiceName`。
- `LocalProvider.AllocateMachineID` 应改为 `AllocateMachineID(serviceName string, dataCenterID int64, maxMachineID ...int64)` 或同等形式，并只统计该服务下 running 节点。
- `claimMachineID` 应传入 `serviceName`，并尊重 `con.Cluster.Claim.MachineIDMax`，不要写死 1023。
- etcd / Consul 已经通过 `List(ctx, serviceName, running)` 做了服务级隔离，LocalProvider 行为需要保持一致。

### 必补测试

- `LocalProvider` 注册 `funds(dc=0,machine=1)` 后，再注册 `orders(dc=0,machine=1)` 应成功。
- 同一服务 `funds` 注册第二个 `dc=0,machine=1` 仍应返回 `ErrSlotConflict`。
- `AllocateMachineID("orders", 0)` 不应被 `funds` 的 MachineID 占用影响。
- 自动 claim 覆盖 `MachineIDMax` 边界：超过上限时返回明确错误或按配置策略扩展 DataCenterID。

## P1: WebSocket 跨节点订阅注销和下线摘要不可靠

### 现象

跨节点 WebSocket 推送依赖每个节点维护 `routePath + hash -> local clients`，并通过订阅摘要同步给其它节点。当前注销和下线逻辑存在两个问题：

1. `UnRegisterWebSocketHash` 判断某个 hash 是否已经没有本地 client 时，统计的是所有分片内的全部 client 数量，而不是目标 hash 的 client 数量。只要同一路由还有其它 hash 的 client，就不会删除当前 hash 的 `rArgs`，也不会发送 `active=false` 摘要。
2. `CrossNodeNoticeBroker.DrainAndStop` 先把 `stopped=true`，随后调用 `OnSubscriptionChange(..., false)`，但 `OnSubscriptionChange` 看到 stopped 会立即 return，导致下线摘要不会发出。

### 定位

- `pkg/server/types/websocketshard.go:96-113` `totalCount += len(s.clients)` 统计了全部 client，不是当前 hash
- `pkg/server/types/websocketshard.go:133-138` 只有 `needUnregister` 为 true 时才发 `active=false`
- `pkg/server/cluster/event.go:147-170` `DrainAndStop` 先设置 stopped，再调用 `OnSubscriptionChange`

### 期望修复

- `websocketShard.clients` 当前是 `map[IWebSocket]IRequest`，无法直接按 hash 统计。修复时可：
  - 增加 hash 到 client set 的索引；或
  - 在每个 hash 对应的 shard 中只统计该 hash 的 client；或
  - 将 client 记录结构扩展为包含 hash。
- 当某个 `routePath + hash` 最后一个本地 client 注销时，必须删除本地订阅并广播 `active=false`。
- `DrainAndStop` 应在标记停止前收集本地订阅并广播 removal，或允许 drain 阶段继续发送 removal 摘要。
- `CrossNodeNoticeBroker` 需要明确区分本地订阅索引和 peer 订阅摘要；下线时应发送本节点本地订阅的 removal，不应依赖 peer `subs`。

### 必补测试

- 同一路由注册两个不同 hash 的 client，注销其中一个 hash 的最后 client，应触发一次 `OnSubscriptionChange(active=false)`。
- 注销 hash A 不应删除 hash B，也不应阻止 hash B 收消息。
- `DrainAndStop` 对本地已注册订阅应向 peer 发送 removal 摘要。
- 集成测试不能只直接调用 `OnSubscriptionChange` / `ForwardNotice`，需要覆盖 `RegisterWebSocketClient` 和 `UnRegisterWebSocketHash` 的真实路径。

## P2: ProviderSwitcher 当前实现无法迁移 local provider 中的节点

### 现象

`clusterSwitcher.Begin` 试图通过 `s.current.List(ctx, "")` 复制当前 provider 的全部节点到新 provider。但 `LocalProvider.List` 会按 `serviceName` 精确过滤，传空字符串只返回 serviceName 为空的节点，正常服务节点不会被复制。

这会让 local -> etcd/consul 的 warm-up 迁移阶段基本没有效果。

### 定位

- `pkg/server/cluster/switcher.go:44-54` 使用 `List(ctx, "")`
- `pkg/server/cluster/provider_local.go:141-164` `List` 中 `if n.ServiceName != serviceName { continue }`

### 期望修复

- ProviderSwitcher 迁移时应按当前已知服务名列表逐个 `List(serviceName)`，或为 provider 增加 `ListAll` 能力。
- 管理接口触发切换时应限定服务范围，避免误迁移其它服务。

## 当前验证结果

已通过的针对性测试：

```bash
go test ./pkg/server/config ./pkg/server/cluster ./pkg/server/transport/... ./pkg/server/mq ./pkg/server/event ./pkg/server/types ./pkg/persistence/types
```

这些测试目前偏包级能力验证，未覆盖服务启动接线、真实管理 API 返回、真实 WebSocket 注册/注销路径，因此不足以证明 Phase 3-6 已完整落地。
