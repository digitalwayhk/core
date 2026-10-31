# Write-behind 自适应同步指南

## 能力和使用边界

v1.5.0 新增显式[有界重试与应用失败处置](WRITE_BEHIND_FAILURE_DISPOSITION_GUIDE.md)。下文仍说明零配置兼容和自适应收集；只有新模式提供应用声明的持久隔离与有限预算。旧模式满失败窗口的限制不代表新模式无法隔离，新模式的组内未解决前序仍必须阻断后序。

`PrefixedBadgerDB` / `ReliableWriteStore` 先可靠写入本地 pending，通过 `UseWriteBehind(target)` 绑定远端权威库；框架 worker 调度批次，远端确认后才处理 pending。它不是 MQ，也不是 MySQL 原生 group commit 参数。相同 key 合并状态的既有语义不变，不用于不可合并的资金流水事件。

业务仅在实例级组合根配置 store 和 target，不重新实现 Trades 等服务级同步循环。Manage 的 models 数据源工厂不参与此调度。

## 加性配置

```go
cfg := nosql.DefaultProductionConfig(pendingPath)
cfg.AutoSync = true
cfg.SyncBatchSize = 512
cfg.SyncFlushThreshold = 32
cfg.SyncMaxCollectDelay = 20 * time.Millisecond
cfg.SyncBacklogDrainDelay = 0
// 用 cfg 创建 ReliableWriteStore/PrefixedBadgerDB，再 UseWriteBehind(target)。
// target 必须指向应用实际的远端权威库；由 ServiceContext.UseResource 管理关闭。
```

三个新字段的 JSON/YAML 名称分别为 `sync_flush_threshold`、`sync_max_collect_delay`、`sync_backlog_drain_delay`。沿用 `BadgerDBConfig` 的 duration 序列化：标准 JSON 数字为纳秒，Go 配置使用 `time.Duration`；不要在 JSON 中把 20 当作 20 ms。

| 配置 | 契约 |
| --- | --- |
| 新增三字段全零 | 保持旧模式，包括原来的 `SyncBatchDelay`，升级不会自动启用 |
| `SyncFlushThreshold` | 启用时必须为正且不超过规范化后的 `SyncBatchSize`；统计不同 pending，不是写入次数 |
| `SyncMaxCollectDelay` | 启用时必须为正；空闲后首条 pending 成功入队时开始计时，后续写入不重置 |
| `SyncBacklogDrainDelay` | 必须非负；零表示有本地确认进展的积压批次之间不主动等待，包括部分确认同时有错误 |
| `SyncBatchSize` | 继续作为单轮和手动 `ForceSyncBatch` 的硬上限；不是达到阈值就只取阈值条数 |
| `SyncBatchDelay` | 自适应启用时不叠加；旧模式共享默认 100 ms，生产/快速构造器默认零 |
| `AutoSync=false` | 不启动 worker；手动同步仍可执行，且不等待收集窗口 |

只设置部分新增配置、负值或阈值超过批次上限都会返回配置错误。配置在共享 manager 首次创建时确定，不用于运行中热修改；同一个 pending 前缀应由一个实例级 store 管理，不应重复构造写者。

## 调度和可靠性

- 空闲后收集，达到数量阈值或首条期限即提交；不为每个信号重新启动完整窗口。
- 有本地确认进展且仍在同一 pending 周期时，重置失败退避，按 `SyncBacklogDrainDelay` 继续；部分确认同时返回错误也适用。队列清空再出现新数据，重新收集。
- 零确认时，从 `SyncMinInterval` 开始指数退避，上限为 `max(SyncMinInterval, SyncMaxInterval)`；包括返回错误、无错误但零确认、本地确认失败或快照已被并发更新的轮次。新写入和阈值不能绕过退避。部分成功后只剩失败项的下一轮若零确认，重新从最小间隔退避，避免忙循环。
- 进展取自原本地确认事务的实际结果，不按 target 返回列表长度计算。重复 key 不重复累计，旧快照不能确认新版本；本地事务失败不构成进展。原始 target/确认错误仍合并返回并记录，未确认项保持 pending；手动 `ForceSyncBatch` / `ForceSyncAllContext` 不吞错。
- 显式启用自适应模式时，构造 store 在返回前重建兼容同步索引并初始化 pending 计数，避免后台恢复扫描覆盖并发写入。已有积压绑定目标后直接排空，无需清目录或改变存储格式；启动扫描成本仍取决于历史本地数据量。
- worker、手动同步、关闭同步继续复用执行互斥与条件确认。旧快照不能清除并发更新后的 pending；未确认数据没有 TTL。
- 收集与退避等待可由关闭信号中断。`Close` 返回 `PendingSyncError` 时本地仍有业务事实，不能删除目录；远端驱动的既有执行/取消边界不因调度变化而消失。

