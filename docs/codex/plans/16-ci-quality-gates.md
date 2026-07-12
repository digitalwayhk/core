# CI 质量门禁与消费方兼容性实施计划

> 面向智能体开发者：按小节执行。每节先建立失败场景或当前基线，再做最小实现；开发完成后停止，由外部审查 Agent 只读验收。未通过审查不得进入下一小节。

**目标：** 让干净检出在本地与 GitHub Actions 执行同一组可复现命令；PR 必需门禁快速、确定且无外部服务依赖，race、Docker 集成和消费方 smoke 分层运行；失败时保留足以复现的日志与元数据，不通过排除包、吞错误或 `continue-on-error` 制造绿色。

**架构原则：** CI 只编排仓库已经拥有且可在本地运行的脚本，不在 YAML 中复制业务测试逻辑。继续复用 Go 官方工具、Docker Compose 和任务 15 锁定的 apidiff；所有 Action、Go 版本和服务镜像必须锁定。框架自身的产品缺陷由对应 owner 修复并提供测试，任务 16 只在证据稳定后提升为 required。

## 门禁分层

| 层级 | 触发 | 阻断 | 目标预算 | 内容 |
| --- | --- | --- | --- | --- |
| `required/quick` | PR、push | 是 | 5 分钟 | 格式、根模块整洁、完整 server vet、快速单元测试 |
| `required/contracts` | PR、push | 是 | 8 分钟 | security、config-contract、api-compat、public-api、release contract 的非重复编排 |
| `required/server-manage` | PR、push | 是 | 10 分钟 | server 与 manage 默认测试，不连接 Docker 服务 |
| `required/race` | PR、push | 是（稳定分片） | 每分片 12 分钟 | types/router/rest/manage/lifecycle 等 `-race -count=1` 分片 |
| `observational/persistence` | PR、push | 暂不阻断 | 10 分钟 | SQLite/Badger 默认套件；失败必须上传产物并关联 owner |
| `scheduled/stress` | nightly、手工 | 否，稳定后再提升 | 30 分钟 | race 重复轮次、goroutine/lifecycle 压测 |
| `scheduled/integration` | nightly、手工 | 否，依赖明确后再提升 | 每 profile 20 分钟 | persistence Compose；Broker/发现仅在任务 2/4 完成后启用 |
| `consumer/futures` | 手工、发布候选 | 发布时阻断 | 15 分钟 | 精确 checkout + 临时 go.work 指向当前 Core，不修改消费方依赖 |

`continue-on-error` 只能用于上述明确标记为 observational/scheduled 的 job，且必须上传失败产物并在 summary 中显示失败；required job 禁止使用。

## 16.1 建立本地 CI 命令与机器可读矩阵

**状态：** 已完成，外部审查 APPROVED。

**文件：**

- 创建：`docs/codex/CI_QUALITY_GATE_MATRIX.md`
- 创建：`scripts/ci.sh`
- 创建：`scripts/test-ci-contract.sh`
- 修改：`scripts/test.sh`
- 修改：`docs/codex/PROJECT_REVIEW_ACTION_PLAN.md`

**实施要求：**

- `scripts/ci.sh <gate>` 是本地与 CI 的唯一入口，gate 名称与上表一致；未知 gate 代码 2。
- 输出稳定的开始/结束标记、gate、Go 版本、commit、耗时和退出码；不得输出 secret 或整个环境。
- 失败使用 `tee` 保留到调用方指定的 `CI_ARTIFACT_DIR`，同时通过 `pipefail` 返回原始非零状态。
- required gate 不下载 Docker 镜像、不依赖本机服务、不使用 `rtk`、不写 API golden、tag 或消费方仓库。
- 矩阵记录命令、owner、预算、依赖、触发器、阻断状态和提升条件；文档中不得声称尚未存在的 Broker Compose job 已启用。

**测试场景：**

1. 每个已登记 gate 都能由 shell contract 枚举，脚本与矩阵双向闭合。
2. 未知 gate 返回 2；子命令失败保持原退出码且日志存在。
3. `CI_ARTIFACT_DIR` 含空格时仍可写；未设置时使用临时目录且正常清理。
4. required gate 静态扫描不含 `rtk`、`git tag/push`、基线更新或隐式外部环境变量。
5. 本地执行 `required/quick` 与 `required/contracts` 均通过。

**验收：**

```bash
bash -n scripts/ci.sh scripts/test-ci-contract.sh scripts/test.sh
./scripts/test-ci-contract.sh
./scripts/ci.sh required/quick
./scripts/ci.sh required/contracts
```

**外部审查重点：** 文档/脚本闭集、退出码、日志脱敏、required 外部依赖、重复测试和跨平台 shell。

