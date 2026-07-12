# CI 质量门禁矩阵

本文是 `scripts/ci.sh`、本地开发与 GitHub Actions 的门禁契约。状态为“已启用”的 gate 必须能在干净检出中直接运行；“观察”或“定时”不等于通过，失败时仍需保留日志并指向 owner。

| Gate | 状态 | 阻断 | 触发 | 预算 | 命令 | 外部依赖 | Owner | 提升条件 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `required/quick` | 已启用 | 是 | PR、push | 5 分钟 | `./scripts/test.sh quick` | 无 | core | 快速包与 server vet 稳定通过 |
| `required/contracts` | 已启用 | 是 | PR、push | 8 分钟 | `./scripts/test.sh release-contract` | 无 | release tooling | API、安全、配置与发布候选契约全绿 |
| `required/server-manage` | 已启用 | 是 | PR、push | 10 分钟 | `go test ./pkg/server/... ./service/manage/... -count=1 -timeout=10m` | 无 | server/manage | 默认测试不连接外部服务 |
| `required/race` | 已启用 | 是 | PR、push | 12 分钟 | `./scripts/test.sh concurrency-race` | 无 | server/manage | 单轮 race 分片无已知不稳定项 |
| `observational/persistence` | 观察（16.2a 修复待审查） | 否 | PR、push | 10 分钟 | `./scripts/test.sh persistence-unit` | 无 | persistence | SQLite `-count=20`、race 与完整 persistence 已本地通过；外部审查通过后评估提升 |
| `scheduled/stress` | 定时 | 否 | nightly、手工 | 30 分钟 | `./scripts/test.sh concurrency-stress` | 无 | server lifecycle | 20 轮压力长期稳定后评估提升 |
| `scheduled/integration` | 定时（冷拉取实跑待通过） | 否 | nightly、手工 | 20 分钟 | `./scripts/test.sh integration-persistence` | Docker Compose | persistence | 信号/超时/锁/诊断/清理契约已通过；锁定镜像冷拉取后 driver contract 需通过 |

## 尚未启用

- `consumer/futures`：任务 16.5 创建精确 commit checkout 与临时 `go.work` 脚本后登记，当前不得报告为通过。
- etcd、Consul、Redis、NATS、Kafka：等待任务 2/4 明确产品实现和 Compose 服务；当前状态为 `planned/blocked_by_task_2_4`，不以绿色 skip 代替执行。

## Action 供应链锁定

| Action | 上游版本 | 完整提交 SHA | 用途 |
| --- | --- | --- | --- |
| `actions/checkout` | v4.2.2 | `11bd71901bbe5b1630ceea73d27597364c9af683` | 只读检出，禁用凭据持久化 |
| `actions/setup-go` | v5.5.0 | `d35c59abb061a4a6fb18e82ac0862c26744d6ab5` | 按根 `go.mod` 安装 Go 并缓存依赖 |
| `actions/upload-artifact` | v4.6.2 | `ea165f8d65b6e75b540449e92b4886f43607fa02` | 无论成功失败上传 gate 日志 |

升级 Action 时必须从官方仓库验证 tag 对应 SHA，并同时更新 workflow、本表和静态契约；不得只改注释版本或使用浮动 tag。

## 运行约束

- CI 与本地统一调用 `./scripts/ci.sh <gate>`，YAML 不复制测试包清单。
- `required/*` 不使用 Docker、外部服务、`rtk`、基线更新、tag、push 或隐式 `CORE_TEST_*` 环境变量。
- 调用方通过 `CI_ARTIFACT_DIR` 指定持久化日志目录；未指定时脚本使用并清理临时目录。
- 每次执行输出 gate、commit、Go 版本、耗时和退出码，不输出环境变量或凭据。
- 未知 gate 返回 2；测试子命令失败时保留原退出码，不吞错。
- `required/quick` 与 `required/server-manage` 有意重叠少量 manage 快速测试：前者提供开发反馈，后者验证完整 server/manage 边界；当前耗时预算允许该重叠。