**选批窗口边界：** `getUnsyncedBatch` 仍按持久索引前缀取最多 `SyncBatchSize` 条。本修复让同一批中可确认的健康项继续排空；若失败项占满整个窗口，后面的健康项仍不能被选中，整轮零确认仍退避。没有新增 lane、轮转或每业务 key 公平隔离，也不保证业务 chunk 的先后顺序；后者由应用 target 维护。

**20 ms 是主动收集上限，不是端到端完成 SLO。** 本地提交、Go 调度、锁等待、远端事务和失败退避另计。缩短低流量窗口必然可能增加小事务；阈值策略不能保证任意负载下 commit、redo、fsync 都下降。

## 同步后本地回收

`IsSyncAfterDelete()` 只决定已确认版本的本地模型副本是否可回收，不决定远端业务删除，也不代替业务提交证明。普通 `UseWriteBehind`、有界 `UseWriteBehindWithFailureHandler` 的 target 成功确认、应用 `WriteBehindConfirm` 且 `Outcome=WriteBehindCommitted`，均在满足各路径原有的本地确认条件（有界模式还核对指纹）、确认事务提交成功后，复用原异步清理队列调用该 hook。

- 返回 true：删除阶段再次核对本地版本未变、`IsSynced=true` 且不是 tombstone，再物理删除本地模型。
- 返回 false：保留已同步副本。之后同 key 保存允许删除的新版本时，必须等新版本自己的确认，旧候选不能删除它。
- 本地确认事务失败、旧版本/指纹不符、重复确认没有实际确认进展时，不调度新的清理；并发保存的新版本保留原 pending。
- 暂停、`HandedOff`、`Release`、`Superseded` 及迟到/失效回调不构成业务提交成功，不能触发此清理。接管结束技术 pending 与业务确认是不同事实。
- 部分成功只清理实际确认的原版本，原错误与失败项继续保留。生命周期快照、失败/完成历史仍按原政策保留，不能用它们与模型副本的区别伪造磁盘排空。

**关闭/重启边界：** 当前异步清理候选仅保存在内存，不是另一个持久队列。确认已提交、清理尚未入队或执行完时退出，已同步模型可能仍存在；重启不会扫描并补删这些记录，也不会为清理而重发已经确认的 target。`pending=0` 和 `Close` 成功只代表原同步/关闭合同，不能当作所有本地副本已回收。真实 Badger 测试使用受控屏障固定“确认提交后、清理入队前关闭”的窗口，并重开原路径验证两种模式均保留此边界。

本次修复补齐有界成功路径漏掉的调用，不增加配置、公开 API、存储格式或持久回收协议。消费方继续实现原 `IsSyncAfterDelete` 和原绑定入口即可；升级后有界模式开始实际执行 hook，应核对返回 true 的业务条件，而不是依赖此前漏调用造成的保留。旧版已经确认但未清理的存量副本不会因升级自动回收，需另行制定业务认可的回收方案；不得用 `Delete` 产生远端 tombstone、调用 `PurgeLocal` 强删或清理 pending/失败历史冒充修复。

## 验证和观测

使用已有 `GetSyncMetrics()` 和 `GetCachedPendingSyncCount()`；必要时用 `GetPendingSyncCount()` 对账。指标按同一轮分别累计：

