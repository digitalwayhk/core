# 已完成任务实现审查报告

| 字段 | 值 |
|------|-----|
| 审查日期 | 2026-07-11 |
| 分支 | `codex/optimize-code-cleanup` |
| HEAD | `e8330c0`（`fix: count confirmed persistence syncs`） |
| 审查依据 | `docs/codex/PROJECT_REVIEW_ACTION_PLAN.md` 完成跟踪表；`docs/codex/plans/11-*.md`、`12-*.md`、`13-*.md` 中已勾选完成的实现单元 |
| 审查方式 | 只读：对照计划验收项与源码/提交；并行 explore 代理 + 关键路径人工复核 |
| 代码修改 | **无**。本文件仅为审查与修复建议，不包含实现变更 |
| 工作区备注 | 审查时存在未提交的 SharedBadger 相关 WIP（`sharedbadger.go` 等）。**结论以已提交代码（至 `e8330c0`）为主**；WIP 不计入“已完成任务”验收 |

---

## 1. 范围说明

### 1.1 总计划中标记为「已完成」的任务

| 任务 | 状态 | 主要提交（计划记载） | 对应聚焦计划 |
|------|------|----------------------|--------------|
| 1. 依赖升级隔离 | 已完成 | `f72447f` | 总计划正文 |
| 3. 测试命令脚本 | 已完成 | `0d29df1`（后续扩展见 §3） | 总计划正文 |
| 11. 安全基线与认证隔离 | 已完成（含审查后 A–E） | `804a2de`…`307f44e` | `plans/11-security-auth-isolation.md` |
| 12. 请求隔离、全局状态与生命周期 | 已完成 | `60b6e3a`…`f0f70ae` | `plans/12-request-lifecycle-concurrency.md` |

### 1.2 部分完成（总计划仍为「进行中」）

| 任务 | 已勾选完成的子项 | 未完成子项 | 对应计划 |
|------|------------------|------------|----------|
| 13. 持久化正确性与外部测试分离 | 13.1a–c、13.2a–b、13.3a/b/d | 13.3c fatal-break、13.4 Docker | `plans/13-persistence-correctness.md` |

任务 13 **不得**按「整任务已完成」关闭；下文仅审计**已勾选**子项是否名实相符，并记录已知残留。

### 1.3 未审查（明确未开始）

任务 2、4–10、14–17 未进入本次实现验收。

---

## 2. 总评

| 任务 | 实现与计划一致性 | 可否视为“可关闭完成” | 一句话 |
|------|------------------|----------------------|--------|
| 1 | 高 | 是（文档基线需刷新） | 依赖提交边界正确 |
| 3 | 高（扩展后文档/`all` 略滞后） | 是（建议补全 `all`） | 脚本可用且未知模式 exit 2 |
| 11 | 高（Casdoor 公开错误漏检） | **条件关闭** | Logto/CORS/IP/写配置 扎实；Casdoor 与 HTML 缺口 |
| 12 | 高（关闭接线/panic 挂死） | **条件关闭** | 主路径到位；Melody 未接入 Stop；init panic 可挂 Stop |
| 13 已完成子项 | 中高 | 否（整任务未完） | CAS/双门禁/GORM Error 到位；pending 双计与验收命令有误 |

**整体结论：** 已标记完成的 1/3/11/12 在主目标上**大体兑现**，不应整体回退；但 11、12 仍有 **P1 级残留**，与计划“最终验收清单全勾选”之间存在差距。任务 13 已完成子项需带着 **pending cache** 与文档验收路径问题继续迭代，**不得**仅因 checkbox 勾选就宣称 13.3b 无残留。

---

## 3. 分任务审查

### 3.1 任务 1：依赖升级隔离

**计划目标：** 有意依赖升级单独提交 `go.mod`/`go.sum`，与业务变更隔离。

**已兑现：**

- 提交 `f72447f chore: update core dependency versions` **仅含** `go.mod`、`go.sum`
- 当前模块 `go 1.26.0`，`go-zero v1.10.2` 与总计划技术栈一致

**问题与建议：**

| 级别 | 问题 | 修复建议 |
|------|------|----------|
| P3 | 总计划「当前基线」仍写依赖状态“不干净” | 刷新基线：已隔离提交；日常保持 `go mod tidy -diff` 干净 |
| 信息 | 本审查未重跑 `go mod verify` | 发布前再跑一次并记入完成证据 |

