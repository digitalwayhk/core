# 公共 API 兼容性与发布治理实施计划

> 面向智能体开发者：按小节执行 TDD。每节先建立当前行为基线或失败测试，再做最小实现和定向验收；开发完成后停止，由外部审查 Agent 只读复审。收到审查反馈并修复前，不进入下一小节。

**目标：** 明确并自动保护下游服务实际依赖的公共表面，使错误状态、路由、响应 JSON、导出 Go API、配置默认值和持久化/Manage 约定能够被稳定比较；建立可复现的版本、废弃、变更说明和发布回滚流程。

**架构原则：** 本框架继续作为 go-zero 和成熟依赖之上的轻量组装层。兼容性门禁优先采用成熟的 Go API 比较工具和现有 OpenAPI/路由元数据，不另造协议生成器。运行时错误使用稳定类型与 `errors.Is/errors.As` 分类，HTTP 边界只负责映射和安全序列化；内部原因保留给日志和错误链，不暴露给客户端。

**历史文档处理：** 已删除的 `docs/codex/CORE_RELEASE_READINESS_PLAN.md`、`docs/codex/DEPENDENT_SERVICES_RISK_PLAN.md` 和 `docs/codex/PERSISTENCE_MANAGE_COMPAT_PLAN.md` 仅作为 Git 历史输入，不恢复原文件。仍有效的要求收敛到本计划、兼容性清单和发布策略，避免重新产生多份状态文档。

## 范围与兼容边界

本任务保护以下公共表面：

- Go API：下游直接导入的 `pkg/server/types`、`pkg/server/router`、`pkg/server/config`、`pkg/persistence/entity`、`service/manage`，以及经审计确认的 Cluster/Transport/MQ 扩展点。
- HTTP API：公开、私有、Manage、ServerManage 路由的 method/path/auth，成功与失败响应 JSON、HTTP 状态、稳定错误码和安全消息。
- 配置：任务 14 能力矩阵中项目自有字段、默认值、拒绝语义和关闭态兼容规则。
- 数据与生命周期：`Model`/`BaseModel`/`ModelList`、Manage CRUD/hook、自定义 Operation、服务启停及 Provider 扩展入口。
- 发布：SemVer 规则、废弃窗口、迁移说明、changelog、tag、回滚和下游依赖锁定。

本任务不包含：

- 不恢复已删除的三份旧计划，也不把它们重新作为执行状态来源。
- 不顺带升级依赖、重构日志、实现新 Provider，或改变任务 14 的配置能力结论。
- 不保证未登记的内部包、未导出符号、测试 hook、运行时 Host/端口和非确定性示例值兼容。
- 不在本任务内搭建完整 CI；任务 15只提供可由任务 16 调用的确定性命令和产物。

## 已确认的当前代码事实

- `pkg/server/types.TypeError` 已携带阶段码 600/700/800，但缺少稳定公共错误分类、`Unwrap` 和 HTTP 映射契约。
- `pkg/server/trans/rest/error.go` 通过英文/中文错误文字匹配 HTTP 状态，本地化或内部文案变化会改变公共行为。
- 默认响应仍是 `pkg/server/router.Response`，包含 `traceid/errorCode/errorMessage/success/duration/data/host/showType`；兼容改造必须先锁定现状，不能直接替换 JSON 结构。
- `pkg/server/run.GetOpenApi` 已能生成 OpenAPI，但当前包含 Host、端口和进程级测试结果等环境输入，需要规范化后才能作为快照。
- 路由 method/path/auth 的权威数据来自 `types.RouterInfo` 和 `router.ServiceRouter`，不应另写一套路由发现逻辑。
- 仓库已有连续 `v0.0.x` tag，当前最新历史 tag 为 `v0.0.247`，但没有根级 `CHANGELOG.md`、发布策略或自动兼容门禁。
- `ManageService.Req/SetReq/IRequestSet`、进程级 CrossNode 入口和部分初始化状态 API 已有 `Deprecated` 注释，尚无统一废弃期限与删除规则。

## 15.1 建立公共兼容性清单与基线

**状态：** 已完成并通过外部复审（`1cd1c90`, `eb71276`）。

**文件：**

- 创建：`docs/codex/API_COMPATIBILITY_SURFACE.md`
- 创建：`internal/compat/testdata/routes.golden.json`
- 创建：`internal/compat/testdata/openapi.golden.json`
- 创建：`internal/compat/route_snapshot_test.go`
- 创建：`internal/compat/openapi_snapshot_test.go`
- 修改：`scripts/test.sh`

**实施要求：**