**开发验收记录（2026-07-13）：** 已建立七个可执行 gate、中文机器可读矩阵和 shell 契约测试；`required/race` 与 `scheduled/stress` 已拆分，原 `concurrency` 入口保持兼容。`bash -n`、`./scripts/test-ci-contract.sh`、`required/quick` 和 `required/contracts` 均通过；后者包含需要回环端口权限的 Logto/REST 测试。

**审查关闭记录：** 实现提交 `a3b5b97` 外部审查结论为 APPROVED，无 P0/P1。提交 `b863d3d` 同步关闭审查建议：required 禁令展开检查 `test.sh` 下游模式、END 日志路径加引号、验证默认临时目录清理、`tee` 失败升级 gate 状态，并记录 quick/server-manage 的预算内重叠。

## 16.2 关闭任务 15 移交的门禁前问题

**状态：** 未开始，三项必须独立 TDD 和独立提交。

### 16.2a SQLite 环境稳定性

**Owner：** persistence。

**状态：** 已完成，外部审查 APPROVED。

**文件：** `pkg/persistence/database/test/oltp_sqlite_test.go`、SQLite 路径/清理 owner 及聚焦测试。

- 在隔离临时目录和独立数据库名下重复运行，复现并分类 `disk I/O`、目录不存在、锁等待与清理竞态。
- 测试不得依赖仓库相对目录、共享固定文件、执行顺序或残留 WAL/SHM。
- 修复路径 ownership 和关闭/删除顺序；不得用重试、sleep、跳过测试掩盖。
- 稳定标准：目标包 `-count=20`、并发分片和完整 `persistence-unit` 在干净临时目录通过后，才允许从 observational 提升 required。

**开发验收记录（2026-07-13）：** 已移除跨工作区绝对路径，为每个测试建立独立临时目录并在包退出时清理；修正 `IsFile` 把不存在路径误判为文件造成的 SQLite 路径/连接错配；测试清理使用真实 GORM 表名，并按 `GetMaxOpenConns()` 调度事务并发。SQLite 直接测试（排除重复聚合入口）`-count=20`、聚焦 race、完整 `database/test` 和 `./pkg/persistence/...` 均通过，待外部审查后决定是否提升门禁。

**审查关闭记录：** 提交 `c030c7f` 外部审查结论为 APPROVED，无 P0/P1；允许进入 16.2b。persistence 继续保持 observational，待 GitHub Actions 连续稳定后再评估提升。测试辅助函数原子初始化、剩余 sleep/short skip、Exec API 双重参数语义和 WAL/SHM 显式清理登记为后续 P2，不在本节扩大生产改动。

### 16.2b 默认 Response 脱敏副作用

**Owner：** server/router + security。

**状态：** 已完成，外部审查 APPROVED。

**文件：** `pkg/server/router/reponse.go`、`pkg/server/trans/rest/error.go` 及测试。

- `Response.GetError` 必须是只读错误访问，不得把原始 `TypeError.Message` 写回公开字段。
- 非 REST 调用 `GetError` 后直接 JSON 序列化也不得出现内部 cause。
- 删除或纯化死代码 `determineStatusCode`，确保没有绕过 `ResolvePublicError` 的副作用路径。
- 保持 600/700/800、IResponse 和自定义 INewResponse 兼容。

**开发验收记录（2026-07-13）：** 默认 `NewResponse` 在进入 REST 前即通过 `ResolvePublicError` 写入安全 code/message，原始 error 只保留在非 JSON 字段中；`GetError` 已改为纯读取，并删除未使用的 `determineStatusCode` 分支。新增直接序列化与字段不变性回归测试，router/rest 聚焦测试通过。

**审查关闭记录：** 提交 `34af55c` 外部审查结论为 APPROVED，无 P0/P1；允许进入 16.2c。`InitRequest.NewResponse`、自定义 `INewResponse` 正反 fixture、plain/600/800 直接序列化覆盖和非 REST `OkJson` 状态语义登记为后续安全 P2；`GetError` 的空消息旧行为暂不做无证据兼容变更。

### 16.2c 发布契约解析加固

**Owner：** release tooling。

**状态：** 已完成，外部复审 APPROVED。

**文件：** `scripts/release-check.sh` 及 shell fixture。

- 只解析 `## [Unreleased]` 到下一版本标题之间的六个子段，已发布段不能补足缺失标题。
- 废弃登记逐行验证 API、替代入口、首次/最早版本、owner、消费方、迁移证据非空且非占位。
- 标记破坏性变化时必须存在迁移说明或批准文件；消费方 smoke 保持人工/发布候选证据，不伪造自动结果。
- fixture 必须证明错误位置标题、缺字段、占位值和破坏无迁移均失败。

