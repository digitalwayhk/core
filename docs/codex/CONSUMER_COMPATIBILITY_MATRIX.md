# Core 消费方兼容性矩阵

证据采集于 2026-07-13。commit 用于复现**已提交树**中的锁定，不代表自动跟随。脏工作树中的未提交 `go.mod` 不得写入「Core 锁定」列，只能作为旁注。

| 消费方 | 本地 commit | 工具链 | Core 锁定 | 状态 | Smoke |
| --- | --- | --- | --- | --- | --- |
| futures | `203ff8eda53a9691d9409d3ee32aa5868fa1d61f` | Go 1.26.1 | `v0.0.247` | 直接消费 | gateway/worker 行为测试 + services 根包仅编译，均通过临时 `go.work` 指向候选 Core |
| omni-flow-ai/grok | `6865e7c497b76ffd883d56998f2db4669f9c02be` | backend Go 1.24.1 | not-applicable | backend `go.mod` 不依赖 core | 在 core 发布门禁中不运行；由该项目自身构建验证 |
| ops-ai | `78499df57832577ac0358b7137f0ee39cf9db135` | Node/TypeScript | not-applicable | 当前仓库无 Go module/core 依赖 | 在 core 发布门禁中不运行；执行其自身 agent/build 流程 |
| ai-ops-platform（提交态） | `a64a3bbf03a014dd0b520f1bf55ab1caa20aaecd` | Go 1.26.0 | `v0.0.247`（`git show a64a3bb:go.mod`） | 可选兼容参考，非当前 ops-ai；可从该 commit 复现 | 仅在仓库恢复为活动消费方后运行其 Go smoke |
| ai-ops-platform（脏工作树旁注） | 同上 HEAD，但 `go.mod`/`go.sum` 等未提交 | Go 1.26.0 | 工作树曾出现未提交 `v0.0.248-0.20260611104225-b17bfabcd8af`；**不可**与上列 commit 同时当作已提交锁定 | 非生产证据；仅说明本地漂移，不得作为发布锁定 | 不作为 core 发布门禁通过条件 |

生产消费方必须使用 tag 或精确 commit。移动开发分支只用于临时验证，不得写入生产锁定列。

## 任务 15.5 消费方验证

2026-07-13 使用 `/private/tmp` 临时 `go.work` 将 futures 指向当前 Core 工作树，未修改 futures 的 `go.mod/go.sum`。Go 因 futures 的 `go 1.26.1` 要求自动选择 1.26.5；以下 smoke 退出码为 0：

```bash
go test ./gateway/api/... ./internal/pkg/services/... -count=1 -timeout=10m
```

omni-flow/grok 与当前 ops-ai 已按上表证据判定 not-applicable，不伪造 Core 消费测试结果。

## 任务 16.5 门禁收敛

矩阵原命令包含 `./internal/pkg/services/...` 的全部行为测试，但同一精确 futures commit 在其锁定 Core `v0.0.247` 下已有服务注册、心跳和 Redis mock 失败，不能作为候选 Core 的有效通过基线。任务 16.5 将可复现 smoke 收敛为：

```bash
go test ./gateway/api/... ./internal/pkg/services/worker/... -count=1 -timeout=10m
go test ./internal/pkg/services -run '^$' -count=1 -timeout=10m
```

第一条运行基线稳定行为，第二条仍编译 services 根包以捕获 Go API 破坏。已知红行为测试不计为候选通过，也不伪装成已验证；其修复属于 futures owner。

## gRPC MAJOR 候选约束

`socket-to-grpc-v1` 删除公开 Go API，以上 2026-07-13 证据只能证明旧基线，不足以批准正式发布。发布前必须在 futures 精确提交上以临时 `go.work` 指向候选 Core，执行 Socket 残留扫描、services 编译和稳定行为 smoke；证据由任务 10 写回本矩阵。在证据写回前，正式发布状态为 `blocked-by-consumer-verification`，不得 tag 或发布；开发期 `--candidate` 仍可验证 Core 自身契约。
