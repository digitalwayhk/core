# Copilot Test Coverage Bugs

本文件记录 Codex 对当前测试用例完善程度的复查结果。当前测试数量已经增加，但整体仍偏“包级函数测试”，对服务启动链路、管理 API、跨节点实际行为、前后端配合的回归保护还不够。

## 当前测试覆盖概况

- 当前仓库约有 42 个 Go 测试文件，新增能力相关测试集中在：
  - `pkg/server/cluster/*_test.go`
  - `pkg/server/transport/*_test.go`
  - `pkg/server/mq/*_test.go`
  - `pkg/server/types/websocketshard_test.go`
  - `tests/integration/*`
- 前端仅发现登录页测试：`web/admin/src/pages/user/login/login.test.tsx`。
- `service/manage` 和 `web/admin/src/components/WayPlus` 暂无针对核心管理界面生成、查询、增删改配合的测试。

## P0: 缺少 ServiceContext 启动生命周期测试

### 现状

当前测试覆盖了 `cluster.BuildProvider`、`mq.BuildManager`、`transport.BuildSelector` 等包级函数，但没有直接覆盖 `router.NewServiceContext` 和 `SetRunState` 的完整启动行为。

因此以下高风险行为缺少回归保护：

- `Cluster.Mode=auto` 时是否真的初始化 `ClusterProvider`。
- `Cluster.Mode=on` 初始化失败是否在 `NewServiceContext` 层阻止启动。
- `MQ.Mode=on` 初始化失败是否在 `NewServiceContext` 层阻止启动。
- `SetRunState(true)` 是否注册当前节点并启动 membership heartbeat。
- `SetRunState(false)` 是否 drain WebSocket broker、停止 membership 并 deregister。
- `TransportSelector` 是否在服务上下文中按配置生效。

### 需要补充

- 新增 `pkg/server/router/servicecontext_test.go`。
- 用最小 `types.IService` fake service 构造 `ServiceContext`。
- 覆盖 `Mode=auto`、`Mode=off`、`Mode=on` 失败路径。
- 断言 `ClusterProvider`、`ClusterSwitcher`、`MQManager`、`TransportSelector` 字段状态。
- 断言 `SetRunState(true/false)` 后 provider 中节点状态变化。

## P0: 缺少 ClusterSwitchProvider 管理 API 调用链测试

### 现状

`pkg/server/cluster/switcher_test.go` 只测试 `clusterSwitcher` 本身：

- `Complete` 后 `switcher.Current()` 是否切换。
- `Begin` 是否按 serviceName warm-up。

但没有测试 `pkg/server/api/manage/clustermanage.go` 中的 `ClusterSwitchProvider.Do`。这意味着以下实际 API 行为没有保护：

- `begin -> complete` 后是否调用 `ServiceContext.SyncProviderAfterSwitch()`。
- `ServiceContext.ClusterProvider` 是否同步到新 provider。
- 服务运行中切换时 membership 和 CrossNode broker 是否重建。
- `ClusterStatus` / `ClusterNodes` 是否返回切换后的 provider。

### 需要补充

- 为 `ClusterSwitchProvider.Do` 增加单元测试或 integration 测试。
- 通过 fake request 返回 `ServiceName()`，使用 `router.GetContext` 获取上下文。
- 覆盖 `begin`、`complete`、`rollback` 三个 action。
- 断言 API 返回后 `sc.ClusterProvider == sc.ClusterSwitcher.Current()`。
- 断言 `ClusterStatus.Do` 返回的 provider name 已更新。

## P1: Transport quic/mq 配置缺口没有测试保护

### 现状

`pkg/server/config/transportconfig.go` 将 `quic`、`mq` 作为合法值，但 `pkg/server/transport/factory.go` 当前只构建 `grpc`、`http`、`socket`。测试只覆盖：

- `Internal=http`
- `Internal=grpc` + `Fallback=http,socket`
- 空配置

没有测试：

- `Internal=quic`
- `Internal=mq`
- `Fallback=["mq","grpc","http"]`
- 只有未实现协议时是否返回明确错误

这会让 `quic/mq` 被静默跳过的问题继续存在或反复回归。

### 需要补充

- 在 `pkg/server/transport/factory_test.go` 增加 quic/mq 测试。
- 如果实现 adapter：断言 primary/fallback 中包含 `quic` / `mq`。
- 如果暂未实现：断言构建返回明确错误，不能返回 nil 后让服务静默 fallback 到旧 HTTP。
- 覆盖 fallback 顺序保留，不允许无提示删掉显式配置项。

## P1: WebSocket 跨节点测试仍偏 hook 级，缺少真实转发断言

### 现状

`tests/integration/websocket_cluster_test.go` 已覆盖注册/注销 hook，`pkg/server/types/websocketshard_test.go` 已覆盖重复注销。但跨节点 broker 的真实行为仍较弱：

