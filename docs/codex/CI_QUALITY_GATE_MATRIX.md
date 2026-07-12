# CI 质量门禁矩阵

本文是 `scripts/ci.sh`、本地开发与 GitHub Actions 的门禁契约。状态为“已启用”的 gate 必须能在干净检出中直接运行；“观察”或“定时”不等于通过，失败时仍需保留日志并指向 owner。

| Gate | 状态 | 阻断 | 触发 | 预算 | 命令 | 外部依赖 | Owner | 提升条件 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `required/quick` | 已启用 | 是 | PR、push | 5 分钟 | `./scripts/test.sh quick` | 无 | core | 快速包与 server vet 稳定通过 |
| `required/contracts` | 已启用 | 是 | PR、push | 8 分钟 | `./scripts/test.sh release-contract` | 无 | release tooling | API、安全、配置与发布候选契约全绿 |
| `required/server-manage` | 已启用 | 是 | PR、push | 10 分钟 | `go test ./pkg/server/... ./service/manage/... -count=1 -timeout=10m` | 无 | server/manage | 默认测试不连接外部服务 |
| `required/race` | 已启用 | 是 | PR、push | 12 分钟 | `./scripts/test.sh concurrency-race` | 无 | server/manage | 单轮 race 分片无已知不稳定项 |
| `observational/persistence` | 观察（本地与 Docker 已通过） | 否 | PR、push | 10 分钟 | `./scripts/test.sh persistence-unit` | 无 | persistence | 连续 CI 稳定后评估提升，不因一次本机通过直接升级 required |
| `scheduled/stress` | 定时 | 否 | nightly、手工 | 30 分钟 | `./scripts/test.sh concurrency-stress` | 无 | server lifecycle | 20 轮压力长期稳定后评估提升 |
| `scheduled/integration` | 定时（真实 driver 契约已通过） | 否 | nightly、手工 | 20 分钟 | `./scripts/test.sh integration-persistence` | Docker Compose | persistence | MySQL/MongoDB/ClickHouse 与清理已通过；连续 scheduled 稳定后评估提升 |
| `consumer/futures` | 手工、发布候选 | 发布时阻断 | workflow_dispatch | 15 分钟 | `./scripts/test-consumer-futures.sh` | futures 精确 Git commit | release/consumer | token/本地对象库可用时必须通过；不可用明确 blocked，不能记 passed |

## 外部能力状态

- etcd、Consul、Redis Streams、NATS JetStream 已有真实 Compose 集成命令，但不进入 PR required。
- Kafka 仅有基础设施 profile，Core 无内建 Provider，不登记虚假绿色 gate。

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
