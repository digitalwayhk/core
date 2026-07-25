# Copilot Test Coverage Bugs Round 2

本文件记录 Codex 对提交 `40ad701 test: add missing tests for ServiceContext lifecycle, manage API, and CrossNode broker HTTP forwarding` 的复查结果。

## 已完成的测试补充

- 新增 `pkg/server/router/servicecontext_test.go`
  - 覆盖 `NewServiceContext` 默认初始化 `ClusterProvider` / `TransportSelector` / `ClusterSwitcher`。
  - 覆盖 `SetRunState(true)` 注册节点。
  - 覆盖 `SetRunState(false)` 清理 `CrossNodeBroker`。
  - 覆盖 `SyncProviderAfterSwitch()` 同步 `ClusterProvider`。
- 新增 `pkg/server/api/manage/clustermanage_test.go`
  - 覆盖未注册服务、未知 action、未 begin 就 complete/rollback、unsupported provider。
  - 覆盖 `ClusterStatus` / `ClusterNodes` 的基本返回。
- 新增 `pkg/server/cluster/event_http_test.go`
  - 使用 `httptest.Server` 覆盖 subscription summary POST。
  - 覆盖 `ForwardNotice` 只发给已订阅 peer。
  - 覆盖无订阅时不发 POST。
  - 覆盖 `DrainAndStop` 广播 active=false removal。
- 新增/保留 `pkg/server/transport/factory_test.go`
  - 覆盖 `quic` / `mq` 显式配置返回错误，不再静默 fallback。

## 当前验证结果

提升权限后通过：

```bash
go test ./pkg/server/config ./pkg/server/cluster ./pkg/server/transport/... ./pkg/server/mq ./pkg/server/event ./pkg/server/types ./pkg/server/router ./pkg/server/api/manage ./pkg/persistence/types
go test -tags=integration ./tests/integration/...
```

普通沙箱下 `httptest.NewServer` 会因为本机端口监听限制报 `bind: operation not permitted`，提升权限后通过。

## P0: ServiceContext 生命周期测试还缺 Mode=on 失败路径和 membership 下线断言

### 现状

`servicecontext_test.go` 已覆盖默认初始化、节点注册和 broker 清理，但还缺以下关键行为：

- `Cluster.Mode=on` 且 provider 初始化失败时，`NewServiceContext` 是否 panic。
- `MQ.Mode=on` 且 provider 初始化失败时，`NewServiceContext` 是否 panic。
- `Transport.Internal=quic/mq` 显式配置错误时，`NewServiceContext` 是否 panic。
- `SetRunState(false)` 后，provider 中当前 node 是否被 deregister / 标记 offline。
- `SetRunState(true)` 后 membership heartbeat 是否启动；至少要能断言节点 `LastHeartbeat` 后续更新或 heartbeat 调用发生。

### 需要补充

- 为 `NewServiceContext` 增加配置隔离 helper，避免真实配置文件污染测试。
- 增加 `require.Panics` 覆盖 cluster/mq/transport 强制错误路径。
- 对 `SetRunState(false)` 增加 `ClusterProvider.Get/List` 断言，确认节点状态变为 offline 或不可再作为 running 候选。

## P0: ClusterSwitchProvider 管理 API 缺少 begin -> complete 成功路径

### 现状

`clustermanage_test.go` 当前主要覆盖错误路径和 status 基本返回，没有覆盖成功迁移路径：

- 没有通过 API 执行 `begin -> complete`。
- 没有断言 API complete 后 `sc.ClusterProvider == sc.ClusterSwitcher.Current()`。
- 没有断言 `ClusterStatus.Do` 返回切换后的 provider。
- 没有覆盖 `rollback` 成功路径。

`SyncProviderAfterSwitch()` 虽然在 `servicecontext_test.go` 中直接测了，但没有通过管理 API 调用链触发。

### 需要补充

- 增加可测试 target provider，例如支持测试用 `"local"` provider，或通过测试 seam 注入 fake provider factory。
- 测试 `ClusterSwitchProvider{Action:"begin"}` 成功进入迁移。
- 测试 `ClusterSwitchProvider{Action:"complete"}` 后同步 `ServiceContext.ClusterProvider`。
- 测试 `rollback` 成功后 provider 回到旧 provider。
- 测试 `ClusterStatus.Do` 在 complete 后返回新 provider name。

## P1: service/manage CRUD contract 仍无测试

### 现状

`service/manage` 是框架的核心管理能力，但当前仍没有测试覆盖：

- View/schema 生成。
- Search/Add/Edit/Remove/Submit/Release 标准路由。
- DoBefore/DoAfter/SearchBefore/SearchAfter hook 顺序。
- hook 返回错误时是否阻止后续动作。
- 字段配置、枚举、表单 schema 和前端 WayPlus 的兼容输出。

### 需要补充

- 增加 `service/manage/*_test.go`。
- 使用最小测试模型，覆盖标准 CRUD contract。
- 覆盖 hook 调用顺序和错误传播。
- 输出一份稳定 schema fixture，供前端 WayPlus 测试复用。

## P1: WayPlus 前端核心组件仍无测试

### 现状

前端目前仍只有登录页测试，`web/admin/src/components/WayPlus` 没有测试。后端管理 schema 变化或 WayPlus 渲染逻辑变化时，测试不会报警。

### 需要补充

- 为 `WayPage`、`WayTable`、`WayForm` 增加组件测试。
- 使用后端 manage schema fixture 渲染，断言关键字段、表格列、按钮、表单控件存在。
- 覆盖 init/search/add/edit/remove 的 request 调用形态。
- 覆盖 routes 中 `main` 页面与 WayPlus 的配合。

## P1: MQ / event-stream provider contract 仍无集成测试

### 现状

MQ 当前仍主要是 manager/switcher/factory 单元测试，缺少 provider 级集成 contract：

- Redis Stream publish -> subscribe -> ack。
- NATS JetStream publish -> subscribe -> ack。
- event-stream 通过 MQ provider 发布和消费。
- MQ 作为 internal transport 的 request/response 行为。
- Switcher 的 complete drain/cancel、double-write 失败 rollback 细节。

### 需要补充

- 增加 `CORE_TEST_REDIS_STREAM` 控制的 Redis Stream integration。
- 增加 `CORE_TEST_NATS` 控制的 NATS JetStream integration。
- 建立 provider contract test helper，统一验证 publish/subscribe/ack/health。
- 补 event-stream 使用 MQ provider 的集成测试。

## P2: 外部依赖 integration 默认 skip，缺少可运行说明或 CI 编排

### 现状

etcd、consul、redis、nats 这类外部依赖测试需要环境变量或服务，默认 CI 价值有限。

### 需要补充

- 在测试文档中列出：
  - `CORE_TEST_ETCD`
  - `ETCD_ENDPOINTS`
  - `CORE_TEST_CONSUL`
  - `CONSUL_HTTP_ADDR`
  - `CORE_TEST_REDIS_STREAM`
  - `CORE_TEST_NATS`
- 提供 docker compose 或 testcontainers 方案，让 CI 能跑外部 provider contract。

## 建议下一步优先级

1. 先补 `ServiceContext` 的 Mode=on panic、transport misconfig panic、SetRunState(false) 下线断言。
2. 再补 `ClusterSwitchProvider` 的 begin -> complete / rollback 成功路径。
3. 然后补 `service/manage` CRUD contract。
4. 再补 WayPlus 组件/schema contract。
5. 最后补 Redis/NATS provider contract 和 CI 外部依赖编排。
