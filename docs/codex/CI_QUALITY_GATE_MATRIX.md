# CI 质量门禁矩阵

本文是 `scripts/ci.sh`、本地开发与 GitHub Actions 的门禁契约。状态为“已启用”的 gate 必须能在干净检出中直接运行；“观察”或“定时”不等于通过，失败时仍需保留日志并指向 owner。

| Gate | 状态 | 阻断 | 触发 | 预算 | 命令 | 外部依赖 | Owner | 提升条件 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `required/quick` | 已启用 | 是 | PR、push | 5 分钟 | `./scripts/test.sh quick` | 无 | core | 快速包与 server vet 稳定通过 |
| `required/contracts` | 已启用 | 是 | PR、push | 8 分钟 | `./scripts/test.sh release-contract` | 无 | release tooling | API、安全、配置与发布候选契约全绿 |
| `required/server-manage` | 已启用 | 是 | PR、push | 10 分钟 | `go test ./pkg/server/... ./service/manage/... -count=1 -timeout=10m` | 无 | server/manage | 默认测试不连接外部服务 |
| `required/race` | 已启用 | 是 | PR、push | 12 分钟 | `./scripts/test.sh concurrency-race` | 无 | server/manage | 单轮 race 分片无已知不稳定项 |
| `observational/persistence` | 观察 | 否 | PR、push | 10 分钟 | `./scripts/test.sh persistence-unit` | 无 | persistence | SQLite 环境问题按 16.2a 连续验证通过 |
| `scheduled/stress` | 定时 | 否 | nightly、手工 | 30 分钟 | `./scripts/test.sh concurrency-stress` | 无 | server lifecycle | 20 轮压力长期稳定后评估提升 |
| `scheduled/integration` | 定时 | 否 | nightly、手工 | 20 分钟 | `./scripts/test.sh integration-persistence` | Docker Compose | persistence | CI runner 具备 Docker 且清理契约稳定 |

## 尚未启用

- `consumer/futures`：任务 16.5 创建精确 commit checkout 与临时 `go.work` 脚本后登记，当前不得报告为通过。
- etcd、Consul、Redis、NATS、Kafka：等待任务 2/4 明确产品实现和 Compose 服务；当前状态为 `planned/blocked_by_task_2_4`，不以绿色 skip 代替执行。

## 运行约束

- CI 与本地统一调用 `./scripts/ci.sh <gate>`，YAML 不复制测试包清单。
- `required/*` 不使用 Docker、外部服务、`rtk`、基线更新、tag、push 或隐式 `CORE_TEST_*` 环境变量。
- 调用方通过 `CI_ARTIFACT_DIR` 指定持久化日志目录；未指定时脚本使用并清理临时目录。
- 每次执行输出 gate、commit、Go 版本、耗时和退出码，不输出环境变量或凭据。
- 未知 gate 返回 2；测试子命令失败时保留原退出码，不吞错。
