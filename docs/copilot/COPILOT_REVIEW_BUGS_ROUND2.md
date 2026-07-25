# Copilot Review Bugs Round 2

本文件记录 Codex 对提交 `d17e9c2 Fix P0/P1/P2 bugs from COPILOT_REVIEW_BUGS.md` 的第二轮复查结果。上一轮的大部分基础结构已经补上，但仍有以下问题需要继续修复。

## P0: ClusterSwitcher 切换后没有更新 ServiceContext 的实际 provider

### 现象

`ClusterSwitchProvider.Do` 现在会调用 `sc.ClusterSwitcher.Begin/Complete/Rollback`，但 `ServiceContext.ClusterProvider` 仍然保持旧 provider。当前服务后续用于节点列表、WebSocket broker、membership 心跳、管理 API 查询的仍是 `sc.ClusterProvider` 字段，而不是 `ClusterSwitcher.Current()`。

这会导致管理接口返回 `ok`，但服务实际仍继续使用旧 provider。动态切换 provider 的目标没有真正达成。

### 定位

- `pkg/server/api/manage/clustermanage.go:115-119` 只操作 `sc.ClusterSwitcher`
- `pkg/server/router/servicecontext.go:311-344` 注册节点、membership、CrossNodeNoticeBroker 都使用 `own.ClusterProvider`
- `pkg/server/api/manage/clustermanage.go:25-37` `ClusterStatus` 查询的也是 `sc.ClusterProvider`
- `pkg/server/cluster/switcher.go:27-31` 有 `Current()`，但 `ServiceContext` 没有在 complete 后同步使用它

### 期望修复

- `complete` 成功后必须把 `sc.ClusterProvider` 切到 `sc.ClusterSwitcher.Current()`。
- 如当前服务已运行，需要重新注册节点、重建 membership，并让 `CrossNodeNoticeBroker` 使用新 provider。
- `rollback` 后也应确保 `sc.ClusterProvider` 与 switcher current 保持一致。
- `ClusterStatus` / `ClusterNodes` 应使用当前有效 provider，不应显示旧 provider。

### 必补测试

- 管理 API 调用 `begin -> complete` 后，断言 `sc.ClusterProvider.Name()` 已变为目标 provider。
- 切换后 `ClusterStatus` 返回目标 provider。
- 切换后 membership 使用新 provider 进行 heartbeat。

## P1: ProviderSwitcher 的 warm-up 仍然使用 List(ctx, "")，local provider 迁移不到节点

### 现象

上一轮指出 `clusterSwitcher.Begin` 使用 `s.current.List(ctx, "")` 复制当前节点，这次提交仍未修复。`LocalProvider.List` 会按 serviceName 精确过滤，空字符串不会返回正常服务节点，因此 local -> etcd/consul 的 warm-up 阶段不会迁移任何真实节点。

### 定位

- `pkg/server/cluster/switcher.go:44-54`
- `pkg/server/cluster/provider_local.go:141-164`

### 期望修复

- 不要用空 serviceName 表示全量节点，除非所有 provider 明确定义并实现该语义。
- 可选方案：
  - 为 provider 增加 `ListAll(ctx)` 接口，并在 switcher 中优先使用；
  - 或让 ProviderSwitcher 以 serviceName 为作用域，只迁移当前服务；
  - 或维护 provider 已知 serviceName 列表后逐个迁移。

### 必补测试

- local provider 注册一个 `svc` 节点后执行 `Begin`，断言目标 provider 收到该节点。
- 覆盖 serviceName 为空不能误认为全量列表的场景。

## P1: Mode=on 的 cluster / MQ 初始化错误被 NewServiceContext 吞掉

### 现象

`BuildProvider` 和 `BuildManager` 已经在 `Mode=on` 时返回错误，但 `NewServiceContext` 只是 `logx.Errorf` 后继续创建服务上下文。这样强制模式下外部 provider 或 MQ 配置错误时，服务仍会继续启动，只是缺少 cluster/MQ 能力。

这与计划中 “on 模式强制使用配置 provider，缺配置或连接失败应启动失败” 不一致。

### 定位

