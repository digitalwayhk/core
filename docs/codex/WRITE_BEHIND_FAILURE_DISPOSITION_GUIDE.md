# Write-behind 有界重试与应用失败处置

本能力自 v1.5.0 提供，复用 `ReliableWriteStore / PrefixedBadgerDB`、原 pending 和 worker。应用决定失败分类、原结果确认、预算及恢复责任；Core 负责可靠快照、有限调度、版本核验和恢复。不提供业务异常管理页面、业务补偿或另一套队列服务。

## 显式启用与正式配置

旧 `UseWriteBehind(target)` 签名不变。`BadgerDBConfig.WriteBehindFailure=nil` 保持旧模式，**不代表旧消费者已经有界重试**；v1.4.8 的自适应部分进展修复继续保留。`RetryKeys / DeadKeys` 在两种模式中都不直接执行重试或删除。新模式的未确认项进入应用处置，不据这两个字段猜分类；额外 key、同 key 交叉分类被拒绝，重复成功 key 不增加确认数。

```go
cfg := nosql.DefaultProductionConfig(pendingPath)
cfg.AutoSync = true
cfg.SyncBatchSize = 128
cfg.WriteBehindFailure = &nosql.WriteBehindFailurePolicy{
    StoreID: "orders:stable-instance-a:order-facts",
    MaxAttempts: 3, // 包含首次target调用，不是额外重试次数
    MaxElapsed: 5 * time.Minute,
    MinRetryInterval: time.Second,
    MaxRetryInterval: 10 * time.Second,
    HandlerMaxAttempts: 8,
    HandlerTimeout: 2 * time.Second,
    HandlerWorkers: 2,
    ScanLimit: 128,
}
// 用cfg在组合根创建store；接受写入前绑定应用handler。
err := store.UseWriteBehindWithFailureHandler(target, handler)
```

`StoreID` 必须非空、长度不超过 1024，按服务、稳定实例和 store 区分；重启、移动原卷及回执恢复时保持不变，不能每次 NewID。`MaxAttempts / MaxElapsed` 非负且至少一种大于零，任一先耗尽即停止重试；MaxElapsed 从首次 target 尝试起按壁钟累计，停机也计入，不强杀在途代码。重试最小间隔为正、最大值不小于最小值；handler 次数和超时为正，workers 为 1–64，ScanLimit 为 1–65536。缺 policy、缺 handler 或非法配置明确拒绝，不代填金融容忍时间。

JSON/YAML 为 `write_behind_failure` 下的 `store_id/max_attempts/max_elapsed/min_retry_interval/max_retry_interval/handler_max_attempts/handler_timeout/handler_workers/scan_limit`。duration 沿用标准数字纳秒序列化，Go 使用 `time.Duration`。零指针不启用，空对象不是安全默认。

## 应用接口与稳定快照

`WriteBehindFailureHandler[T]` 包含两个方法：

- `OrderingGroup(*SyncQueueItem[T]) (string, error)`：确定、有界、无 I/O 的纯函数，在本地事务中调用。空字符串表示仅同 key 依赖；非空字符串是应用声明的顺序组。Core 不识别 UserID、MarketID 或成交序号。
- `HandleFailure(context.Context, WriteBehindFailure[T]) (WriteBehindDecision, error)`：在固定 worker、独立超时上下文及原同步执行互斥之外调用。

输入提供原 `Record`、`Cause`、预算耗尽标志、已等待时间、`BatchID`、实际本地 `ConfirmedKeys` 和完整 `Unconfirmed` 原版本记录。包含稳定 ID、StoreID、key、指纹、原操作/数据、版本、组、持久序号、target/handler 次数、首次尝试/失败时间和状态。批次成员在 target 调用前持久记录，回调从同一 Badger 快照核对原版本状态；崩溃不能伪造确认集合。整个原子事务未知时可声明 target 范围，不要求编造精确坏条目。

