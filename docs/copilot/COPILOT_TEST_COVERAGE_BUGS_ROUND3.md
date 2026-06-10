# Copilot Test Coverage Bugs Round 3

本文件记录 Codex 对提交 `4c472cf test: add Round 2 coverage — config validation, switch success paths, manage CRUD` 的复查结果。

## 已完成的新增覆盖

- `pkg/server/router/servicecontext_test.go`
  - 增加了 config validate 级别的 Cluster/MQ/Transport 配置错误测试。
  - 增加了 `SetRunState(false)` 后 running node 为空的断言。
- `pkg/server/api/manage/clustermanage_test.go`
  - 增加了 `complete` / `rollback` 成功路径，但前置迁移是直接调用 `sc.ClusterSwitcher.Begin`。
  - 增加了 `ClusterStatus` 在 switch 后返回新 provider name 的断言。
- `service/manage/crud_test.go`
  - 增加了 View schema 的基础测试。
  - 增加了 Add 的 DoBefore stop、DoBefore error、Insert、DoAfter 测试。

## 当前验证结果

以下测试通过：

```bash
go test ./pkg/server/config ./pkg/server/cluster ./pkg/server/transport/... ./pkg/server/mq ./pkg/server/event ./pkg/server/types ./pkg/server/router ./pkg/server/api/manage ./service/manage ./pkg/persistence/types
go test -tags=integration ./tests/integration/...
```

## P0: ServiceContext 的 Mode=on / transport misconfig panic 仍未真实覆盖

### 现状

新增测试只验证了 config struct 的 `Validate()` 行为：

- `pkg/server/router/servicecontext_test.go:134-146` 测 `ClusterConfig.Validate()`。
- `pkg/server/router/servicecontext_test.go:148-156` 测 `TransportConfig.Validate()` 接受 `quic`。
- `pkg/server/router/servicecontext_test.go:158-166` 测 `MQConfig.Validate()`。

但 `COPILOT_TEST_COVERAGE_BUGS_ROUND2.md` 要求的是 `NewServiceContext` 层面的真实行为：

- `Cluster.Mode=on` provider 初始化失败时 `NewServiceContext` 是否 panic。
- `MQ.Mode=on` provider 初始化失败时 `NewServiceContext` 是否 panic。
- `Transport.Internal=quic/mq` 显式配置错误时 `NewServiceContext` 是否 panic。

当前测试没有构造真实 `ServerConfig` 给 `NewServiceContext`，因此无法防止 `NewServiceContext` 再次吞掉错误或静默 fallback。

### 需要补充

- 增加可测试的配置注入 seam，例如：
  - 让 `NewServiceContext` 可接收 test config provider；或
  - 在测试中使用临时配置目录，并保证不会污染真实配置文件；或
  - 新增 `NewServiceContextWithConfigForTest(service, config)` 这类内部测试 helper。
- 使用 `require.Panics` 覆盖：
  - cluster on + etcd endpoints empty / invalid。
  - mq on + redis-stream invalid address。
  - transport internal quic / mq。

## P1: ClusterSwitchProvider.Do 仍未覆盖 begin action 的成功路径

### 现状

`clustermanage_test.go` 新增了 complete / rollback 成功路径，但测试是先直接调用：

```go
sc.ClusterSwitcher.Begin(context.Background(), newProvider)
```

然后再调用 API 的 `complete` 或 `rollback`。这绕过了 `ClusterSwitchProvider.Do` 中的 `begin` 分支，也绕过了 `buildTargetProvider()`。

因此仍未覆盖：

- `ClusterSwitchProvider{Action:"begin"}` 成功创建目标 provider。
- `begin -> complete` 全流程都经由管理 API。
- `begin -> rollback` 全流程都经由管理 API。

### 需要补充