- 按 Go API、HTTP、配置、持久化/Manage、生命周期五类登记公共入口、owner、消费方、兼容级别和证据；以代码和测试为准，不复制旧计划中的 TODO 状态。
- 路由快照直接从生产 `RouterInfo/ServiceRouter` 构造路径读取 method、path、path type、auth 和 service，不手工维护第二套路由列表。
- OpenAPI 快照固定 scheme/host/port，移除 duration、host、trace id 等运行时值，稳定排序 paths、methods、tags、servers、schemas 和 security。
- 将任务 14 的配置矩阵作为配置表面的唯一来源，只在兼容清单中链接，不复制字段表。
- `scripts/test.sh api-compat` 运行兼容清单、路由和 OpenAPI 基线测试；未知模式继续以代码 2 失败。

**测试场景：**

1. 同一组服务重复生成路由/OpenAPI，字节级结果一致。
2. 改变请求 Host、服务启动端口或 map 插入顺序，不改变规范化快照。
3. method/path/auth/security/request schema/response schema 任一变化都会产生可审查 diff。
4. 空服务集合不会 panic，并生成合法的空文档或明确错误。
5. 重复 operation ID、method+path 冲突和不可构造 RouterInfo 必须失败，不能静默覆盖。

**验收：**

```bash
./scripts/test.sh api-compat
go test ./internal/compat -count=20
```

**开发记录（2026-07-12）：** 已建立当前公共 Go/HTTP/配置/数据与生命周期表面清单；新增从生产 `ServiceRouter` 和 `run.GetOpenApi` 生成的确定性路由/OpenAPI golden。快照拒绝跨服务 method+path 冲突，规范化 Host、端口和响应运行时 example，普通测试不自动覆盖 golden；生产 OpenAPI 零服务场景不再访问空 `Servers[0]`。`api-compat`、20 次重复、竞态测试及 `pkg/server/run` 全包均通过，等待外部只读审查。

**外部审查修复（2026-07-12）：** 首轮裁定为 CHANGES_REQUIRED。OpenAPI golden 改用真实 `api/public` 与 `api/private` fixture，经 `DefaultRouterInfo` 锁定默认路径、method 和 auth，并纳入 private Bearer security、POST requestBody、请求 schema 与响应 schema。`SnapshotOpenAPI` 在调用生产生成器前拒绝 nil service、重复 method+path 和重复 operationId，避免 kin-openapi 静默覆盖。兼容清单补充 config/event/persistence types/utils/proto 分级并明确 Manage/ServerManage 不在当前 OpenAPI 输出范围；`api-compat` 校验清单存在，run 包已有匹配的零服务 OpenAPI 测试，usage 已登记新模式。全部定向、重复、race 和 run 回归通过，等待外部复审。

**复审结论（2026-07-12）：** APPROVED。首轮 P1-1、P1-2 与全部 P2 均为 RESOLVED，无新增 P0/P1/P2；`api-compat`、20 次重复、race、run 全包和未知模式代码 2 均通过，允许进入 15.2。

**外部审查重点：** 清单是否遗漏实际导入面；快照是否来自生产元数据；规范化是否掩盖真正的破坏性变化；是否误把运行时噪声纳入契约。

## 15.2 建立类型化公共错误契约

**状态：** 已完成并通过外部审查（`25d3770`）；依赖的 15.1 响应基线已 APPROVED。

**文件：**

- 修改：`pkg/server/types/typeerror.go`
- 创建或修改：`pkg/server/types/typeerror_test.go`
- 修改：`pkg/server/router/reponse.go`
- 修改：`pkg/server/trans/rest/error.go`
- 创建或修改：`pkg/server/trans/rest/error_test.go`
- 保留并扩展：`pkg/server/trans/rest/error_security_test.go`
- 修改：`docs/codex/API_COMPATIBILITY_SURFACE.md`

**决策：**

- 定义稳定的公共错误类别和码，例如 validation、unauthenticated、forbidden、not_found、conflict、business、rate_limited、unavailable、internal；类别到 HTTP 状态和安全默认消息为固定表。
- 保留 `TypeError`、`NewTypeError` 及现有 600/700/800 阶段码的源码兼容。新增能力采用加性字段、构造器或包装类型，禁止直接改变已有函数签名。
- 类型化错误通过 `errors.Is/errors.As` 识别并保留内部 cause；REST 边界不得再以错误文字决定状态。普通未分类错误默认映射为 500 和通用安全消息，而不是把内部文本作为 422 返回。
- 默认 `router.Response` 的 JSON 字段名先保持兼容。若稳定公共错误码与历史阶段码不能共用 `errorCode`，必须先在兼容清单中记录加性迁移方案，不能静默改义。
- 日志记录内部 cause 由任务 8 统一治理；本节只保证客户端不泄露内部错误、token/JWKS/SQL/路径/堆栈。