尝试次数在调用 target **之前**持久预占；预占后崩溃可能消耗一次预算，但绝不多授予执行次数。进程在 target 返回或本地确认前退出，恢复 unknown，先交应用确认，不能盲调 target。旧 pending 首次启用时，历史次数/事务结果不可恢复，先标记 `legacy_outcome_unknown`；应用确认后才能重试，新预算从启用后计，不声称约束旧版本已经发生的次数。

当前进程及时准入的回调可保留原错误链；排队恢复或跨进程后只能恢复持久错误说明，不能重建任意第三方错误的 Go 类型。应用不得只按错误字符串猜业务结论；原错误仍由同步调用返回，不能吞 error 换取排空。

## 决策与恢复

每个 `WriteBehindDecision` 必须绑定原 `ID + Version + Fingerprint`，声明 `Scope=key|group|target`。非法身份、旧版本、缺失/非法决策、回调错误、panic、超时或处理存储失败保留原件并暂停，不回落到无限 target 重试。

| Action | 必需事实和效果 |
| --- | --- |
| `WriteBehindRetry` | 应用确认 `Outcome=WriteBehindNotCommitted`，RetryAfter 在最小/最大区间且尚有 target 预算，持久到期后才执行；超时或一次 not found 不证明原请求停止 |
| `WriteBehindResolve` | `Outcome=WriteBehindStillUnknown`，按原身份再次查询，消费独立 handler 预算，不执行 target |
| `WriteBehindPause` | 持久暂停原记录及声明范围，不表示成功或取消 |
| `WriteBehindConfirm` | 应用按原身份核验真实提交，`Outcome=WriteBehindCommitted`；仍按原版本/指纹条件确认，不清除后来新值 |
| `WriteBehindHandoff` | 应用先幂等持久保存原件/独立保全引用，Core 保存回执后结束相应技术 pending，不解除顺序屏障 |
| `WriteBehindRelease` | 仅适用于已交接记录，明确 `Outcome=WriteBehindOrderingReleased` 后解除原身份屏障；不能跳过未交接的失败数据 |

target/handler 预算耗尽分别记为 `target_budget_exhausted / handler_budget_exhausted` 并暂停。重新通知、ForceSync、重启和人工操作不重置预算。通过 `ApplyWriteBehindDecision(ctx, decision)` 请求仍有预算的重试、确认真实提交、接管或继续结果确认；耗尽后的恢复由应用明确接管，不能偷偷补次数。相同持久接管、成功确认和 Release 可幂等重放，冲突回执拒绝。

同组只执行本地持久准入最早未解决版本，后序不进 target；无关组按持久游标轮转，每轮最多 ScanLimit 组、最多 SyncBatchSize 条。完整失败窗口不会永久占住索引前缀预算；不承诺跨 store、跨副本或业务全局序号顺序。应用仍负责事务幂等、业务版本和 chunk 依赖。target 范围决定持久后阻止新准入，不能取消此前已准入或在途的批次。

同 key 新版本不会抹掉旧在途/失败快照；旧决定最多解决原版本，不能 ACK 新值。尚未投递的同 key 版本沿原可合并状态语义替换；不可合并金融事实必须使用独立业务 key。

## 可靠接管与事实区分

回执必须包含原 `DispositionID / Version / Fingerprint`、稳定 ReceiptID、Owner 和应用自己的 DurableReference。原件必须已在应用独立事务中保全，不能仅引用将结束的 Core pending。Core 校验绑定和持久顺序，**不能独立证明任意外部数据库提交**；应用事务、幂等及恢复责任是信任边界。

1. 应用按稳定处置 ID 保存完整事实与恢复状态，提交成功后返回回执。
2. Core 同一事务持久绑定原版本回执、交接状态及技术 pending 的条件完成。
3. 应用已提交但回执丢失时，原 pending 保留并暂停；应用按原身份 Resolve，或在崩溃恢复时复读同一回执，不产生第二次业务效果。
4. Core 保存回执前退出，恢复原身份确认；保存后退出，恢复 handed_off，不重做 target。
5. `HandedOff=true` 与 `IsSynced=true` 分开：接管不标远端同步成功、不进入 ConfirmedKeys、不增加 SyncedItems；索引重建不复活已交接项。同组屏障持续到应用 Release。

