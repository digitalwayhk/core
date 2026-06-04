# Copilot Test Coverage Review - Round 6

Review target:

- Main repo commit: `f118be2 test(round5): Submit state fix, MQBridge ServiceContext wiring, EventBridge test, lockfile`
- `web/admin` commit: `0c6b743 fix(lockfile): add package-lock.json for reproducible installs, update lint-staged for biome ignore`

结论：Round 5 的主体修复已经有效。Submit 的 State 0 -> 1 路径、`web/admin` submodule 指针、lockfile、Go 测试和 WayPlus Jest 基础运行都已推进完成。剩余问题只集中在两个尾项：ServiceContext 自动初始化路径的测试没有按 Round 5 要求覆盖，普通 WayPlus Jest 命令仍有 open handles warning。

## 已验证通过

- `git status --short`
  - 只有 `?? COPILOT_TEST_COVERAGE_BUGS_ROUND5.md`。
- `git ls-tree HEAD web/admin`
  - 已指向 `0c6b743922e5f4d2762c35b41f94c13ffdfefed3`。
- `web/admin/package-lock.json`
  - 已被 submodule 跟踪提交。
- `gofmt -l pkg/server/router/servicecontext.go pkg/server/router/servicecontext_test.go service/manage/crud_test.go service/manage/submit.go pkg/server/event/mqbridge.go pkg/server/event/mqbridge_test.go`
  - 无输出。
- `go test ./pkg/server/router ./pkg/server/api/manage ./service/manage ./pkg/server/mq ./pkg/server/event`
  - 通过。
- `go test -tags=integration -v ./tests/integration/...`
  - 通过；Redis/NATS/etcd/Consul 测试仍因环境变量未设置而 `SKIP`。
- `npm test -- --runInBand --detectOpenHandles src/components/WayPlus/__tests__/WayPage.test.tsx src/components/WayPlus/__tests__/WayTable.test.tsx src/components/WayPlus/__tests__/WayForm.test.tsx`
  - 通过，3 suites / 14 tests。

## P2 - EventBridge 测试没有覆盖 NewServiceContextWithConfig 自动初始化路径

文件：`pkg/server/router/servicecontext_test.go`

Round 5 要求：

- `NewServiceContextWithConfig` 初始化出 `MQManager` 后，可以获得 MQ-backed event stream / bridge。
- 通过该框架入口 Publish/Subscribe `event.Envelope`。

当前测试：

```go
sc := &router.ServiceContext{MQManager: mgr}
sc.EnableEventBridge()
```

它验证了 `EnableEventBridge()` 本身，但没有验证 `initServiceContextPost` 中：

```go
if mgr != nil && containsUsage(con.MQ.Usage, "event-stream") {
    sc.EnableEventBridge()
}
```

会在 `NewServiceContextWithConfig` 路径自动发生。

要求：

- 增加一个可测试的 MQ provider 注入缝，或在 config/factory 层提供 test provider，使 `NewServiceContextWithConfig` 能在无需 Redis/NATS 的情况下初始化 fake `MQManager`。
- 新增测试必须从 `router.NewServiceContextWithConfig(...)` 开始，而不是手工构造 `&router.ServiceContext{MQManager: mgr}`。
- 断言：
  - `sc.MQManager != nil`
  - `sc.EventStream != nil`
  - `sc.EventBridge != nil`
  - 通过 `sc.EventBridge.Publish/Subscribe` 能完成一轮 `event.Envelope` 投递。

## P2 - 普通 WayPlus Jest 命令仍有 open handles warning

文件：`web/admin`

普通命令：

```bash
npm test -- --runInBand src/components/WayPlus/__tests__/WayPage.test.tsx src/components/WayPlus/__tests__/WayTable.test.tsx src/components/WayPlus/__tests__/WayForm.test.tsx
```

结果：

- 测试通过：3 suites / 14 tests。
- 但仍输出：

```text
Jest did not exit one second after the test run has completed.
```

`--detectOpenHandles` 命令可以通过且没有打印具体 handle，但普通命令仍有 warning。Round 5 要求是“清理，或明确说明 warning 来源和是否可接受”，当前提交没有说明。

要求：

- 优先清理 warning。
- 如果 Umi/Jest 组合导致该 warning 无法避免，请在 `web/admin` 的测试说明文档或提交报告中说明原因，并推荐使用的验证命令。

## 提交要求

- 修复后提交一次主仓库 commit。
- 如果改 `web/admin`，先在 submodule 内提交，再在主仓库提交 submodule 指针。
- 回复必须包含：
  - 主仓库 commit hash。
  - `web/admin` commit hash，如有变更。
  - `git status --short` 结果。
  - Go 测试结果。
  - WayPlus 普通 Jest 命令和 `--detectOpenHandles` 命令结果。