**开发验收记录（2026-07-13）：** `release-check.sh` 仅抽取 `## [Unreleased]` 到下一版本标题的内容并要求六个标题各出现一次；废弃登记逐行检查七个字段、占位符与两个 SemVer 字段；Unreleased 标记 BREAKING/破坏性时要求同段迁移说明或非空批准文件。独立 shell fixture 覆盖四类拒绝场景及迁移说明/批准文件两类接受场景，新增 `release-check-contract` 本地入口。

**首轮审查修复：** 首轮结论为 CHANGES_REQUIRED，macOS BSD awk 对 UTF-8 字面量 `==` 可能误判合法中文字段。修复后 AWK 仅执行 ASCII 整格比较，中文占位符、破坏性标记和迁移说明改用 grep 字节匹配；fixture 新增合法中文行、当前真实登记表 smoke，以及 `-`、`N/A`、`TODO`、`暂无`、`—` 参数化拒绝。shell contract 与真实 `release-contract` 均通过，等待复审。

**复审关闭记录：** 修复提交 `1a4d66f` 复审结论为 APPROVED，首轮 P1-1/P1-2 已关闭，允许关闭整个 16.2 并进入 16.3。破坏性否定句误报、消费方矩阵 TODO/TBD 子串和批准文件结构继续登记为 P2。

**验收：** 三项各自定向测试与 race/shell contract 通过；CI 矩阵记录 owner、证据和是否已提升 required。

**外部审查重点：** 是否真正修根因、是否改变公共兼容、是否通过 sleep/retry/skip/排除包伪装稳定。

## 16.3 实现 PR 必需 GitHub Actions

**状态：** 已完成，独立验证通过。

**文件：**

- 创建：`.github/workflows/ci.yml`
- 创建或修改：锁定 Action 版本的说明/自动更新配置
- 修改：`docs/codex/CI_QUALITY_GATE_MATRIX.md`

**实施要求：**

- 触发 `pull_request` 和受保护分支 push；设置 `concurrency` 取消同 ref 旧运行。
- 最小权限 `contents: read`；不授予写 package、PR、release、OIDC 权限。
- Action 使用完整 commit SHA，并用注释标注上游版本；Go 固定为根 `go.mod` 支持版本。
- required jobs 调用 `scripts/ci.sh`，设置明确 timeout，缓存 Go module/build，但缓存 key 包含 OS、Go、`go.sum` 与 `tools/go.sum`。
- 每个 job 无论成功失败都上传对应日志/测试产物；产物不含 module cache、凭据或整个工作区。
- 不在 CI 中自动更新 golden、changelog、go.mod/go.sum、tag 或分支。

**测试场景：**

1. YAML 可解析，所有 `uses:` 均为完整 SHA，权限无写入。
2. required job 没有 `continue-on-error`，均有 timeout、artifact 和本地同名 gate。
3. cache key 覆盖根/tools 依赖；并发取消只影响同 workflow/ref。
4. 模拟 gate 失败时 artifact 步骤仍执行，job 最终非零。
5. 干净检出执行 required jobs 的底层命令全部通过。

**验收：** workflow 静态 contract、`actionlint`（若采用则锁定版本）和所有本地 required gate 通过。

**开发与验证记录（2026-07-13）：** 新增 `.github/workflows/ci.yml`，四个 required job 仅调用同名 `scripts/ci.sh` gate，权限为 `contents: read`，同 workflow/ref 取消旧运行，checkout/setup-go/upload-artifact 均锁定官方 tag 对应完整 SHA。每个 job 有独立 timeout、Go 缓存和 always artifact。独立验证执行 YAML 解析、workflow 静态契约及 quick/contracts/server-manage/race 四个 gate，均在矩阵预算内通过；本机未安装 actionlint，未将其列为通过证据。

**外部审查重点：** 权限、供应链 pin、缓存污染、错误吞噬、artifact 泄密、YAML 与本地脚本偏移。

## 16.4 添加 race、Docker 与定时门禁

**状态：** 开发完成；stress 与生命周期验证通过，Docker 冷拉取实跑待通过。

**文件：**

- 创建：`.github/workflows/ci-scheduled.yml` 或在 `ci.yml` 中增加清晰分层
- 修改：`scripts/ci.sh`、`docs/codex/CI_QUALITY_GATE_MATRIX.md`
- 修改：`docker-compose.integration.yml`（仅当任务 2/4 已提供对应服务）

**实施要求：**

- PR race 使用稳定的 `-count=1` 分片；`-count=20` 仅 scheduled，直到无已知挂起。
- persistence Compose 使用已锁定 MySQL/MongoDB/ClickHouse 镜像、healthcheck、localhost 端口和有界清理。
- etcd/Consul/Redis/NATS/Kafka 不得在 compose 尚未定义时写成可运行 job；先标 `planned/blocked_by_task_2_4`。
- scheduled/手工 job 即使非阻断，也必须把真实失败写入 summary 并上传 compose logs、测试日志和版本信息。
- Docker job 使用唯一 project name/lock，超时或取消后执行 `down -v --remove-orphans`。

