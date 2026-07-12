# Core 消费方兼容性矩阵

证据采集于 2026-07-13。commit 用于复现本次盘点，不代表自动跟随。

| 消费方 | 本地 commit | 工具链 | Core 锁定 | 状态 | Smoke |
| --- | --- | --- | --- | --- | --- |
| futures | `203ff8eda53a9691d9409d3ee32aa5868fa1d61f` | Go 1.26.1 | `v0.0.247` | 直接消费 | `go test ./gateway/api/... ./internal/pkg/services/... -count=1` |
| omni-flow-ai/grok | `6865e7c497b76ffd883d56998f2db4669f9c02be` | backend Go 1.24.1 | not-applicable | backend `go.mod` 不依赖 core | 在 core 发布门禁中不运行；由该项目自身构建验证 |
| ops-ai | `78499df57832577ac0358b7137f0ee39cf9db135` | Node/TypeScript | not-applicable | 当前仓库无 Go module/core 依赖 | 在 core 发布门禁中不运行；执行其自身 agent/build 流程 |
| ai-ops-platform（本地旧工作树） | `a64a3bbf03a014dd0b520f1bf55ab1caa20aaecd` | Go 1.26.0 | `v0.0.248-0.20260611104225-b17bfabcd8af` | 可选兼容参考，非当前 ops-ai | 仅在仓库恢复为活动消费方后运行其 Go smoke |

生产消费方必须使用 tag 或精确 commit。移动开发分支只用于临时验证，不得写入生产锁定列。
