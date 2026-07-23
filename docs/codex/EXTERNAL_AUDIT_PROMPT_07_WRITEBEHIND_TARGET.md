# 示例 07 WriteBehindTarget 外部全面审计提示词

请对 `github.com/digitalwayhk/core` 当前分支做一次外部全面代码审计。审计必须以当前代码为准，不要只根据 README、历史计划或测试全绿下结论。

## 1. 审计范围

- 分支：`codex/optimize-code-cleanup`
- 重点提交：`9223adc feat(persistence): add write-behind target`
- 对比基线：优先对比上一个已完成提交 `d869bcd test(example): strengthen 07 docker role UAT`，必要时再对比 `main`
- 核心目录：
  - `pkg/persistence/database/nosql`
  - `examples/04-shop-performance`
  - `examples/07-shop-order-scale`
  - `examples/integration/07-shop-order-scale`
  - `examples/integration/07-shop-order-scale-multi-process`
  - `.codex/skills/use-digitalway-core`
  - `docs/codex`

## 2. 背景和目标

本轮目标是把示例 04/07 中业务层手写的本地可靠写、write-behind、远程汇合、ACK 清理等复杂逻辑收敛到框架层：

1. `PrefixedBadgerDB` 新增 `UseWriteBehind(WriteBehindTarget)`，业务热路径直接绑定专用远端汇合目标。
2. `EnableWriteBehind(ModelList)` / `SetSyncDB` 仅作为兼容层，不再作为高 TPS 业务新实现的默认方案。
3. 示例 04 改为通过 `NewModelListWriteBehindTarget(list)` 兼容旧 `ModelList` 汇合。
4. 示例 07 改为通过 `OrderWriteBehindTarget` 在 MySQL 远程权威库中完成订单幂等汇合和 Outbox 同事务写入。
5. 业务 API 只负责写本地 durable pending 并触发同步，不再手工实现 pending 扫描、远程写入、Outbox 写入和本地删除闭环。

请判断当前实现是否真正达成以上目标，有没有数据丢失、重复事件、幂等漂移、并发竞态、测试缺口、文档/能力误导或旧实现残留。

## 3. 必查设计契约

请按以下契约审计，不符合时给出证据、影响和修复建议：

1. 高 TPS 业务写路径不能围绕 `ModelList` / Manage API / SQLite 表轮询实现；只能通过 business + 专用 store + `PrefixedBadgerDB.UseWriteBehind(WriteBehindTarget)`。
2. Badger pending 是未同步业务事实，不是缓存；本地持久成功后才能向 API 返回成功。
3. 远端 target 成功后才能 ACK 本地 pending；target 失败时 pending 必须保留并可重试。
4. 远端成功但本地 ACK 失败时允许 at-least-once 重试，因此远端汇合必须幂等。
5. 业务事实与 Outbox 必须同一个远端事务写入，不能 API 层二次写 Outbox。
6. 示例 07 分布式/Docker 下 order 远程权威库必须是真共享 MySQL 等网络数据库，不能用每进程本地 SQLite 冒充共享 remote。
7. 示例 07 必须支持 `AutoMachineID=true`，多副本必须有唯一 `MachineID` 和 `ServiceInstanceID`。
8. Docker/多进程 UAT 必须按角色拆分：买家、供应商、管理员均应有可单独运行的角色闭环测试；如果角色需要数据，必须通过对应角色 fixture 准备，不允许在测试中伪造业务前置条件。
9. 有 WebSocket 的角色必须在集成测试和 UAT 中覆盖真实登录、订阅、事件投递、身份隔离和异常边界。
10. 示例、测试、能力文档和 README 必须使用中文，并保持当前能力一致；不能继续推荐旧的高 TPS `EnableWriteBehind(ModelList)` 写法。

## 4. 重点审计项

### 4.1 `PrefixedBadgerDB` / `WriteBehindTarget`

请重点检查：

- `WriteBehindTarget` / `WriteBehindResult` 接口是否足够表达业务汇合、确认和保留 pending 的语义。
- `UseWriteBehind` 是否正确配置 `SyncWrites`、`DetectConflicts`、`CorruptionPolicyFail` 等安全参数。
- `syncTarget` 与旧 `syncList` 的优先级是否清晰，是否存在两套目标同时生效或被意外覆盖。
- `syncBatchWithTarget` 是否只确认 target 明确成功的 key。
- `confirmSyncSuccess` 是否可能误删未同步数据，尤其是 delete tombstone、部分成功、target 返回重复 key 或空 key 的场景。
- `ForceSyncAll` 是否会可靠重建待同步队列，避免历史 pending 因内存队列丢失而永不重试。
- `PendingSyncError` 在 `Close` / `Flush` / 测试中的语义是否合理，是否会掩盖真实失败。
- 后台 worker、`TriggerSync`、`ForceSyncAll`、`Close` 并发时是否存在竞态、死锁、重复 ACK 或漏 ACK。
- 现有 `AutoSync` 行为是否与文档一致；如果历史实现就未严格使用，也请判断当前行为是否需要文档化或修复。

### 4.2 `ModelListWriteBehindTarget` 兼容层