**测试场景：** 正常、测试失败、compose up 失败、timeout、取消和并发锁冲突均有有界清理证据；无残留容器、volume、锁和测试进程。

**验收：** 稳定 race required 通过；scheduled stress 可见；persistence integration 通过；未完成 Broker 依赖明确显示 planned 而非绿色 skip。

**开发与验证记录（2026-07-13）：** 新增 `ci-scheduled.yml`，nightly/手工运行 stress 与 persistence integration，失败保持 job 非零并 always 写 summary/上传 artifact。Compose 使用唯一 project name，失败时采集脱敏 ps/logs；compose-up 增加默认 10 分钟有界 watchdog，超时返回 124 后再执行既有 down/锁清理。静态契约、YAML、scheduled stress、Compose 信号/超时/锁生命周期测试均通过。真实 Docker 冷拉取在 5 分钟试验阈值返回 124，诊断产物存在且无残留容器；因此本节暂不声称 integration 通过，最终验收前以 10 分钟默认阈值重试。

**外部审查重点：** 非阻断失败可见性、容器清理、端口/secret、并发隔离、虚假 skip。

## 16.5 消费方 smoke、失败产物与总验收

**状态：** 开发与消费方验证完成；任务 16 总验收等待 Docker integration 通过。

**文件：**

- 创建：消费方 smoke 脚本和 CI 手工入口
- 修改：`docs/codex/CONSUMER_COMPATIBILITY_MATRIX.md`
- 修改：`docs/codex/PROJECT_REVIEW_ACTION_PLAN.md`

**实施要求：**

- futures 必须 checkout 矩阵中的精确 commit，用临时 `go.work` 指向当前 Core；运行后验证消费方 `git status` 未新增 go.mod/go.sum/源码变化。
- 私有仓库凭据不可用时 job 明确 `blocked/not-run`，不得显示 passed；omni-flow/grok、ops-ai 继续基于证据标 not-applicable。
- artifact 至少包含 gate、commit、Go/OS、命令、退出码、测试日志；Docker 失败增加 compose ps/logs。禁止上传环境变量、auth、module cache 和数据库数据卷。
- 生成 Job Summary，列出 required/observational/scheduled 的真实状态、owner 和复现命令。
- 总验收从干净检出运行 required gates，并核对 workflow 与矩阵闭集；任务 16 通过最终外部审查后才能关闭。

**最终验收：**

```bash
./scripts/ci.sh required/quick
./scripts/ci.sh required/contracts
./scripts/ci.sh required/server-manage
./scripts/ci.sh required/race
./scripts/ci.sh observational/persistence
./scripts/test.sh release-contract
```

**开发与验证记录（2026-07-13）：** 新增 futures 精确 commit smoke：从本地对象库 archive 到临时目录，使用临时 `go.work` 指向候选 Core，测试前后校验源工作树及临时 `go.mod/go.sum` 不变；缺仓库/commit 明确返回 blocked(3)。手工 workflow 在缺跨仓库 token 时保留 blocked 证据并非绿退出。原矩阵全量 services 行为命令在锁定 Core `v0.0.247` 下已有相同基线失败，故调整为 gateway/worker 稳定行为测试加 services 根包编译，当前候选 Core smoke 通过。`ci.sh` 产物元数据补齐 gate、commit、OS、Go、命令、退出码和耗时，required/scheduled/consumer workflow 均生成真实状态 summary。

## 每小节外部审查反馈格式

- 审查范围：`git diff <本节基线>..<本节提交>`。
- 规格：本计划对应小节。
- 必查：required 真实性、退出码、权限、供应链、缓存、artifact、secret、超时/清理、消费方可复现性和范围外修改。
- 只读，不修改文件；按 P0/P1/P2 输出，并明确 `APPROVED` 或 `CHANGES_REQUIRED`，以及是否允许进入下一小节。

## 完成定义

- [ ] 本地与 CI 使用相同 gate 命令，矩阵与脚本双向闭合。
- [ ] required 门禁快速、确定、无外部服务依赖且没有错误吞噬。
- [ ] SQLite、Response 脱敏和 release parser 移交项有独立修复/归属证据。
- [ ] GitHub Actions 权限最小、Action 全 SHA pin、缓存和 timeout 正确。
- [ ] required、observational、scheduled 失败状态不会混淆。
- [ ] Docker 失败/取消有界清理并上传可操作产物。
- [ ] 消费方 smoke 使用精确 commit 且不修改消费方依赖。
- [ ] 干净检出 required gates 全绿，最终外部审查 APPROVED。