原快照、失败历史和交接事实无 TTL、不自动删除。成功版本也保留生命周期记录；本轮未提供历史回收协议，长期运行必须按实例磁盘预算监控占用，另行制定有证据的归档/回收流程，不能清 pending 制造排空。

## 资源、关闭与观测

复用原同步 worker，target 仍在原同步互斥下串行批次执行。失败回调使用固定 HandlerWorkers 及等容量队列，不逐条起 goroutine，不补充卡住的 worker。handler 次数在准入时预占，HandlerTimeout 包含有限队列等待；过期或容量不足会保留原件并暂停，预占次数不等同函数实际开始次数。短本地事务按 prefix 串行保护元数据，不跨 target/回调持锁，不自动重放带应用副作用的闭包。

超时或取消不证明原代码/远端请求停止。不合作回调占住原槽；即使人工请求 Retry，同组仍等实际退出，无关组继续。Core 无法强杀任意 Go/远端代码，应用 I/O 必须有界并幂等。关闭取消上下文并等待原生命周期；新模式未实际退出时返回 `ErrWriteBehindDispositionCloseTimeout`，即使技术 pending 为零。超时返回不能授权回收仍被业务持有的应用存储。

运行中选批和启动索引修复都分页，内存受 ScanLimit、SyncBatchSize、固定 worker/队列及模型快照大小限制；这些功能测试不证明最佳预算或稳态吞吐。

- `GetSyncMetrics()` 保留原 Attempts/Failures/真实 SyncedItems；应用核验后的真实确认可增加 SyncedItems，不伪造一次 target 尝试。LastSuccessAt 仍指无错误且有确认的同步轮次。
- `GetWriteBehindDispositionMetrics()` / facade `WriteBehindDispositionMetrics()` 区分 ready、retrying、unknown、paused、handed_off、预算耗尽、最老失败时间及 handler 实际/排队/上限。显式扫描未解决索引，内存恒定但耗时随积压增长，不用于高频业务请求；未启用返回错误，不能伪装为零。
- `ListWriteBehindFailures(after, limit)` 是应用受权限保护的本地恢复入口，limit 为 1–1000，不是匿名 API 或管理页面。记录含原数据和错误，禁止日志 dump。
- 轮次日志仅 confirmed、has_error、scan_limit、next_wait_ms、progress/no_progress；next_wait 是控制轮次快照，具体到期以持久 NextAttemptAt 为准。暂停/交接仅记录固定原因和技术状态，不输出 key、用户、订单、payload、SQL、凭据或回执，不增加采样线程。

当前技术 pending 和生命周期记录不是同一集合：pending 可为零而 handed_off 仍大于零，不能相加成业务成功数。全局 Attempts 是同步轮次，原记录 Attempts 才是该版本的 target 预算计数。

## 升级与示例

新模式持久冻结 policy；前缀须用相同 StoreID、配置和处置器重开。新代码的旧绑定入口拒绝绕过；未实现在线改预算、mode 回退或跨版本混跑。旧二进制不识别这些状态，**禁止用旧 Core 打开已启用新模式的卷**。调整 policy、分组定义或处置语义前须停稳实际回调/target 并制定状态迁移，不能删 mode 标记或 pending。

07 的 `Service.FailurePolicy / FailureHandler` 允许组合根显式注入，零值不启用。`business.OrderFailureReceipts` 演示应用独立 SyncWrites Badger 保存完整订单和 `owned_unresolved` 回执；资源逆序先停 Core 再关闭应用库。立即接管是示例应用明确选择的策略，不是 Core 对 SQL 异常的默认猜测；没有开发业务补偿或异常管理页面。

```bash
rtk proxy go test -race ./pkg/persistence/database/nosql -count=1 -timeout 300s
rtk proxy go test -race ./examples/07-shop-order-scale/order-service/... -count=1 -timeout 300s
```

Core 真实存储、独立进程硬杀和示例资源链通过，不代表消费方真实 MySQL、业务路由、UAT 或容量通过；发布证据及 NOT_RUN 单列。