---

### 3.2 任务 3：测试命令脚本

**计划目标：** 可重复的 `scripts/test.sh`；未知模式用法 + exit 2；不依赖 `rtk`。

**已兑现：**

- `quick` / `server` / `security` / `concurrency` / `persistence-unit` / `integration-*` 均存在
- 未知参数打印 usage 且 `exit 2`
- 无 `rtk`，可移植 bash + `go test`

**后续扩展提交（总计划任务 3 行未列出）：**

- `security`（任务 11）
- `concurrency`（任务 12，`2f70294`）
- `persistence-unit` / `integration-persistence`（任务 13.1，`b144f9a`）

**问题与建议：**

| 级别 | 问题 | 证据 | 修复建议 |
|------|------|------|----------|
| P2 | `all` 未包含 `security`、`persistence-unit` | `scripts/test.sh` `all` 分支 | 本地门禁至少串入 `security` + `persistence-unit`；外部集成保持可选 |
| P2 | `integration-persistence` 默认 `CORE_TEST_*=0`，全 skip 仍 exit 0 | 同脚本 | 13.4 接 Docker 后：无启用 env 时非 0 退出，或要求至少一项 =1 |
| P3 | 验证矩阵仍写 persistence 模式「计划中」 | `PROJECT_REVIEW_ACTION_PLAN.md` | 改为已实现并写清 `all` 组成 |
| P3 | plan 11 验收提到 security `-race`，模式本身无 race | `scripts/test.sh` security | 可选：`security` 加 `-race`，或文档写明 race 由 concurrency 覆盖重叠包 |
| P3 | `AUTOMATED_VERIFICATION_PLAN.md` 仍写 `rtk go test` | 文档 | 改为 `./scripts/test.sh` / 裸 `go` |

---

### 3.3 任务 11：安全基线与认证隔离

**聚焦计划：** `docs/codex/plans/11-security-auth-isolation.md`（各 11.x 均已勾选完成）

#### 已兑现（高置信）

| 验收项 | 实现要点 |
|--------|----------|
| 配置写权限 | `writeConfigFile`：`WriteFile`+`Chmod` → `0o600`；`Save`/`migrateConfig`（有内容变更时）使用 |
| 显式 CORS | 启用 CORS 且无 origin → error；`NewServer` 返回 error；`*` 仅显式传入 |
| Logto 策略隔离 | `AuthConfig` 值传入；`HandlerFactory` 共享 JWKS；初始化失败返回 error，非 `log.Fatal` |
| 受信代理 IP | `ClientPublicIP`：无 TrustedProxies 不信转发头；本地 + 伪造头 → 空；从右向左走 XFF |
| 通用错误（Logto / `writeErrorResponse`） | 客户端固定文案；内部原因不入 body |
| REST 安全头 | `nosniff` / `no-referrer` / `DENY` |
| REST nil Request | `RouteHandler` 对 nil → 401 |
| `scripts/test.sh security` | 覆盖 config、logto、rest、utils |

#### 问题清单

| ID | 级别 | 问题 | 位置 | 说明 |
|----|------|------|------|------|
| T11-1 | **P1** | Casdoor 认证仍向客户端泄露解析细节 | `pkg/server/safe/casdoor/authmiddleware.go:43-55` | 返回 `"ParseJwtToken() error: "+err.Error()` 及 `"authHeader is empty"` 等；REST 在 Logto 关闭时仍可挂 Casdoor。与「通用公共认证错误」验收冲突 |
| T11-2 | **P2** | 无内容迁移时配置文件 mode 不收紧 | `pkg/server/config/serverconfig.go` `migrateConfig` | `!changed` 直接 return，历史 `0666` 文件可读到 Save 才变 0600 |
| T11-3 | **P2** | HTML 管理代理未防护 nil Request | `pkg/server/run/htmlserver.go` | `NewRequest` 后无 nil 检查，认证失败路径可 panic |
| T11-4 | **P2** | 双 Logto 构造路径 | `pkg/server/trans/rest/server.go` `newLogtoHandler` | 包级函数绕过 `HandlerFactory`，存在回归风险（生产注册用 factory） |
| T11-5 | **P3** | HTML 服务未挂安全响应头 | `htmlserver.go` | 与 REST 不一致 |
| T11-6 | **P3** | JWKS map key 未规范化 issuer | `logto/authmiddleware.go` | 尾斜杠/空白可导致重复 JWKS 实例 |
| T11-7 | **P3** | 废弃 `AuthHandler` 成功路径无 Close 生命周期 | 同上 | 外部误用可泄漏 refresh goroutine |

