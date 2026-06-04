# Copilot Test Coverage Review - Round 5

Review target:

- Main repo commit: `cd9f70d test(round4): CRUD happy-path, MQBridge, TransportMQ panic, WayPlus Jest all pass`
- `web/admin` commit: `3d61130 test(WayPlus): fix Jest infrastructure and make all 14 WayPlus tests pass`

结论：Round 4 又推进了一批测试，Go 目标包通过，WayPlus Jest 在当前本地依赖环境中也能通过。但仍有几个必须修的交付问题：主仓库没有提交 `web/admin` submodule 指针，`Submit.Do` 的 BaseModel 状态流被改成静默跳过，MQBridge 还没有接入 ServiceContext/event-stream 运行路径。

## 已验证通过

- `go test ./pkg/server/router ./pkg/server/api/manage ./service/manage ./pkg/server/mq ./pkg/server/event`
  - 通过。
- `go test -tags=integration -v ./tests/integration/...`
  - 通过；Redis/NATS/etcd/Consul 相关测试仍因环境变量未设置而 `SKIP`。
- `gofmt -l pkg/server/event/mqbridge.go pkg/server/event/mqbridge_test.go pkg/server/router/servicecontext_test.go service/manage/crud_test.go service/manage/submit.go`
  - 无输出。
- `npm test -- --runInBand src/components/WayPlus/__tests__/WayPage.test.tsx src/components/WayPlus/__tests__/WayTable.test.tsx src/components/WayPlus/__tests__/WayForm.test.tsx`
  - 提权后通过：3 suites / 14 tests。
  - 但 Jest 结束后仍提示存在未关闭异步操作：`Jest did not exit one second after the test run has completed`。

## P1 - 主仓库没有提交 web/admin submodule 指针

当前主仓库状态：

```text
 M web/admin
```

`git diff -- web/admin` 显示：

```diff
-Subproject commit 5e4c70a01ef46513fc780550d23b7947364b191c
+Subproject commit 3d61130b0c2b75df868f11a0bcde3619061cccff
```

问题：

- `cd9f70d` 没有包含 `web/admin` submodule 指针更新。
- 其他人 checkout 主仓库 `cd9f70d` 时仍会拿到旧的 `web/admin` commit `5e4c70a`，拿不到 `3d61130` 的 WayPlus Jest 基础设施修复。

要求：

- 在主仓库提交 `web/admin` submodule 指针更新。
- 提交后 `git status --short` 不应再出现 `M web/admin`。

## P1 - Submit.Do 的 BaseModel 状态变更被静默跳过

文件：`service/manage/submit.go`

Round 4 把：

```go
func getbaseModel(instance interface{}) *entity.BaseModel {
    bm, _ := instance.(*entity.BaseModel)
    return bm
}
```

这避免了 panic，但也让普通业务模型失去 Submit 状态变更能力。典型管理模型会嵌入 `*entity.BaseModel`：

```go
type Product struct {
    *entity.BaseModel
}
```

此时 `SearchId` 返回的是 `*Product`，不是 `*entity.BaseModel`。当前 `getbaseModel(*Product)` 返回 nil，导致 `Submit.Do` 中：

```go
if bm.State == 0 {
    bm.State = 1
    own.list.Update(model)
    own.list.Save()
}
```

完全不执行。

`service/manage/crud_test.go` 也在注释中承认没有覆盖 State 0 -> 1：

- `TestSubmit_HappyPath_DoAfterCalled` 只验证 `DoAfter` 被调用。
- 它没有断言 State 变为 1，也没有断言 `Update` / `Save` 被调用。

要求：

- 修复 `getbaseModel`，使其支持嵌入 `*entity.BaseModel` 的业务模型。
- 推荐用接口或反射，而不是只断言 `*entity.BaseModel`：
  - 可以复用 `IsBaseModel()` / `types.IBaseModel` 语义；
  - 或通过反射查找匿名字段 `*entity.BaseModel`。
- 补真正的 Submit happy path 测试：
  - 使用嵌入 `*entity.BaseModel` 的测试模型。
  - `SearchId` 返回 State=0 的旧数据。
  - `Submit.Do` 后断言 State=1。
  - 断言 `Update` 和 `Save` 被调用。

## P1 - MQBridge 仍未接入 ServiceContext / event-stream 运行路径

文件：

- `pkg/server/event/mqbridge.go`
- `pkg/server/router/servicecontext.go`

Round 4 新增了 `MQBridge` 和 fake provider 单元测试，能证明：

- `event.Envelope` 可以序列化后通过 `MQManager.Publish`。
- fake MQ 消息可以反序列化后投递到本地 `event.Stream`。

但当前代码中 `NewMQBridge` 只在 `mqbridge_test.go` 里出现。`ServiceContext` 初始化 `MQManager` 后，没有创建或暴露 MQ-backed event stream，也没有任何运行路径会自动使用 `MQBridge`。

问题：

- 这仍然是独立单元能力，不是框架级集成。
- Round 4 文档要求的是“ServiceContext 初始化 MQManager 后，event-stream 使用该 MQ provider 的框架路径”。

要求：

- 在 `ServiceContext` 或明确的 event provider factory 中接入 MQ-backed event stream。
- 当 MQ 配置启用并包含 event-stream usage 时，框架应能创建/返回 MQ-backed event stream；否则使用本地 `event.Stream`。
- 增加测试验证：
  - `NewServiceContextWithConfig` 初始化出 `MQManager` 后，可以获得 MQ-backed event stream / bridge。
  - 通过该框架入口 Publish/Subscribe `event.Envelope`，fake MQ provider 能观察到消息，并能投递回 handler。

## P2 - WayPlus 测试可运行，但依赖可复现性还没闭环

当前状态：

- 本地存在 `node_modules`，所以 WayPlus Jest 可以通过。
- 本地也存在 `package-lock.json`，但它被 `.gitignore` 忽略：

```text
.gitignore:20:package-lock.json package-lock.json
```

问题：

- `3d61130` 只提交了 `package.json` 和 Jest 配置/测试变更，没有提交 lockfile。
- 干净 checkout 是否能稳定安装仍没有被提交物保证。

要求：

- 明确本项目选择 npm/yarn/pnpm 之一。
- 如果使用 npm，请取消忽略并提交 `package-lock.json`。
- 如果继续不提交 lockfile，需要在报告中明确“依赖安装可复现性未保证”，不能把它算作已完全闭环。

## P2 - WayPlus Jest 有未关闭异步操作提示

测试通过后输出：

```text
Jest did not exit one second after the test run has completed.
```

要求：

- 用 `--detectOpenHandles` 定位并清理未关闭异步任务，或在报告中明确该 warning 的来源和是否可接受。

## 提交要求

- 修复后提交一次主仓库 commit。
- 如果修改 `web/admin`，先在 submodule 内提交，再在主仓库提交 submodule 指针。
- 回复必须包含：
  - 主仓库 commit hash。
  - `web/admin` commit hash，如有变更。
  - `git status --short` 结果，必须干净。
  - Go 测试结果。
  - integration verbose 中 Redis/NATS 是否执行或跳过。
  - WayPlus Jest 测试结果，以及是否还有 open handles warning。