**测试场景：**

1. 相同类型化错误在错误文字、本地化和包装层级变化后仍返回相同 HTTP 状态、公共码和安全消息。
2. `errors.Join` 或多层 `%w` 包装后仍可识别目标类别。
3. 未分类错误返回 500，响应体不包含内部原因。
4. 600/700/800 历史阶段错误的 Go 构造方式和 JSON 字段保持基线兼容。
5. auth、权限、not found、conflict、validation、business、rate limit、dependency unavailable 的表驱动映射完整。
6. 自定义 `INewResponse` 仍由服务拥有；框架只依赖 `IResponse` 契约，不强制转换为默认响应类型。

**验收：**

```bash
go test ./pkg/server/types ./pkg/server/router ./pkg/server/trans/rest -count=20
go test -race ./pkg/server/types ./pkg/server/router ./pkg/server/trans/rest -count=1
./scripts/test.sh security
```

**开发记录（2026-07-12）：** 新增稳定 `ErrorKind`、公共码、HTTP 状态和安全默认消息，`PublicError` 支持 `%w`、`errors.Join` 与 `errors.Is/As`；未分类错误固定 fail closed 为 500。保留 `TypeError/NewTypeError` 和 600/700/800 阶段码，新增 `NewTypeErrorWithCause` 让 RouterInfo 保留原始错误链。REST 不再按字符串分类，默认 `router.Response` 通过加性的 `ISetPublicError` 写回安全码/消息，自定义响应所有权不变。types/router/rest 定向重复、完整 race、security 与 api-compat 均通过，等待外部只读审查。

**外部审查结论（2026-07-13）：** APPROVED，无 P0/P1，允许进入 15.3。审查确认类型化映射、错误链、fail-closed、历史阶段码、可选接口和默认 REST 脱敏均符合规格。非阻断 P2：`Response.GetError` 仍会在非 REST 调用路径写入原始 TypeError 文本，后续安全清理应消除该副作用；`determineStatusCode` 为可删除的副作用 helper；router `-count=20` 偶发挂起属于既有生命周期测试稳定性，交由任务 16 单独治理。

**外部审查重点：** 是否存在源码/JSON 破坏；未分类错误是否 fail closed；内部 cause 是否泄露；HTTP 映射是否仍依赖字符串；自定义响应实现是否被误伤。

## 15.3 建立导出 Go API 漂移门禁

**状态：** 未开始，依赖 15.1 的公共包清单。

**文件：**

- 创建：`api/public-api.txt`
- 创建：`scripts/check-public-api.sh`
- 创建：`scripts/update-public-api.sh`
- 修改：`scripts/test.sh`
- 创建：`tools/go.mod` 或等价工具锁定文件（仅在工具评估确认需要时）
- 修改：`docs/codex/API_COMPATIBILITY_SURFACE.md`

**决策：**

- 优先评估并固定成熟的 Go API 比较器；首选 `golang.org/x/exp/cmd/apidiff` 或维护状态更合适的等价工具。工具版本必须锁定，不进入运行时 `go.mod`，不得手写不完整的 AST 导出扫描器。
- 基线只覆盖 15.1 登记的公共包。新增 API 允许但必须进入 diff；删除、改签名、收紧接口、改变可见 struct 字段类型等不兼容变化默认失败。
- 生成基线与检查基线必须分成两个明确命令。普通测试绝不自动覆盖 golden；更新必须伴随变更说明和审查。
- generated protobuf 等上游生成表面要么纳入并由生成器版本锁定，要么在清单中明确排除理由，不能静默忽略。

**测试场景：**

1. 干净基线检查通过且重复运行不修改工作区。
2. 删除导出函数、改变参数/返回值、删除公开 struct 字段、向下游实现的接口增加方法均被判为破坏性。
3. 新增导出 API 被报告为加性变化，不被无声吞掉。
4. 工具不可用、版本漂移、基线缺失或目标包无法加载时返回非零状态。

**验收：**

```bash
./scripts/test.sh public-api
./scripts/check-public-api.sh
git diff --exit-code -- api/public-api.txt
```

**外部审查重点：** 工具是否成熟并锁定；公共包范围是否合理；生成和检查是否严格分离；是否存在绕过破坏性变更的排除规则。

## 15.4 建立废弃、变更说明与发布流程

**状态：** 未开始，依赖 15.1-15.3 的可执行产物。

**文件：**

- 创建：`CHANGELOG.md`
- 创建：`docs/RELEASE_POLICY.md`
- 创建：`docs/codex/DEPRECATION_REGISTER.md`
- 创建：`docs/codex/CONSUMER_COMPATIBILITY_MATRIX.md`
- 创建：`scripts/release-check.sh`
- 修改：`scripts/test.sh`
- 修改：`README.md`
- 修改：`.codex/skills/use-digitalway-core/references/core-backend-api.md`
- 修改：`.github/copilot/skills/core-backend-api.md`