#### 修复建议（任务 11）

1. **P1：** Casdoor 与 Logto 对齐——客户端仅 `authentication failed` / `authentication unavailable`；`err` 只进 `logx`；补披露测试。  
2. **P2：** `ReadConfig`/`migrateConfig` 在内容未变时也 `Chmod(0o600)`；补「仅 mode 过宽」测试。  
3. **P2：** HTML 路径 `if req == nil { 401; return }`。  
4. **P2：** 删除或委托包级 `newLogtoHandler`，只保留 factory。  
5. **P3：** HTML 复用 `securityHeaders`；规范化 AuthConfig key；`all` 纳入 security。

---

### 3.4 任务 12：请求隔离、全局状态与生命周期

**聚焦计划：** `docs/codex/plans/12-request-lifecycle-concurrency.md`（最终验收清单全勾选）

#### 已兑现（高置信）

| 子任务 | 实现要点 |
|--------|----------|
| 12.1 | 生产不再 `SetReq`；`GetDefaultItemsWithRequest`；AST 测试 |
| 12.2 | 上下文/测试结果注册表锁 + 快照；`ready` 占位初始化 |
| 12.3 | WebServer 实例级 once/map；typemap 安全断言 |
| 12.4 | WebSocket 回调持锁计数/channel 同步 |
| 12.5 | CrossNode forwarder 按 ServiceName；实例安全 Clear |
| 12.6a | Membership `Once`+`doneCh`；ServiceContext `lifecycleOp`；Provider 锁外 |
| 12.6b | REST/HTML 有界 `Shutdown`；HTML 实例 mux；WebServer 等 group + 业务 Stop；顶层 `proc.Shutdown` |
| 12.7 | 迁移 Watch → 单一 reconciler；Complete/Rollback 等 worker |
| 12.8 | `concurrency` 模式 + race + 生命周期 ×20 |
| 进程级 WS | 通知系统 + 周期清理挂 `proc.AddShutdownListener`，可等待退出 |
| Melody | `Close` 可等待 stats/限流 worker（**能力有，接线见缺口**） |

#### 问题清单

| ID | 级别 | 问题 | 位置 | 说明 |
|----|------|------|------|------|
| T12-1 | **P1** | `MelodyManager.Close` 未接入 REST/`WebServer` 关闭链 | `trans/rest/server.go:141-160` vs `377-379` | Stop 只关 HTTP + Logto；统计监控与限流清理 goroutine 在服务 Stop 后仍存活。进程级 `proc.Shutdown` 只收全局通知/清理，不收 per-service Melody |
| T12-2 | **P1** | `runStarted=true` 后 `initServer` panic → `Stop` 可能永久阻塞 | `run/server.go` `Start`/`Stop`/`runServiceGroup` | `runDone` 仅在 `runServiceGroup` 关闭；init panic 后 `<-runDone` 永不返回 |
| T12-3 | **P3** | 导出兼容：`Req`/`SetReq`/`TestResult` 仍在 | manage / router | 计划允许，任务 15 删除；需防止业务回退依赖 |
| T12-4 | **P3** | `FiberServer.Stop` 仍为空 | `fiberserver.go` | 计划称不在启动路径；建议标 unsupported 以免误用 |

#### 修复建议（任务 12）

1. **P1：** `rest.Server.Stop` 中类型断言关闭 `Hub`（`MelodyManager.Close`），并加「Stop 后 worker 已退出」测试。  
2. **P1：** `Start` 的 `runOnce` 全程保证 `runDone` 关闭（或 init 失败不置 `runStarted`）；panic/失败路径下 `Stop` 有界返回测试。  
3. **P3：** 任务 15 清理废弃 API；文档标明 Fiber 未接线。

---

### 3.5 任务 13：已勾选完成的子项（整任务仍进行中）