- `Attempts`：进入同步轮次的次数；`Failures`：该轮返回错误的次数。
- `SyncedItems`：本地实际确认数，部分成功带错误也累计。例如一轮确认 120 条并返回错误，三项分别增加 1、1、120。
- `LastSuccessAt`：最近一次无错误且有确认的轮次时间。部分成功带错误、零确认都不更新，也不清除以前的成功时间。
- 耗时沿用现有轮次累计和最近值。没有采集的队列等待分位数不能填零。

自动 worker 在同一轮次边界记录 `confirmed`、`has_error`、`next_wait_ms`、`reason=progress|no_progress`，复用已有低基数 store `prefix`。有错误沿用 `write_behind_adaptive_sync_failed` Error 日志，无错误使用 `write_behind_adaptive_sync_completed` Debug 日志；默认级别未采集 Debug 时不能把缺失当零。`next_wait_ms` 是轮次结束时的调度快照，空队列时为既有空闲唤醒间隔，不是端到端延迟或下一次实际执行承诺；新写入、关闭、锁等待可能改变实际时间。不增加采样线程，不记录消息正文、SQL、凭据或业务标识。

修复不增加配置、改变零值模式或存储格式。消费方只需升级 Core 并继续通过原 `UseWriteBehind` 入口接入；只有已显式开启自适应调度的 store 改变部分成功后的等待策略。指标修复也覆盖手动和旧模式的同步轮次。不得清理存量 pending 或重建业务数据来升级，消费方自己的业务排序、故障恢复、UAT 和容量仍需单独验证。

真实测试需隔离 MySQL、框架自动建表及预热在计量窗口外；固定数据和批次上限，对照旧 100 ms、固定 10 ms、自适应配置，记录每批数量、远端行数、pending 收敛、事务和可支持的资源指标。数据库全局计数可能包含后台刷盘，不能当作每次业务 commit 的严格归因。

```bash
go test -race ./pkg/persistence/database/nosql -count=3
CORE_ADAPTIVE_MYSQL_ADDR=127.0.0.1:<专用端口> \
  go test ./pkg/persistence/database/nosql -run TestAdaptiveMySQLComparison -count=3 -v
```

比较测试使用专用、空密码 root MySQL，创建唯一的 `core_adaptive_*` 测试库并保留数据。不得指向应用数据库。不提供地址时明确跳过，不能算真实 MySQL 验收。结果见 [自适应同步验证记录](WRITE_BEHIND_ADAPTIVE_VERIFICATION.md)；Bitzoom R60 同负载验收独立执行。


## 有界后台本地确认的提交预算

有界 `ReliableWriteStore` 的 target 结果按固定最多8项和约1MiB的编码写集预算（直接PrefixedBadgerDB无facade时最多32项）分块落地；Badger还会检查实际事务硬限，遇 `ErrTxnTooBig` 有界二分，不重新调用target。单个不可拆分原件超过软字节预算时隔离为单项事务，仍受Badger硬限；失败保留原件/未知结果，不扩大整批或删数据。不能把 SyncBatchSize=128 理解成128次本地确认提交，也不能把Badger实际事务数等同于OS fsync/msync次数。

本地确认及其他处置元数据复用现有 `BatchCommitter` 的同一个有界FIFO。前台保存仍可合批；内部事务是明确边界，处理前先完成之前接收的前台批次，再执行内部有界块。没有第二套队列或提交worker；target、网络、重试计时、OrderingGroup以外的应用回调均不在提交器中执行。`storageMu`仍保护同prefix的sequence/cursor/组索引和原件原子性，不能因“统一队列”删锁或按业务用户分锁。

真实提交成功后才扣pending、累加确认并安排IsSyncAfterDelete；失败块全部保留，已提交块不因后块error被遗忘。旧状态/版本/指纹隔离，不回滚无关有效项；target成功和本地确认成功仍是两阶段。可能变空的组在同一实际事务最终写集上去重核验，各组使用设置了目标组`IteratorOptions.Prefix`且不预取value的新迭代器。仅在Seek返回后检查ValidForPrefix不足以限制Seek内部查找；也不能跨后续写集复用迭代器。索引格式和组首屏障不变。

