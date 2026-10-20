# Core 消费方兼容性矩阵

初始证据采集于 2026-07-13，v1 候选刷新于 2026-09-06。commit 用于复现**已提交树**中的锁定，不代表自动跟随。脏工作树中的未提交 `go.mod` 不得写入「Core 锁定」列，只能作为旁注。

| 消费方 | 本地 commit | 工具链 | Core 锁定 | 状态 | Smoke |
| --- | --- | --- | --- | --- | --- |
| bitzoom | `4b2b18a8298d47ec1edd3f8dabd1d7c086a2d49c` | Go 1.26.6 | 候选 `v0.0.248-0.20260906080609-e7cf45a7e9db` | 当前重构消费方；已迁移 gRPC/full-e4 | 临时 archive 中 bootstrap + contract 行为测试、全仓 Go 编译通过；未修改消费方工作区 |
| futures | `203ff8eda53a9691d9409d3ee32aa5868fa1d61f` | Go 1.26.1 | `v0.0.247` | 直接消费 | gateway/worker 行为测试 + services 根包仅编译，均通过临时 `go.work` 指向候选 Core |
| omni-flow-ai/grok | `6865e7c497b76ffd883d56998f2db4669f9c02be` | backend Go 1.24.1 | not-applicable | backend `go.mod` 不依赖 core | 在 core 发布门禁中不运行；由该项目自身构建验证 |
| ops-ai | `78499df57832577ac0358b7137f0ee39cf9db135` | Node/TypeScript | not-applicable | 当前仓库无 Go module/core 依赖 | 在 core 发布门禁中不运行；执行其自身 agent/build 流程 |
| ai-ops-platform（提交态） | `a64a3bbf03a014dd0b520f1bf55ab1caa20aaecd` | Go 1.26.0 | `v0.0.247`（`git show a64a3bb:go.mod`） | 可选兼容参考，非当前 ops-ai；可从该 commit 复现 | 仅在仓库恢复为活动消费方后运行其 Go smoke |
| ai-ops-platform（脏工作树旁注） | 同上 HEAD，但 `go.mod`/`go.sum` 等未提交 | Go 1.26.0 | 工作树曾出现未提交 `v0.0.248-0.20260611104225-b17bfabcd8af`；**不可**与上列 commit 同时当作已提交锁定 | 非生产证据；仅说明本地漂移，不得作为发布锁定 | 不作为 core 发布门禁通过条件 |

生产消费方必须使用 tag 或精确 commit。移动开发分支只用于临时验证，不得写入生产锁定列。

## v1.4.4 可靠事件跨实例分担（2026-10-20）

本节记录已发布版本；后续连接池隔离候选见[v1.4.5候选说明](RELEASE_V1_4_5_2026-10-20.md)，尚未发布，不替换已锁定版本。旧构造器及消息契约兼容，新增有限command/blocking预算，需按订阅数核对配置和指标；现场消费方迁移NOT_RUN。

旧 SharedKeys=false 保留单 owner 与 v1.4.2 持续调度；显式新合同需要维护窗口停旧订阅、确认实际 handler 结束及旧 lease 释放，使用同组原 pending，不删除或重置历史。模式与有效组级预取冻结，不支持在线 Provider 切换或直接回旧版本消费共享组。NATS 未支持且拒绝，业务跨副本幂等仍必需。真实 Core 双进程、原生租约硬杀恢复、TCP断网/迟到ACK与第10例三服务 HTTP 分别记录；Bitzoom/生产迁移 NOT_RUN。完整命令、结果和边界见[v1.4.4 发布说明](RELEASE_V1_4_4_2026-10-20.md)。

## v1.4.3 OpenAPI 依赖安全补丁（2026-10-20）

`kin-openapi` 升至 `v0.149.0`，`jsonschema/v6` 升至 `v6.0.3`；gRPC 保持已修复对应 xDS 告警的 `v1.83.2`。Core 的 OpenAPI 生成、HTTP 文档路由和兼容快照须通过本轮普通、race 与发布门禁；五组依赖级安全回归先用 v1.4.2 锁定文件复现失败，再用新依赖验证。详细命令、结果和公告映射见 [v1.4.3 发布说明](RELEASE_V1_4_3_2026-10-20.md)。

