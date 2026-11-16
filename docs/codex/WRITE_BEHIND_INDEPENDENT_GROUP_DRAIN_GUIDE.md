# 同 store 独立顺序组持续排空

## 适用场景与边界

默认 `UseWriteBehindWithFailureHandler(target, handler)` 仍把一轮不同组的组首交给一次 `SyncBatch`，在原 `syncExecMu` 内等待整批结果。target 内部即使并发，Core 也要等它返回才知道可确认哪些原件。它保证正确性，但健康组可能等待无关慢组。这里不把消费方全部延迟归因于 Core，也不提供跨 store、跨副本或跨主题的全局业务顺序。

仅当应用提供了稳定的独立 `OrderingGroup`，每组的 target 调用可以独立提交，且 target 可安全并发调用时，显式使用加性入口：

```go
// 仍使用原 ReliableWriteStore、WriteBehindFailurePolicy、target 和 handler。
err := store.UseWriteBehindWithFailureHandlerOptions(
    target, handler,
    nosql.WriteBehindExecutionOptions{IndependentGroups: 4},
)
```

`ReliableWriteStore` 和 `PrefixedBadgerDB` 均提供此方法。零值 `IndependentGroups=0` 与旧绑定相同；合法显式值为 1–64，负数或大于64拒绝。该值是并发槽数，不是总组数上限；1个槽仍无法让B在A占槽时执行，跨组并行至少需要2个空闲预算。缺少有效有限 policy、target、handler 或不满足可靠写配置仍按原合同拒绝。本入口不是 `SyncMaxConcurrency`，不自动启用旧保留字段，不增加业务配置开关或修改 policy 指纹。

调用应用仍须在接受写入前绑定；不得在运行时改变组定义。Core 只认识应用不透明组字符串，不推断用户、市场或金融语义。空组仍只隔离同 key。

## 实际执行与预算

新模式复用原 worker 生命周期、持久选批、游标、前序校验及有界确认。固定 N 个 target worker、N 个总执行槽、容量 N 的内部执行交接，**在途加排队总数不超过 N**；每次 target 调用只含一个组首，按实际调用分别持久 BatchID。`SyncBatchSize` 与 `ScanLimit` 仍限制一次选批和扫描。没有每条启动 goroutine、第二套业务队列或新提交器；SQL、网络和失败回调不进入本地 FIFO/元数据事务。

`syncExecMu` 保留选择、预留与历史维护的串行边界，但独立组确认不持它等待本地FIFO或可靠提交，已确认worker也不再第二次取它归还槽。原 `storageMu` 与 BatchCommitter FIFO 继续串行本地原子写集；组/ID的登记和真实回调退出后的归还使用既有执行器的短内存互斥，不跨Badger、FIFO或应用回调。target在本地提交保护之外。它真正返回并完成原版本本地确认后才释放组和槽；B1 真实确认后，B2 可占空闲槽执行，不必等仍占另一个槽的 A1。自动 worker 不等待整轮所有 target 返回。

原条带锁继续防止强制本地删除与远端同步重叠。独立组 target 对条带使用共享保护，不同 key 的哈希碰撞不再相互串行；同 key（含改到不同组的新版本）仍由原持久前序和实际在途 ID 阻断。普通模式与强制删除保持排他保护。不是删锁来换性能。

这项能力将跨组原子批拆成独立调用；依赖跨组同一事务的 target 不适用。即时小 target 可能增加调用数和本地确认事务数，不能把该模式等同于无条件吞吐优化。确认仍使用原最多8项/约1MiB量子、TxnTooBig拆分和真实提交后计数。ReliableWriteStore的原FIFO现在可以将已经相邻入队、同store的独立组结果合并为有界本地事务；遇到前台保存、元数据屏障、其他store或预算边界就停止，不跨越或重排，不添加收集等待。单次target仍一组首，各命令分别保留原BatchID、cause与结果；这不是等待所有组完成的屏障。直接PrefixedBadgerDB没有facade FIFO时继续原本地互斥提交，不新建提交器。前后台仍共享本地提交器、CPU和磁盘，不保证 Save P99 固定上限。

当全部 N 个槽都被慢或不合作回调占用时继续背压，不扩 worker；一个组本身的速度不足也不会因本能力变快。按原组游标有界扫描，不宣称所有积压已公平隔离。选项不改变 target 自己的 I/O 超时责任。

## 成功、失败、取消与手动调用

原版本/指纹/attempt/BatchID、当前原件和同步索引在确认事务内复核。target 成功不等于本地确认成功；事务失败不能提前扣 pending、计 SyncedItems 或触发 `IsSyncAfterDelete`。旧结果不能确认或删除新版本。未确认/取消/未知仍进入原有限处置，应用决定 Resolve/Retry/Pause/Handoff/Confirm/Release，预算不重置。target范围决定阻止此后新准入，不取消已在途的其他组。

自动与手动都使用同一固定执行池。显式模式下 `ForceSyncBatch(ctx, limit)` 至多选择 min(limit、SyncBatchSize、当前空闲槽) 个不同组首，返回本调用已收回且真实完成的确认数（取消后尚未收回的结果需查实际状态）；不是保证处理满 limit。空闲槽被占时可等原工作释放或取消。本调用尚待 A 时，另一个调用或自动 worker 仍可利用空闲槽处理 B；同一次 ForceSyncBatch 的返回仍等待它自己的已选项，不能把请求已入队当作完成。`ForceSyncAll` 保持循环/错误/无进展边界。