**聚焦计划：** `docs/codex/plans/13-persistence-correctness.md`  
**相关提交：** `b144f9a`、`aa6c2ad`、`e8330c0`

#### 子项状态

| 子项 | 计划勾选 | 审查结论 |
|------|----------|----------|
| 13.1a–c 默认/外部分层 + 脚本 | [x] | **基本兑现**；integration 静默 skip 为质量缺口 |
| 13.2a–b GORM Raw/Exec `.Error` | [x] | **代码兑现**；验收命令包路径错误 |
| 13.3a SyncBatchDelay 默认 100ms | [x] | **兑现** |
| 13.3b CAS 确认计数 / pending / 删除确认 | [x] | **确认路径兑现**；`pendingCountCache` multi-Set 双计残留 |
| 13.3d 零/部分/全部日志 | [x] | **日志兑现**；零确认仍 `(0, nil)` |
| 13.3c fatal-break | [ ] | **未完成**（不按失败完成计，但不可关闭任务 13） |
| 13.4 Docker | [ ] | **未完成** |

#### 问题清单（针对「已完成」勾选）

| ID | 级别 | 问题 | 位置 | 说明 |
|----|------|------|------|------|
| T13-1 | **P1** | 同 key 多次 `Set` 每次 `incrementPendingCount(1)`，队列仍一条 | `sharedbadger.go` Set ~310 | 与 CAS 只减确认数叠加后，cache 可永久 >0，ticker 空转 |
| T13-2 | **P2** | 零确认 `processSyncQueue` 返回 `(0, nil)` | `sharedbadger.go` ~1376 | 日志为失败，错误通道却“成功” |
| T13-3 | **P2** | 13.2 验收命令指向无测试的 `oltp` 包 | plan L77 | 真实用例在 `pkg/persistence/database/test` |
| T13-4 | **P2** | `integration-persistence` 默认 env=0 全 skip 绿 | `scripts/test.sh` | 易误判集成已跑 |
| T13-5 | **P2** | CAS 生产胶水（syncBatch→pending/delete/日志）e2e 偏弱 | 测试多直测 Count 包装 | 建议 pure-fake 驱动 `processSyncQueue` |
| T13-6 | **P3** | 计划仍写「六个回退用例双门禁待 fake」 | plan 完成记录 | 代码侧已有 `memoryAction`/fatal 相关默认测试演进；文档需与现状对齐 |
| T13-7 | **P3** | `badgerdbconfig` 注释仍写默认 10ms | 配置注释 | 与 DefaultSharedConfig 100ms 不一致 |

#### 修复建议（任务 13）

1. **P1：** `Set`/`BatchInsert` 仅在 sync queue key **新建**时 +1 pending；批次后可用 `GetPendingSyncCount` 对账 cache。  
2. **P2：** 非空批次且 confirmed==0 时返回 typed error（如 `ErrSyncNoProgress`）。  
3. **P2：** 修正 plan 验收命令；`integration-persistence` 无启用 env 时 fail 或明确文档。  
4. **继续 13.3c / 13.4：** fatal-break、可取消重试、Docker 健康与非静默集成。  
5. **文档：** 刷新 13 完成记录与总计划 §任务 13 正文（勿仍写「下一步 13.2」）。

---

## 4. 跨任务文档漂移

| 位置 | 陈旧断言 | 建议 |
|------|----------|------|
| 总计划「当前基线」依赖状态 | 不干净 | 改为已隔离（任务 1） |
| 总计划「竞态检查」 | 未通过 | 改为 concurrency 门禁已通过（任务 12 范围） |
| 总计划「安全默认值」 | 需加固（开项口吻） | 改为任务 11 已交付 + 列 Casdoor/HTML 残留 |
| 验证矩阵 persistence 模式 | 计划中 | 已实现 |
| 总计划 §任务 13 正文 | 下一步执行 13.2 | 与完成表对齐：下一步 13.3c + 13.4 |
| `AUTOMATED_VERIFICATION_PLAN.md` | `rtk go test` | 改 `scripts/test.sh` |

---

## 5. 汇总：按严重级别的修复建议

### P1 — 应在合入主干 / 关闭任务前处理