公开 API、消息编码、认证域、MQ 默认配置及内嵌产物不变。消费方若自行使用 `openapi3filter.ValidationHandler`，须复测编码斜杠与路由分派；无 `items` 的 query 数组会返回解码错误，OAS 3.1 无约束表单数组可能被接受，不应一律期待 400。第三方库行为收紧不替代业务鉴权。未修改或部署 Bitzoom 等消费方，其升级、部署及自定义中间件验收为 `NOT_RUN`；上表的历史锁定保持原样。

## v1.4.2 Redis 可靠订阅补丁（2026-10-20）

本版修复 `v1.4.1/60791a7` 的 Redis 分 key 可靠订阅跨读取批次屏障，修复提交 `65806a2`；正式版本、发布门禁与升级命令见 [v1.4.2 发布说明](RELEASE_V1_4_2_2026-10-20.md)。公开签名和消息编码不变，零值/1 仍串行；`Count` 增加预取预算约束，满时背压；可靠 handler 不得手动提前 ACK；分 key 超时后等待真实 handler 退出，会影响不合作 handler 的关闭时长。不会清空 PEL、重置 group 或替消费方迁移业务数据。

Core 已在独占 Redis 7.4.11 验证新读取、PEL 分页/恢复、同 key 顺序与失败隔离、并发/预取上限、owner/取消/关闭和正式 MQBridge 链；三包普通回归及 race 各 207 条 PASS 记录，十轮重复回归 200 条 PASS 记录。详细结果见[修复验收](REDIS_RELIABLE_CROSS_WINDOW_FIX_2026-10-20.md)。这些是 Core 证据；Bitzoom 等生产消费方的依赖升级、部署、现场 11.33 秒归因及容量复测仍为 `NOT_RUN`，不得覆盖上表的历史项目锁定或声称已完成消费方迁移。

## v1.4.0 升级边界（2026-10-20）

v1.4.1 补齐了内嵌编译 JSON；当前推荐 [v1.4.4](RELEASE_V1_4_4_2026-10-20.md)，另含 Redis 可靠订阅及 OpenAPI 依赖安全修复。以下仍是 v1.4.0 的历史功能与源码验收边界，不替代新版验证。

上版基线为 `v1.3.0` / `7cdf8d998d9ac54a9e880736284536d27380838b`，本轮功能审查起点为 `5df05bbb2bc31a53e0e3691baa59cea64d844558`；源码验证固定为 `7b5795b`，最终版本与回归范围见[发布记录](RELEASE_V1_4_0_2026-10-20.md)。上述历史表不代表当前生产锁定或本版消费方 smoke，禁止把旧通过结果覆盖成本轮通过。

本轮未修改、升级、重启或部署 Bitzoom/futures 等消费方，消费方精确提交 smoke 与实际存量卷迁移均为 `NOT_RUN`。已完成的 Core 自测仅属于本仓库证据。升级者须按[从 v1.3.0 升级](UPGRADE_FROM_V1_3.md)核对 MQ 缺省、Badger 数据及 v3 原生错误比较、MySQL 认证池隔离、可选 stable 身份与编号迁移，再登记自己的项目/Core 精确提交和结果。依赖本地 Badger 的项目未通过旧卷副本恢复前不得直接切正式流量。

本次变更已合入 main，07 首单启动窗口在 `7b5795b` 修复；固定源码全仓回归 133 包、2656 项测试及子测试通过，55 跳过，变更范围 race 184 项通过。Core 使用已提交的 `pkg/server/run/dist`，普通 Go 消费方不依赖前端子模块源码；该源码精确提交的远端冷获取失败另列为维护限制，不作为内嵌产物发布阻断。消费方升级仍为 NOT_RUN，不把 Core 自测或历史项目 smoke 等同于生产升级验收。

## v1.3.0 Manage RoleCode RBAC 候选

本候选为标准 WebServer 增加默认启用的 Manage 授权。Core 保存管理员主体与稳定 RoleCode 的关系；消费方不得重复建立管理员/角色关系表，也不得把权限明细或权限哈希塞入 token。Core 内置 `core.system_admin` 与 `core.viewer`，自定义角色权限按精确 `service + path + command` 保存和判断。只有角色权威位于外部 IAM 时，消费方才覆盖 `IManageRoleProvider`。