- `TestCrossNodeBrokerRegistration` 只是调用 `ForwardNotice` 并确认不 panic，没有断言 peer 收到 HTTP notice。
- `TestCrossNodeBrokerDrainAndStop` 只调用 `DrainAndStop`，没有断言 removal 摘要真的发给 peer。
- 没有通过 `httptest.Server` 模拟 peer 节点的 `/api/servermanage/ws/notice` 和 `/api/servermanage/ws/subscription`。
- 没有验证 `routePath + hash -> nodeID` 摘要索引在 register、unregister、drain 后的完整生命周期。

### 需要补充

- 使用 `httptest.Server` 注册两个 fake peer 节点。
- 验证 `OnSubscriptionChange(active=true)` 会 POST 到 peer subscription endpoint。
- 验证 `ForwardNotice` 只向订阅了对应 `routePath + hash` 的 peer 发送 notice。
- 验证 `DrainAndStop` 会对所有 local subscription 发送 `active=false`。
- 验证非匹配 hash / routePath 不会被转发。

## P1: MQ / event-stream 缺少 provider 级和集成语义测试

### 现状

当前 MQ 测试主要覆盖：

- `MQManager` 注册、current、health、publish 委托。
- `BuildManager` 在 provider 错误时 on/auto 的行为。
- `Switcher` 的阶段跳转。

但缺少：

- Redis Stream provider publish/subscribe/ack 的 integration 测试。
- NATS JetStream provider publish/subscribe/ack 的 integration 测试。
- request/reply、retry、dead-letter、failure callback 的行为测试。
- event-stream 使用 MQ provider 的集成测试。
- MQ 作为 internal transport 时的 request/response 测试。

### 需要补充

- 增加带环境变量开关的 Redis Stream integration 测试，例如 `CORE_TEST_REDIS_STREAM`。
- 增加带环境变量开关的 NATS JetStream integration 测试，例如 `CORE_TEST_NATS`。
- 对 provider 统一测试 contract：publish -> subscribe -> ack -> health。
- 对 switcher 增加 double-write 失败 rollback、complete drain/cancel、read-new 后旧 provider 关闭的测试。
- 增加 event-stream 通过 MQ provider 发布和消费的测试。

## P1: 服务分片筛选只有纯函数测试，未覆盖服务发现和调用路径

### 现状

`pkg/server/cluster/shard_test.go` 覆盖了 `ServiceShardRouter.Filter` 的纯函数行为，但没有覆盖配置驱动的实际调用路径。

缺少：

- `Cluster.Services` 配置到 `ServiceShardRouter` 的构建测试。
- `userid` / `accountTypeid` 等筛选标记如何从 request/payload 进入服务发现选择的测试。
- 无筛选标记时默认平均分布。
- 指定 shard key 时只访问目标服务实例集合。
- 找不到候选实例时的 fallback / error 策略。

### 需要补充

- 增加 shard config factory 测试。
- 增加服务发现 + balancer + shard router 的组合测试。
- 用 `funds` 服务示例覆盖：
  - `userid` hash 到固定一台。
  - `accountTypeid` 在指定两台中平均分布。
  - 无标记时全量平均分布。

## P1: service/manage 和 WayPlus 前后端配合没有测试

### 现状

`service/manage` 是当前框架“极少代码完成管理界面增删改查”的核心，但没有 Go 测试覆盖：

- View/schema 生成。
- Search/Add/Edit/Remove/Submit/Release 标准路由。
- DoBefore/DoAfter/SearchBefore/SearchAfter hook。
- 字段、枚举、表单配置到前端 schema 的兼容性。

`web/admin/src/components/WayPlus` 也没有组件测试，只有登录页测试。这导致后端 manage schema 或前端 WayPlus 组件变更时，很容易破坏管理界面而测试不报警。

### 需要补充

- 为 `service/manage` 增加最小模型的 CRUD contract tests。
- 为 hook 顺序和错误传播增加测试。
- 为 `web/admin/src/components/WayPlus/WayPage`、`WayTable`、`WayForm` 增加组件测试。
- 增加前后端 schema contract fixture：后端输出一份 view/search/add/edit schema，前端用 fixture 渲染并断言关键字段、按钮、表格列存在。

## P2: 外部 provider integration 测试默认全部跳过，CI 价值有限

### 现状

`tests/integration/etcd_provider_test.go`、`consul_provider_test.go`、`cluster_local_test.go` 都依赖环境变量开关。默认 CI 或本地直接运行时很多关键测试会 skip。

这不是错误，但会降低持续回归保护价值。

### 需要补充

- 在文档中明确 integration 测试所需服务和环境变量。
- 增加可选 docker compose / testcontainers 方案，用于 CI 启动 etcd、consul、redis、nats。
- 至少让 local cluster integration 默认可运行，或移到普通单元测试中。

## 建议优先级

1. 先补 `ServiceContext` 生命周期测试和 `ClusterSwitchProvider` API 测试，因为它们直接保护启动和动态 provider 的真实行为。
2. 再补 `Transport quic/mq` 显式配置测试，对应当前第三轮 bug。
3. 然后补 WebSocket broker 的真实 HTTP 转发和 drain removal 测试。
4. 最后补 MQ/event-stream、service/manage、WayPlus 的 contract/integration 测试。