前台替换`Ready && Attempts==0`旧版本时仍持久保存Superseded历史并删除旧队列项。旧新实际组身份相同时，新队列项会在同一事务补入，因此跳过这次空组核验；同key改组必须核验旧组，不能按key推断组不变。ApplyWriteOperations/SaveBatch与BatchInsert复用事务内deferredGroups，集合在每次实际事务闭包中建立；回滚、ErrTxnTooBig二分和子批重试不复用失败事务的集合。清理仍在提交前与原件/索引原子完成，不是异步组回收。该修复不改变API、配置、StoreID/policy、指纹、sequence、卷格式或接入方式；无需应用开关或迁移。性能收益按负载验证，不能宣称所有跨组负载均提速，见[具体修复和边界](WRITE_BEHIND_DISPOSITION_GROUP_FIX_2026-10-31.md)。

Close先停止前台准入，唤醒满队列中尚未被接收的保存请求并返回ErrWriteStoreClosed，再等待已接受保存的FIFO屏障，保留内部提交通道直到原后台/清理生命周期退出，再关闭提交器。接受后等待真实结果；超时不代表外部target/handler实际停止，也不授权回收仍被持有的卷。启动恢复、通知及到期重试继续走原worker，无新请求也能推进。直接使用 `PrefixedBadgerDB` 没有facade提交器时仍使用同prefix互斥和有界块；legacy/零配置不启用内部FIFO接线。

`GetSyncMetrics()` / `store.Metrics().Sync` 增加 `ForegroundLocal`、`ConfirmationLocal`、`MetadataLocal` 和 `TargetDuration`。三个本地快照包含Attempts/Committed/Failures、Items/MaxItems、TooBig、QueueWait、LockWait、CommitDuration及最大事务时长：Committed表示无错的Update调用（可含空写集），Items为成功块输入项数，不是业务确认数；实际确认仍看SyncedItems。MetadataLocal统计其余本地Update，含选批、恢复、决定和物理清理。facade的ForegroundLocal.QueueWait复用Batch.TotalQueueWaitDuration逐操作累计；前台排队也可继续看Batch.TotalQueueWaitDuration，准入看Admission.TotalDuration；这些是累计值，不是分位数。CommitDuration含编码及Badger可靠提交，不是纯fsync；TargetDuration仅在有界target调用边界采集（legacy不能用未采集值作性能对比），已包含在同步轮次TotalDuration中，不能相加。无采集的OS同步次数、CPU限流及I/O字段必须为null/NOT_RUN。

本修复不更改配置、policy指纹、持久格式或刷盘参数；升级自动生效，无需关闭有限处置。存量原卷仍须相同StoreID/policy；保持既有旧二进制回退限制，禁止删mode/pending/history迁移。异步清理仍仅内存队列，不提供跨重启补删。

## 合批等待与官方参数实验

Save平均和P99需按实际分布与慢请求分段判断，不能把20ms远端收集窗口、1ms本地合批窗口当成整体Save上限。Badger官方建议摊销逐key事务，但Core原件核验是读改写事务，不能直接替换为盲写WriteBatch或在Commit前确认成功。调度让出也可能帮助尚未入队的请求合批；删Gosched/队列暂空即提交须同时验证事务数、批大小和尾部，不能只看一次P99。具体官方参数差异、实际Options及本轮未采纳候选见[合批与参数对照](WRITE_BEHIND_BATCH_PARAMETERS_2026-10-31.md)。生产默认值和可靠性合同保持不变。

实际使用先核对持久SSD路径和可用CPU预算；同一二进制串行对照1P/2P，不能从单P结果推定多核表现。2P仍共享本地提交器和Badger，不承诺完全隔离。复用实例store，并发Save或已有数据的SaveBatch在有限窗口内摊销事务；不得每笔Save后再ForceSync远端。07提供显式2P资源Compose候选及配对命令，默认仍为原1ms窗口；具体结果和硬配额NOT_RUN边界见[SSD与Go并行度验证](WRITE_BEHIND_TWO_PROCS_2026-10-31.md)。