- `pkg/server/router/servicecontext.go:209-211` cluster 初始化失败只打日志
- `pkg/server/router/servicecontext.go:218-225` MQ 初始化失败只打日志
- `pkg/server/cluster/factory.go:21-39` `Mode=on` 已返回错误
- `pkg/server/mq/factory.go:17-23` `Mode=on` 已返回错误

### 期望修复

- `Cluster.Mode=on` 且 provider 初始化失败时，应阻止服务启动。现有 `NewServiceContext` 没有 error 返回值，可先采用明确 panic，或引入可返回 error 的新构造函数并保留旧函数兼容。
- `MQ.Mode=on` 且 provider 初始化失败时同理，不能静默降级到没有 MQ。
- `auto` 模式可以继续降级并记录日志。

### 必补测试

- `Cluster.Mode=on Provider=etcd Endpoints=[]` 创建服务上下文应失败或 panic。
- `MQ.Mode=on Provider=redis-stream Addr=""` 且无法连接时应失败或 panic。
- `Mode=auto` 下同样配置可降级，不影响旧服务启动。

## P1: TransportSelector 仍未按 Transport.Internal / Fallback 配置构建

### 现象

`TransportConfig` 声明支持 `grpc | http | socket | quic | mq` 和 fallback 顺序，但 `ServiceContext` 现在只在 `con.Transport.GRPC.Enable` 时创建单个 gRPC selector。HTTP、Socket、QUIC、MQ 都没有按配置加入统一选择链。

这会导致用户配置 `Transport.Internal=http`、`socket`、`quic`、`mq` 或配置 fallback 顺序时不生效。

### 定位

- `pkg/server/config/transportconfig.go:5-13` 定义了 Internal/Fallback 和多 transport 配置
- `pkg/server/router/servicecontext.go:212-216` 只初始化 gRPC
- `pkg/server/transport/http/http_transport.go` 和 `pkg/server/transport/socket/socket_transport.go` 已有实现但未接线
- MQ 作为内部传输的一种选择目前没有 transport adapter

### 期望修复

- 增加 transport factory，例如 `transport.BuildSelector(config.TransportConfig, mqManager)`。
- 按 `Internal` 选择 primary，按 `Fallback` 顺序构建 fallback 列表。
- 已实现的 HTTP / Socket / gRPC 应接入；QUIC 可先接 stub/明确错误；MQ 需增加 adapter 或在配置启用时明确返回未实现错误。
- `sendPayload` 不应要求 `payload.TargetAddress != ""` 才使用 selector；服务发现拿到节点后应由 selector 按节点信息构造 target。

### 必补测试

- `Internal=http` 时 selector primary 为 HTTP。
- `Fallback=["grpc","http","socket"]` 时按配置顺序尝试。
- 未实现的 `mq/quic` 配置在 `Mode=on` 或显式启用时给出明确错误。

## P2: WebSocket 注销应避免重复注销导致 hash 计数为负

### 现象

`UnRegisterWebSocketHash` 现在使用 `rHashClients` 解决了不同 hash 互相影响的问题，但它无条件递减 `rHashClients[hash]`。如果同一个 client 被重复注销，或者传入的 client 本来不在该 hash 下，计数会变成负数并可能重复触发 `active=false` 摘要。

### 定位

- `pkg/server/types/websocketshard.go:88-110`

### 期望修复

- 删除前先判断 `client` 是否存在于 shard。
- 只有实际删除了一个 client 时，才递减 `rHashClients[hash]`。
- 计数小于 0 时应防御性归零并记录异常。

### 必补测试

- 同一个 client 连续调用两次 `UnRegisterWebSocketHash`，只触发一次 `active=false`。
- 未注册 client 调用 unregister 不应改变 hash 计数。

## 当前验证结果

提升权限后，以下针对性测试通过：

```bash
go test ./pkg/server/config ./pkg/server/cluster ./pkg/server/transport/... ./pkg/server/mq ./pkg/server/event ./pkg/server/types ./pkg/persistence/types
go test -tags=integration ./tests/integration/...
```

普通沙箱下 gRPC 测试会因为不能监听 `127.0.0.1:0` 报 `bind: operation not permitted`，提升权限后通过。