**决策：**

- 采用 SemVer。`v0.x` 阶段仍将已登记公共表面的破坏视为需显式迁移和批准的变更，不以“尚未 1.0”为由静默破坏。
- changelog 使用 Keep a Changelog 风格的 `Unreleased/Added/Changed/Deprecated/Removed/Fixed/Security` 分组，并记录配置和行为兼容影响。
- 废弃登记至少包含 API、替代入口、首次废弃版本、最早删除版本、消费方、迁移测试和 owner。现有 `ManageService.Req/SetReq/IRequestSet`、旧 CrossNode 全局入口及初始化兼容 API先纳入登记，不在本任务删除。
- 发布检查验证工作区、版本/tag、changelog、API/路由/OpenAPI/config-contract/security 门禁和迁移说明；脚本只检查和报告，不自动推 tag 或发布。
- 下游引用生产发布时必须锁定 tag 或 commit；移动分支仅用于开发验证。消费方矩阵记录实际仓库路径、当前 core 版本/commit、Go 工具链和可执行 smoke 命令，不保留“待确认”占位后声称完成。
- futures、omni-flow、ai-ops 若本地不存在或不使用 Go module，必须记录 `not-applicable` 及证据；不得伪造通过结果。

**测试场景：**

1. changelog 缺少 `Unreleased`、破坏性变化缺迁移说明、废弃项缺删除窗口时发布检查失败。
2. tag 与声明版本不一致、工作区有非预期修改、API/路由/OpenAPI golden 漂移时失败。
3. 下游 smoke 命令缺失、引用移动分支作为生产基线或工具链不兼容时失败并给出项目名。
4. 回滚演练能从目标 tag/commit 恢复，并能重新执行相同兼容门禁。

**验收：**

```bash
./scripts/test.sh release-contract
./scripts/release-check.sh
```

**外部审查重点：** 发布流程是否可复现；是否存在自动推送/tag 的危险副作用；废弃窗口是否可执行；消费方证据是否来自真实仓库和精确版本。

## 15.5 总验收与任务 16 交接

**状态：** 未开始，依赖 15.1-15.4 全部通过外部审查。

- 将 `api-compat`、`public-api`、`release-contract` 模式接入总验收，但保持任务 16 才负责 CI workflow、required checks 和失败产物上传。
- 更新 `docs/codex/PROJECT_REVIEW_ACTION_PLAN.md`：记录每小节提交、外部审查结论、命令与耗时。
- 任务 15 的最终外部审查范围必须从任务 14 完成提交之后的基线到任务 15 最终提交，检查跨小节兼容性，而不只看最后一个 diff。

**最终验收：**

```bash
./scripts/test.sh api-compat
./scripts/test.sh public-api
./scripts/test.sh release-contract
./scripts/test.sh security
./scripts/test.sh config-contract
go test ./pkg/server/... ./pkg/persistence/... ./service/manage/... -count=1 -timeout=10m
```

## 每小节外部审查反馈格式

开发 Agent 每节完成后提供以下提示词变量，不自行宣称 APPROVED：

- 审查范围：`git diff <本节基线>..<本节提交>`。
- 规格：本计划对应小节。
- 必查项：公共 Go/HTTP/JSON/配置兼容性、错误泄露、确定性、测试强度、工具锁定和范围外修改。
- 审查约束：只读，不修改文件；按 P0/P1/P2 输出，并明确 `APPROVED` 或 `CHANGES_REQUIRED`。

外部审查反馈必须包含：

1. 实际执行命令、退出码和关键结果。
2. 每条 finding 的文件、行号、失败场景、严重级别和建议修复方向。
3. 是否存在破坏性 API/路由/JSON/配置变化。
4. 最终裁定，以及允许进入的下一小节编号。

## 完成定义

- [ ] 公共兼容性清单来自当前代码和真实消费方证据。
- [ ] HTTP 状态不再由错误文字决定，客户端不暴露内部原因。
- [ ] 路由、OpenAPI 和导出 Go API 均有确定性、可审查、默认不覆盖的基线。
- [ ] 配置契约继续由任务 14 矩阵唯一维护，不产生重复字段清单。
- [ ] 现有废弃 API 有版本、替代方案、迁移测试和最早删除窗口。
- [ ] changelog、发布检查、回滚和消费方锁定可复现。
- [ ] 每个小节均有独立提交、定向测试和外部审查结论。
- [ ] 总计划已记录任务 15 的提交哈希与最终门禁证据。