## 选批范围与前台提交等待

后台`selectDispositionRound → updateWriteBehindTransactionKind → BatchCommitter FIFO → finishLocalCommit → updateLocalTransactionDirect → Badger.Update`与前台Save共享本地提交面。target/SQL/HandleFailure不在此事务中，但选批读状态、写InFlight/attempt/BatchID/cursor时仍可能阻挡已入队Save。不能用最终Txn.Commit很短推断整个队列等待很短，也不能将一个共享批次耗时除以请求数当各单独占耗时。

选批三类迭代器使用精确底层范围：target为`base+target:`，组为`base+g:`，具体队列为`base+q:+groupHash+:`。组范围始终覆盖全部组，cursor仅决定Seek起点，末尾按原规则回绕；迭代器仍在原事务创建，不复用到看不到新pendingWrites的旧快照。外层ValidForPrefix和ScanLimit不能替代底层Prefix。

范围内的删除历史仍可能让Seek昂贵，尤其普通记录过去也会留下target删除标记。现在保留原删除写集/历史，只复用经版本核验的target范围结果：每轮事务点读既有mode key，将其Badger Item.Version与上次范围结果比对；target成员增加/移除时，在同一事务重写mode的**原字节**。policy值与指纹不改变，不增加持久key、配置、格式或worker。同prefix的另一正式wrapper变更也会失效；与并发成员变更相交的选批事务通过该点读参与Badger冲突检测，不可用旧缓存越过新全局屏障。

缓存只持有一个ID/空结果及版本，使用原storageMu保护，关闭后丢弃；原target marker仍为权威。首次/重开的查找在绑定返回前完成；ScopeTarget相关变化可保守失效，之后的查找仍可能随该范围历史增长，未承诺所有冷启动或target高频变更的长尾消失。禁止裸改内部marker、删除历史或把ScopeTarget失败当成功来换取空缓存。

正常候选的InFlight/attempt和BatchID在同一最终事务写入一次，避免先编码/Set无BatchID快照再覆盖；之前的未知、超时、预算、PreviousID及active组核验仍执行。组首一次至多一个，Ready/InFlight都属于未解决，因此不能因减少事务内重复写入而跳过前序。本地事务失败仍整体回滚，不提前调用target或确认pending。

这项修复不改变原FIFO、1ms窗口、固定8项确认量子、SyncWrites/DetectConflicts、policy/StoreID、原件/组/队列/cursor编码或成功后清理。已识别有界格式的v1.6.3卷可按原配置重开；缓存重建不等于重置重试预算。回退必须先停稳target/handler并备份副本，不并发打开同卷，不回到不识别有界状态的旧版。

诊断沿用Admission、Batch、ForegroundLocal/MetadataLocal/ConfirmationLocal和TargetDuration，额外包内选批观察仅用于测试，nil时不采集时钟、不增加线程。NewIterator/Seek是嵌套阶段；round计时包含本次元数据队列等待，事务计时含编码/提交，不能相加冒充Save壁钟。target_cached命中没有新建/Seek迭代器；未测的版本点读、纯fsync、物理I/O和CPU限流不填0。Core组件实验与消费方Full-E4验收分开。

已有cursor最终值未变时不重复Set，空轮/全暂停轮不再仅为相同cursor制造可靠写事务。首次cursor建立、推进和回绕保留；其它状态/批次写集即使cursor未变仍正常原子提交，不按pending缓存推断空轮。`LocalTransactionMetrics.Committed`沿用成功Update调用计数，包含只读/无写集成功，不能当物理刷盘数；测试以实际Badger提交版本及分段核验。普通ScopeKey保存不增加target存在性点查，离开ScopeTarget的应用决定与marker删除/失效同事务。

选批范围与重复写集的修复不等于前后台资源隔离，也不保证Save P99全面改善。v1.6.4多轮持续数据、仍存尾部及既有硬杀恢复失败见[正式报告](WRITE_BEHIND_SELECTION_CONTENTION_2026-10-31.md)，升级无需配置/格式迁移，消费方旧skill链接或复制需同步。