请确认该兼容层是否安全保留旧 `EnableWriteBehind(ModelList)` 语义，至少要覆盖：

- `Insert` / `Update` / `Delete` 的批量分组逻辑是否等价于旧实现。
- `GetModelDB`、事务边界、批量失败 fallback、hash/unique 冲突处理是否正确。
- delete 后确认、`IsSyncAfterDelete`、tombstone 清理是否与旧行为一致。
- 是否会把兼容层误导成高 TPS 新业务默认方案。
- 示例 04 使用它是否合理，是否仍保持 04 的性能优化示例定位。

### 4.3 `SQLWriteBehindTarget`

请检查：

- 是否能作为通用 SQL target 安全使用。
- 按 operation 分组、事务边界、确认 key 映射是否正确。
- SQL 写入成功后确认所有 key 是否存在隐藏风险。
- 错误处理是否会导致部分成功后全量重试，并要求业务 target 幂等。

### 4.4 示例 04 迁移

请审计：

- `examples/04-shop-performance/models/order_write_store.go` 是否只做本地可靠写和触发框架同步。
- `UseWriteBehind(NewModelListWriteBehindTarget(list))` 是否替代旧手写同步，同时不改变 04 对 SQLite/ModelList 的兼容演示目标。
- `Flush`、`RemoveLocal`、`IsSyncAfterDelete`、group commit、pending 保留、性能 benchmark 是否仍符合示例 04 的能力定位。
- benchmark 是否继续使用 04 模式：计时外准备数据，`ReportAllocs`、`ResetTimer`、按能力拆分，不把建表/fixture/远程探测计入热路径。

### 4.5 示例 07 迁移

请审计：

- `OrderWriteBehindTarget` 是否通过 `UserID+RequestID` 做幂等汇合，返回稳定 OrderID，并防止重复订单和重复事件。
- `OrderWriteBehindTarget` 是否在同一个 MySQL 事务里写远程订单事实和 Outbox。
- Public 下单、撤单、支付路径是否没有二次写 Outbox。
- `RemoteOrderSyncer` 是否只是触发框架同步，不再手工删除 local pending 或重复实现同步闭环。
- `models/transaction.Order.IsSyncAfterDelete() == true` 是否必要且实现正确。
- `service.Start` 是否在接受真实订单前绑定 write-behind target。
- MySQL 不可达时下单策略是否明确：fail-closed 或降级纯本地必须与 README 一致。
- Docker scale 下是否确实共享 MySQL，而不是本地 SQLite。
- 多 order 副本下同一 `requestID` 是否返回稳定 OrderID；如果依赖 user-service 进程内映射，请判断残余风险是否已文档化。
- `ServiceInstanceID`、`ServiceInstanceIP`、`MachineID`、`TraceID` 是否写入关键业务事实、pending、Outbox/事件，并足以诊断多副本问题。

### 4.6 事件、缓存和 WebSocket

请检查：

- OrderCreated / OrderStatusChanged / PaymentChanged 等事件是否只产生一次语义事件。
- Outbox `EventID` 是否是事件幂等键，不把 TraceID 当 EventID 使用。
- EventBridge/Outbox 是否符合 `sc.UseOutbox(models.OutboxStore{})` 的发布模型：发布方不关心消费者是谁。
- User/Supplier 的 Inbox 幂等、缓存失效和 WebSocket 推送是否不会跨用户串线。
- 07 从本地 pending 到远程 MySQL 到 Outbox 到 User WebSocket 的链路是否有数据丢失或重复推送风险。

### 4.7 Docker 多进程 UAT

请审计：

- `examples/integration/07-shop-order-scale-multi-process` 是否真的覆盖 Docker 下多 order 副本、共享 MySQL、服务发现、`AutoMachineID=true`。
- 买家、供应商、管理员三类角色是否各有单独可运行的 UAT 测试，并形成角色闭环。
- 买家 UAT 是否覆盖 WebSocket 订单订阅和其他买家隔离。
- Docker UAT 默认通过环境变量门控是否合理；README 是否给出明确运行命令。
- 是否有必要补充非 Docker 的双 order 真实进程测试，避免 CI 默认跳过后漏掉扩容回归。

### 4.8 文档和能力文件

请检查以下文档是否与当前实现一致：

- `.codex/skills/use-digitalway-core/SKILL.md`
- `docs/codex/FRAMEWORK_USAGE_GUIDE.md`
- `examples/04-shop-performance/README.md`
- `examples/07-shop-order-scale/README.md`
- `docs/superpowers/plans/2026-07-19-prefixed-badger-writebehind-target.md`

重点确认：

- 不再把 `EnableWriteBehind(ModelList)` 描述为高 TPS 新业务默认方案。
- 不再暗示 order 水平扩展可以使用每副本本地 SQLite 作为共享 remote。
- 不再推荐业务层手写 worker、pending 扫描、Outbox 二次写入或手动 ACK。
- 历史计划和审计提示词如果保留旧说法，必须明确是历史证据，不是现行规范。

## 5. 建议运行的验证命令