- 增加测试 seam，让 `ClusterSwitchProvider` 在测试里可构建 local/fake provider。
- 或让 `buildTargetProvider` 支持测试用 `"local"` provider，但避免生产 API 暴露不安全行为。
- 覆盖：
  - API begin 成功返回 ok。
  - API complete 后 `sc.ClusterProvider == sc.ClusterSwitcher.Current()`。
  - API rollback 后 `sc.ClusterProvider` 回到 original provider。
  - `ClusterStatus.Do` 返回切换后的 provider。

## P1: service/manage CRUD contract 只覆盖了 View 和 Add，仍缺大部分标准操作

### 现状

`service/manage/crud_test.go` 当前只覆盖到：

- `View.Do` 输出包含字段。
- `Add.Do` 的 DoBefore stop、DoBefore error、Insert、DoAfter。

文件在 `service/manage/crud_test.go:221` 结束，没有覆盖：

- `Search`
- `Edit`
- `Remove`
- `Submit`
- `Release`
- `SearchBefore` / `SearchAfter`
- Edit/Remove/Submit/Release 的 DoBefore/DoAfter hook 顺序和错误传播

这还不能证明 `ManageService[T]` 的 7 个标准操作 contract 是稳定的。

### 需要补充

- 扩展 `mockDataAction`，记录 `Load` / `Update` / `Delete` 调用参数。
- 覆盖 `Search.Do` 是否构造正确查询并返回列表。
- 覆盖 `Edit.Do` 是否调用 Update。
- 覆盖 `Remove.Do` 是否调用 Delete。
- 覆盖 `Submit.Do` / `Release.Do` 的状态流转或至少路由行为。
- 覆盖 `SearchBefore` / `SearchAfter` hook。
- 覆盖 hook error 是否阻止后续 DB 操作。

## P1: WayPlus 前端组件/schema contract 仍未补

### 现状

目前前端仍只有登录页测试。未发现 `web/admin/src/components/WayPlus` 下的测试文件。

缺少：

- `WayPage`
- `WayTable`
- `WayForm`
- 后端 manage schema fixture 渲染
- init/search/add/edit/remove 的 request 形态断言
- `web/admin/src/pages/views/main.tsx` 与 WayPlus 的路由配合测试

### 需要补充

- 增加 WayPlus 组件测试。
- 使用 `service/manage` 生成的 schema fixture 或手写稳定 fixture。
- 断言字段、表格列、按钮、表单控件存在。
- 断言组件调用 `web/admin/src/components/WayPlus/request.ts` 的参数结构。

## P1: MQ / event-stream provider contract 仍未补

### 现状

MQ 测试仍停留在 manager/switcher/factory 层。未新增：

- Redis Stream provider integration。
- NATS JetStream provider integration。
- event-stream 通过 MQ provider 发布和消费。
- MQ internal transport request/response。

### 需要补充

- `CORE_TEST_REDIS_STREAM` 控制 Redis Stream integration。
- `CORE_TEST_NATS` 控制 NATS JetStream integration。
- provider contract helper 覆盖 publish -> subscribe -> ack -> health。
- event-stream + MQ provider 集成测试。

## P2: 测试代码需要 gofmt

### 现状

新增测试中有多处缩进不符合 gofmt，例如 `servicecontext_test.go` 新增测试块和 `clustermanage_test.go` success path 块。当前 Go 编译能通过，但提交前应统一 `gofmt`。

### 需要补充

- 对新增 Go 测试执行：

```bash
gofmt -w pkg/server/router/servicecontext_test.go pkg/server/api/manage/clustermanage_test.go service/manage/crud_test.go
```

## 下一步建议

1. 先补 `NewServiceContext` 真实 panic 测试，因为这是 P0，当前测试没有覆盖核心行为。
2. 再补 `ClusterSwitchProvider.Do` 的 API begin 成功路径。
3. 然后补完整 `service/manage` 七个标准操作 contract。
4. 再补 WayPlus schema/component contract。
5. 最后补 Redis/NATS provider contract 和 event-stream 集成。