调用者取消可先返回；已选任务仍由原固定 worker 持有，直到原 target 真正退出。关闭取消并等待原 worker；不合作代码导致 `ErrWriteBehindDispositionCloseTimeout`，不能因此回收其仍使用的应用存储。迟到成功不靠取消前的结果伪造确认。应用继续按原幂等/未知事实核实合同恢复。

本地成功清理沿用原异步 hook：false保留，true仍核对原版本；pause/handoff/release不当业务成功。清理队列是内存状态，确认提交后清理前退出，重启不自动补删。终态保留、nil Item、原件/历史回收及单向格式门禁全部沿原指南，不借本任务改格式或删历史。

## 观测、升级和回退

复用 `GetSyncMetrics` / `ReliableWriteStore.Metrics` 的 target、本地事务、FIFO等待、准入及确认计数。新模式的 Attempts/Duration 按独立实际target工作计；零值仍按原整轮，不能直接跨模式相加比较。`WriteBehindDispositionMetrics.TargetActive/TargetQueued/TargetLimit` 与原 Handler 指标分开；旧模式三个值为0表示未启用该池。状态扫描仍是显式诊断，不能每请求全量调用。

`write_behind_group_dispatch` 只输出选取量、池在途/排队/上限及错误标志，不输出 group、key、payload、SQL或凭据。目标回调的用户业务身份不得成为指标标签。累计时间、嵌套阶段和请求壁钟不能相加。

升级保留原卷、StoreID、policy、组定义、原 pending、序号和历史；不需要重建或清消息。停止并确认原 target/handler 真正退出后替换二进制，再在绑定处显式传选项。不升级应用绑定时行为不变。已支持同代终态格式的旧版本可以在停稳后关闭该选项、验证卷副本再回退；这不取消更早版本不识别有界/终态格式的拒绝规则，禁止删除mode或同时开同卷。

07 的组合根 `Service.WriteBehindExecution` 透传到原订单 store；没有 FailurePolicy 却设置独立组时明确拒绝。默认仍是零值。04示例保持默认目标，应用可参照本指南在其可靠 store 层显式绑定，不复制调度循环。

组件测试与性能命令见[专项验收报告](WRITE_BEHIND_INDEPENDENT_GROUP_DRAIN_2026-11-16.md)。Core 本地真实Badger、race、进程恢复和示例通过，不代表消费方已升级、Full-E4功能/UAT或容量通过。

### 手动等待通知补丁（v1.13.1）

独立组API自v1.13.0增加，但其手动等待者只收到target完成通知，可能遗漏等待期间新Save或应用Confirm/Retry等统一通知。正式启用使用v1.13.1或更高：复用原triggerSync唤醒自动worker及代际等待者，选批前捕获通知代际；手动调用等待无关在途组时，沿原policy.MinRetryInterval使用一个可取消timer有界复查到期资格，原NextAttemptAt/attempt和预算仍决定是否可执行，未知结果不会因此自动重试。不增加后台轮询线程或worker，不改预算/格式，关闭/取消仍走原生命周期。原v1.13.0 tag保留，不能把其自动场景通过当作全部手动场景完成。

### 既有恢复限制

本专项真实新PID恢复覆盖已确认健康组不重放、未知组查应用事实后推进。完整回归另捕获旧有Badger硬杀重开`Create a new file`错误，固定v1.12.1基线与候选各20轮均19通过/1失败，尚未修复；后续单轮普通/race通过不等于所有硬杀恢复已获认证。原FAIL和具体命令见专项报告，不能据此改回调确认或删除卷数据。

上述v1.13.1验收时的零字节memtable重开失败已由v1.13.2专项处理，旧失败原件仍保留。仅针对已验证的空WAL回收中断，不覆盖非空损坏、物理断电或所有平台。详见[恢复修复与平台边界](BADGER_EMPTY_MEMTABLE_RECOVERY_2026-11-16.md)。

### 在途组的选批成本

独立组的实际target在途且尚未归还原组/槽时，选批仅轮转组标记，不重复对该组queue Seek和解码完整原件。失败handler仍读取原件并按原期限持久暂停；不能将全部active组一概跳过。ScanLimit、cursor、target作用域、版本/attempt/BatchID和可靠本地确认保持不变。该优化降低重复选批成本，不保证消费方端到端时延，也不消除本地可靠提交之间的资源竞争。

### 确认控制锁与FIFO合批补丁

独立组结果在真实本地事务开始时再次核对调用者取消和store关闭，取消/未知不借合批提前确认；事务失败整块不发布计数/hook，TooBig只拆本地写集并分别保留已提交与失败块，不重调target。确认、pending计数、hook和失败通知是不同边界，后两者仍由原等待worker在FIFO之外执行。相邻确认不经过前台1ms收集窗口，不扩大原8项/字节量子，不能承诺任何负载下都合批或Save P99固定下降。

仅显式IndependentGroups已绑定的ReliableWriteStore自动采用此内部修复，无新参数/API/卷格式；普通UseWriteBehind与旧有界零值目标接线保留。停稳原回调、保留原卷与policy/StoreID后升级，不清pending/历史；同代格式回退仍须原卷副本和进程验证。真实32组RED、确认前后硬杀恢复、持续性能差轮及升级skill见[验收报告](WRITE_BEHIND_CONFIRMATION_CONVOY_2026-11-16.md)。