Bitzoom 接入时不需要新增管理员模型或 Provider；升级后直接使用 Core 的管理员主体与角色绑定页面，再做真实 Casdoor Manage callback、refresh 和 HTTP 403 回归。第一个真实 Manage 用户由 Core 绑定 `core.system_admin`，后续用户绑定 `core.viewer`；TestToken 不创建主体或占用首用户名额。若 Bitzoom 已有仅为该功能增加的本地管理员存储/Provider，可删除；既有 `IAuthHookProvider`、业务限域与审计 Hook 保留，外部 IAM 映射仅在确有需要时作为 Provider 兜底。当前没有修改 Bitzoom，本节也不把 Core 单元/HTTP 测试当作 Bitzoom smoke；消费方升级、真实 Casdoor/MySQL 验证和部署状态均为 `NOT RUN`。

本能力不修改 `web/admin`、不隐藏菜单/按钮，也不改变 `/api/servermanage/getmenu`。测试阶段应明确点击无权 Add/Edit/Remove/自定义 command 并断言 403；用户角色关系改变后 refresh/重新登录，角色权限明细改变后下一请求立即复测。

控制面存储只在 `etc/server.json.ManageStore` 配置一次：新配置默认 SQLite `core_manage`；旧配置缺少该字段时继续使用历史 `models` SQLite 库；正式多进程环境可显式改为共享 MySQL。业务服务 JSON 不得重复配置，同一进程也不能绑定两个不同控制面库。`NewWebServer` 在监听前初始化 Core 的目录、菜单、按钮、角色、权限、管理员主体和主体角色关系；不公开控制面 action，也不需要消费方 `ConfigureStorage`。SQLite 库名切换或 SQLite 到 MySQL 的既有数据迁移不由 Core 自动执行。

## v1.2.2 公共错误分类修复

候选补丁把未分类 Do 错误从历史误判的 HTTP 422 / `business rule rejected` 收紧为
HTTP 500 / `50000` / `internal server error`，并把标准 Manage 重复记录及数据库唯一约束
稳定映射为 HTTP 409 / `40900` / `record already exists`。显式 Business、Conflict、
NotFound、RateLimited、Unavailable PublicError 的 Kind、Code、HTTPStatus、Message 保持不变。

Bitzoom 升级后应复测 Simple-E4 Manage FuturesSpec 重复市场、普通未知数据库错误、显式业务拒绝、
跨服务公开码/消息恢复和 Admin 展示。`internal/apierrors.NormalizeServiceBoundary` 中针对 Core 旧版
`800`/泛化错误的兼容分支可在所有运行节点升级后删除；对混跑旧节点或非 Core 下游的防御性
Internal fallback 应暂时保留并降级为兜底。本文不把 Core 本地测试当作 Bitzoom smoke；消费方
实际升级、真实 MySQL/HTTP 回归和部署状态均为 `NOT RUN`，完成后再登记精确 commit 与结果。

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

2026-07-17 在 futures 精确提交 `e0bc32088b2125bb5d6e8880ef37c5b033541b5e` 上，以临时 `go.work` 指向 Core `3d6b888`，未修改 futures 的 `go.mod/go.sum`：

```bash
go test ./gateway/api/... ./internal/pkg/services/worker/... -count=1 -timeout=10m
go test ./internal/pkg/services -run '^$' -count=1 -timeout=10m
```

两条命令均退出 0。Go 源码扫描没有发现 `SocketPort`、`Transport.Socket`、`TargetSocketPort`、`SourceSocketPort` 或旧 `SetAttachService` 调用，因此源码编译迁移通过。

配置扫描发现 `docker/local/etc` 下 13 份 JSON 仍含顶层 `SocketPort`。通过 Core `ReadConfig` 加载临时 `gateway.json` 副本时，`migrateConfig` 会先移除已废弃的 Socket 字段；随后 `conf.MustLoad` 因既有 `Telemetry.Batcher=jaeger` 不属于 go-zero 当前允许值而 fail closed。该失败不是本次 Socket 删除引入，但会阻止消费方原样升级。futures 需先选择当前受支持的 telemetry exporter 并更新这些配置，再复跑迁移后的完整配置 smoke。