| # | 任务 | 项 | 建议动作 |
|---|------|-----|----------|
| 1 | 11 | Casdoor 错误体泄露 | 通用认证错误文案 + 测试 |
| 2 | 12 | Melody 未在 REST Stop 关闭 | Stop 调 `MelodyManager.Close` + 测试 |
| 3 | 12 | WebServer init panic 后 Stop 挂死 | 保证 `runDone` 关闭 / 延迟 `runStarted` |
| 4 | 13* | pendingCountCache multi-Set 双计 | 仅首次 pending +1；对账 cache |

\* 任务 13 整任务未完成；该项阻断「13.3b 无残留」的表述。

### P2 — 应排期修复

| # | 任务 | 项 |
|---|------|-----|
| 5 | 11 | 配置 mode 在无内容迁移时 chmod |
| 6 | 11 | HTML nil Request |
| 7 | 11 | 删除/委托双 Logto 构造 |
| 8 | 3/13 | `all` 含 security + persistence-unit；integration 静默 skip 策略 |
| 9 | 13 | 零确认返回 error；13.2 验收包路径；CAS e2e |

### P3 — 文档与卫生

| # | 项 |
|---|-----|
| 10 | 刷新总计划基线 / 验证矩阵 / 任务 13 正文 |
| 11 | HTML 安全头、JWKS key 规范化、Fiber 未接线说明 |
| 12 | 去掉 `rtk` 文档；SyncBatchDelay 注释 100ms |

---

## 6. 建议的回归命令（修复后验证，非本审查执行清单）

```bash
# 任务 3 / 11 / 12 门禁
bash -n scripts/test.sh
./scripts/test.sh security
./scripts/test.sh concurrency

# 任务 11 定向
go test -race ./pkg/server/safe/logto ./pkg/server/safe/casdoor ./pkg/utils ./pkg/server/config ./pkg/server/trans/rest -count=1

# 任务 12 定向
go test -race ./pkg/server/cluster ./pkg/server/router ./pkg/server/run ./pkg/server/trans/rest ./pkg/server/trans/websocket/melody ./pkg/server/types -count=1

# 任务 13 已完成子项定向（勿用错误的 oltp 包路径）
go test ./pkg/persistence/database/test -run 'TestSqliteRawReturns|TestSqliteExecReturns' -count=1
go test -race ./pkg/persistence/database/nosql -run 'TestSyncQueue_|TestSyncConfig_DefaultBatchDelay|TestIssue_BatchUpdateSyncedStatus' -count=1
./scripts/test.sh persistence-unit   # 若 database/test 全包仍有历史挂起用例，需单独记录
```

---

## 7. 审查结论

1. **任务 1、3** 可作为已完成关闭，但应刷新基线文档并扩展 `all` 的本地门禁组成。  
2. **任务 11** 主安全面（Logto、CORS、TrustedProxies、配置写 0600、REST 头/nil）**达标**；因 **Casdoor 仍泄露认证错误**，不宜无备注地写“公共认证错误全面通用化”。  
3. **任务 12** 请求隔离、注册表、迁移对账、幂等生命周期 **主路径达标**；**Melody 关闭接线**与 **init panic → Stop 挂死** 与最终验收「可重复关闭且 deadline 内完成」存在张力，建议记为完成条件下的 **必跟 follow-up**，或在关闭说明中显式接受风险。  
4. **任务 13** 整任务 **未完成**；已勾选的 13.1/13.2/13.3a/b/d **主体正确**，但 **pending 双计** 与 **验收命令错误** 说明 checkbox 与“无已知 P1 残留”不等价。  
5. 本审查 **未修改任何业务代码**；修复应由后续独立提交按 §5 优先级推进。

---

## 8. 审查元数据

| 项 | 值 |
|----|-----|
| 审查类型 | 完成后置实现审计（非单 PR diff） |
| 代理 | explore ×4（任务 1/3、11、12、13 已完成子项）+ 人工关键路径复核 |
| 产出 | 本文件 `docs/codex/COMPLETED_TASKS_IMPLEMENTATION_REVIEW.md` |
| 不包含 | 自动修码、自动提交、CI 全量重跑证明 |

如需将本报告中的 P1 项拆成可执行修复任务清单或更新总计划完成证据栏，可另开会话处理；**默认仍不修改产品代码。**