请至少尝试运行或等价验证以下命令；如果环境限制导致无法运行，请说明限制和未覆盖风险。

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/persistence/database/nosql -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/04-shop-performance/... -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/07-shop-order-scale/... ./examples/integration/07-shop-order-scale ./examples/integration/07-shop-order-scale-multi-process -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/persistence/database/nosql -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./examples/07-shop-order-scale/order-service/business ./examples/integration/07-shop-order-scale-multi-process -count=1
SHOP_RUN_DOCKER_UAT=1 GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale-multi-process -run '^(TestDockerUATBuyerRoleFlow|TestDockerUATSupplierRoleFlow|TestDockerUATAdminRoleFlow|TestDockerComposeOrderScaleUAT)$' -count=1 -v
rtk git diff --check
```

如果 Docker UAT 时间较长，可以单独运行：

```bash
SHOP_RUN_DOCKER_UAT=1 GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale-multi-process -run '^TestDockerUATBuyerRoleFlow$' -count=1 -v
SHOP_RUN_DOCKER_UAT=1 GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale-multi-process -run '^TestDockerUATSupplierRoleFlow$' -count=1 -v
SHOP_RUN_DOCKER_UAT=1 GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale-multi-process -run '^TestDockerUATAdminRoleFlow$' -count=1 -v
SHOP_RUN_DOCKER_UAT=1 GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale-multi-process -run '^TestDockerComposeOrderScaleUAT$' -count=1 -v
```

## 6. 旧实现残留专项搜索

请用 `rg` 或等价方式检查是否仍存在以下危险残留：

- 高 TPS 下单、支付、撤单路径直接调用 `ModelList` 写入。
- 07 业务层手写 pending 扫描、手写 remote sync worker、手写 local pending 删除闭环。
- Public API 成功后再次写 Outbox。
- `EventID=RequestID` 或 `EventID=TraceID` 的订单事件。
- `GetGlobalSqliteInstance(common.RemoteDatabaseName)` 用作 07 共享远程权威库。
- Docker scale 固定宿主机端口或固定 `MachineID`。
- user/supplier/order 权限边界退回旧 `api/call` 或无 `WithInternalCallers` 的内部 Public。

## 7. 分级标准

请按以下级别输出问题：

- P0：确定会导致数据丢失、全局幂等破坏、跨租户/跨用户越权、内部 API 可绕过、Docker 多副本核心路径完全不可用。
- P1：生产级可靠性或边界问题，可能导致重复订单、重复事件、pending 永久卡死、状态成功但事件丢失、扩容语义不成立。
- P2：重要设计风险、测试缺口、文档误导、可恢复但需要明确的残余风险。
- P3：清理、命名、注释、可读性、低概率边界或非阻断优化。

每个问题必须包含：

| 字段 | 要求 |
| --- | --- |
| 级别 | P0/P1/P2/P3 |
| 位置 | 文件路径 + 函数/方法/大致行号 |
| 证据 | 代码或测试证据，不要只写推测 |
| 影响 | 说明对数据、幂等、事件、权限、扩容或可维护性的影响 |
| 触发 | 最小触发条件或复现场景 |
| 修复 | 推荐修复方案，尽量给出最小改动 |
| 类型 | confirmed bug / design risk / doc mismatch / test gap / acceptable residual risk |

## 8. 请额外确认的“未发现问题”清单

如果没有发现，也请明确写“未发现”并给出依据：

1. 没有外部 HTTP/Header/SourceService 自报绕过内部 Public 的路径。
2. 没有 order Public 受限路由缺失 `WithInternalCallers`。
3. 没有 Supplier/User/Order 退回旧 `api/call`。
4. 没有 07 高 TPS 订单热路径把 Manage/ModelList 当主写路径。
5. 没有 07 手动删除 pending 导致 target 失败后数据丢失。
6. 没有撤单/支付二次写 Outbox。
7. 没有 Docker 多副本使用非共享 remote。
8. 没有 WebSocket 跨用户投递。
9. 没有缓存放在内部权威服务并与入口 facade 双层缓存冲突。
10. 没有新增英文文档或违背中文注释契约的新增 public API。

## 9. 已知规划边界

以下能力属于后续生产级增强规划，不要仅因为当前 Phase 1 未实现就直接判 P1；但如果当前文档承诺“已具备”，请按文档不一致报告：

- target 级重试退避、最大重试次数、死信队列。
- write-behind 同步检查点和更完整的指标。
- 多 target 顺序保证和分区顺序。
- 远端部分成功的标准化协议。
- 更丰富的 backpressure 策略。

## 10. 期望输出格式

请按以下格式输出：

1. 审计结论摘要：是否建议合并，是否有 P0/P1。
2. 分级问题清单：按 P0、P1、P2、P3 排序。
3. 已核实正确实现：只列与风险判断直接相关的证据。
4. 旧实现残留检查结果。
5. 测试运行结果和未覆盖风险。
6. 文档/能力一致性检查结果。
7. 最小合并前修复清单。
8. 可接受残余风险清单。

请务必给出具体代码证据。不要把“测试通过”当成唯一依据；也不要因为历史设计讨论存在某个目标，就假设当前实现已经满足。