旧 futures 工作树仍含 `SocketPort` 与 `Telemetry.Batcher=jaeger`，因此必须继续锁定 `v0.0.247`，不得升级到 Core v1。它是旧系统对照，不作为已完成 gRPC 迁移的 Bitzoom v1 候选证据。

## Bitzoom v1 候选刷新

2026-09-06 在干净的 Bitzoom `main` 精确提交 `4b2b18a8298d47ec1edd3f8dabd1d7c086a2d49c` 上验证。当前提交已不含旧 futures 的 `SocketPort` 或 `Telemetry.Batcher=jaeger` 配置，`bootstrap/config_test.go` 明确断言内部传输为 gRPC；full-e4 gRPC 发布链可追溯到 `110779fa`。

为遵守 Bitzoom 不使用本地 `go.work`/`replace` 的规则，从已提交树创建临时 archive，仅在临时副本执行：

```bash
go get github.com/digitalwayhk/core@e7cf45a7e9dbc138ac737a88957a9a88e5201698
go test ./bootstrap ./contract/... -count=1 -timeout=10m
go test -exec=/usr/bin/true ./... -count=1 -timeout=15m
```

解析出的候选版本为 `v0.0.248-0.20260906080609-e7cf45a7e9db`；bootstrap、全部 contract 行为测试和全仓 Go 编译均退出 0。archive 不含 `.git` 与三个前端/Casdoor 子模块，因此依赖这些仓库元数据的 architecture 行为测试不作为本次证据；需要真实 MySQL/Redis 的 fullsystem、orderedtradefill 与用户市场 UAT 也明确为 `NOT RUN`，不以编译替代。

本次证据证明 Bitzoom 已跨过 Socket→gRPC 的源码、配置与全仓编译迁移边界，解除 Core v1 tag 的消费方配置阻断。旧 futures 仍停留在 `v0.0.247`；若未来迁移，必须独立修改配置并重跑其完整 smoke。

## 可信内部调用方的消费方迁移

采用 `router.WithInternalCallers` 后，受限 Public 的 HTTP 直连不再是兼容调用方式。消费方应直接构造目标服务注册的 API，通过 `req.CallService` 进入 Core Resolver：同进程由源 ServiceContext 提供身份，跨进程由 mTLS 客户端证书 SAN 绑定 `SourceService`。不得通过 Header、请求字段、静态地址 client 或仅自报服务名绕过。

消费方 smoke 至少覆盖：允许服务成功、错误服务在 Parse 前拒绝、普通 HTTP 拒绝、无证书/错误 SAN 拒绝，以及重试写请求具备业务 `requestID` 幂等。该变化是加性的安全能力；一旦现有路由增加白名单，HTTP 可达性会收紧，必须随迁移说明和路由兼容快照一起发布。

## OpenAPI HTTP 入口迁移

旧 `/api/servermanage/openapi` 已按 `openapi-audience-split-v1` 批准删除。只需普通 Public/Private 描述的工具改用匿名 `GET /api/openapi`；需要内部专用 Public 和 `x-internal-callers` 的联调工具改用 `GET /api/internal/openapi`，携带 ServerManage 域 Token，并可通过 `service` query 筛选服务。

消费方不得继续探测旧路径，也不得把匿名文档缺少内部路由视为接口删除。发布前应分别验证匿名文档不泄露内部调用方，以及内部身份能读取完整文档；当前已登记消费方未声明对旧 OpenAPI HTTP 路径的直接依赖，后续发现实际调用方时必须补充其精确 commit 和 smoke 证据。

## 稳定实例身份候选的消费方门禁

本轮只修改 Core 及其 06/07 示例，没有升级、重启或迁移 Bitzoom/futures 等消费方。消费方实测状态为 NOT_RUN；既有表中的锁定不能当成本候选的兼容证据。

升级前先盘点实际 WorkerID 编码、容量/别名、原身份、可信完整历史时间上界及 pending 路径；旧值冲突或无法证明时停止 stable 迁移。为每个副本准备独占持久卷、显式独立编号权威和运维恢复流程。不得把既有事件消费组按新 InstanceID 自动改名，不回填历史实例字段。先在隔离部署验证断网超过 TTL 的真实调用恢复、原卷重启、硬崩溃拒绝抢占、授权 recover 和退出不复活，再固定精确 Core commit。具体步骤见 [指南](STABLE_INSTANCE_IDENTITY_GUIDE.md)。
