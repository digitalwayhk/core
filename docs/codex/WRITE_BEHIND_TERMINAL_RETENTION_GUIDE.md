# 有界可靠写终态历史保留与回收

本指南适用于新增终态回收格式。它解决Core每个成功/被替代版本永久保留完整快照的问题，不改变业务数据库事务，也不宣布消费方部署、UAT或容量通过。

## 三种数据、三个完成边界

- 业务副本由原模型和`IsSyncAfterDelete`决定。hook=false仍保留；hook=true也只在原版本本地确认真实提交后异步执行，不能因为同步或历史回收而提前删业务副本。
- 未解决的ready/in_flight/unknown/handling/retrying/paused/handed_off保存完整原件、指纹、顺序、尝试预算和回执；不设TTL、不自动完成。handoff不等于业务成功，release只是应用明确解除已交接屏障。
- completed/superseded/released是已解除Core责任的技术终态。默认保存key/version/指纹/状态等轻量信息，立即去掉持久副本中的`Snapshot.Item`。原调用中的完整快照不被修改，提交后hook仍拿到原件。

同步完成、业务副本清理完成、技术历史回收、Badger物理文件回收分别核验，不能互相替代。`pending=0`只证明技术同步索引为空。

## 默认与可选审计

不改原`WriteBehindFailurePolicy`，使用原`UseWriteBehindWithFailureHandler`。默认`BadgerDBConfig.WriteBehindTerminalRetention=nil`，不额外保留完整终态载荷。普通`UseWriteBehind`和未启用有界失败模式不改行为。

确有额外审计需求时，在组合根声明：

```go
cfg.WriteBehindTerminalRetention = &nosql.WriteBehindTerminalRetention{
    MaxAge: time.Hour,
    MaxRecords: 1000,
    MaxBytes: 16 << 20,
}
```

JSON/YAML字段为`write_behind_terminal_retention.max_age/max_records/max_bytes`；duration仍用标准数字纳秒。三种预算必须全部为正，且必须同时启用`WriteBehindFailure`。空对象、无限预算和legacy模式配置均拒绝。时限表示逻辑回收资格，后台执行存在有界分页/调度延迟，不是Badger TTL或物理删除截止时间。

预算是本prefix持久原子计数，`MaxBytes`计额外完整Snapshot的JSON编码字节，`MaxRecords`计完整终态快照数。超出预算只不保留该项额外审计载荷，不让已经成功的业务确认变失败；未解决原件不使用此预算，不被它淘汰。重启不重新授予预算，缩减/取消审计配置后按同一有界维护路径清理超期载荷。业务合规审计须使用应用权威审计存储，不能依赖Core临时技术历史。

轻量终态提供固定一分钟诊断宽限。宽限结束、可选审计到期且引用安全解除后，记录、已结束批次、无用key映射和引用索引都可删除，不留下每版本永久墓碑。仍被未解决批次成员需要的`LocalConfirmed`、版本和指纹继续保留；这部分生命周期由未解决责任约束，不能伪称已有硬性终态条数上限。一个真实未退出回调只保护其自身/关联批次，不阻塞无关健康组回收。

同身份Confirm/Release在记录仍在时沿原语义幂等；历史回收后旧ID查询/决定会明确返回不存在，不猜测成功、不确认新版本。无限幂等回执属于应用权威事实，不能要求Core为此永远保存每版本对象。

## 引用与有界执行

新版本原子维护前序反向引用和每次target尝试的批次成员引用。可合并旧版本变成Superseded时，新版本继承其更老未解决责任；在途回调未真实退出时不能压缩其直接引用。选批有界核对终态链后的原责任，不越过暂停/未知前序。

终态回收将后继PreviousID原子接到更老前序；所有原批次成员终态且关联回调实际退出后才退休批次，成员逐项归还引用，最后删除批次。部分失败批次仍能重建准确ConfirmedKeys/Unconfirmed，不把target自报列表当本地确认。

复用既有worker和BatchCommitter。自动维护按`HistoryAt`到期索引定位，每页最多min(ScanLimit,128)条、实际本地块最多8条；未到期项不反复扫描，仍受引用保护的到期项同事务移到下次核验时间，不能用追逐不断新增项的全局游标留下旧历史。事务TooBig二分，只有提交块计入维护结果。没有进展时最多每秒尝试一次维护（实际还受原worker等待影响），有回收进展继续有界排空。没有新Save也能继续，不新增worker/队列。大量新历史超过维护能力时存在积压和背压，应同时监控占用；不宣称无限输入下资源无条件恒定。

## 现有卷升级和安全维护

1. 停止该实例的新写，等待实际target/handler退出；确保原卷只有一个进程打开。必须使用原模型T、原前缀、原StoreID、policy和实例规范化目录，保留业务副本、原pending及应用权威回执。不能换成简化维护DTO或误开新空目录后用pending=0当恢复成功。不要删mode/索引，不调用PurgeLocal/Delete清历史。
2. 保留可核验备份，在隔离卷副本验证后，以正式新版本打开原卷。仅有界模式在绑定阶段自动升级为`terminal-history-v1:`格式门禁；旧policy指纹原样保留在标记中。
3. 启动接收写入前分段建立旧记录前序引用、终态回收索引及历次批次成员引用。进度同事务保存，中途退出后继续；不是先清空再重建。旧终态无完成时钟，不虚构“仍在审计窗口”，按可回收旧历史处理。未解决原件/attempt/序号/指纹和业务模型不动。
4. 不需要另调Upgrade。`NewReliableWriteStore`后、绑定前即可调用`WriteBehindHistoryUsage`只读记录旧卷分类；逻辑转换/Compact则必须绑定原真实target/handler。设置`AutoSync=false`，不执行ForceSync，不接收新写即可在维护进程里完成转换及显式分页：

```go
for {
    page, err := store.CompactWriteBehindHistory(ctx, 128)
    if err != nil { return err }
    // 保存Scanned/Stripped/Removed/Deferred，不输出key/payload。
    if page.CycleComplete { break }
}
usage, err := store.WriteBehindHistoryUsage(ctx)
```

一轮完成不表示所有项都可删除：一分钟诊断宽限、有效审计、未解决批次或实际回调可能仍引用。按原因等待后再分页，不循环重置计时或跳过责任。API允许limit=1..1000，事务块仍最多8条；取消/存储失败时保留已提交块，未提交部分不计完成，下次从持久到期索引幂等重扫；不会强杀已接受的本地提交或既有target/handler，运维前先停稳实际执行。`AutoSync=false`时显式维护由应用运维发起，Core不偷偷增加后台线程。

显式有界配置下，`WriteBehindHistoryUsage`/`GetWriteBehindHistoryUsage`无需先绑定target，在一次只读事务内分类当前可见key/value编码字节：业务副本、未解决记录、终态完整Item载荷、终态其余元数据、其他技术索引，以及真实pending索引数量。分类彼此不重叠；这是显式全prefix诊断，不能放在Save热路径，也不是物理LSM/VLog。

5. 逻辑历史回收后，Badger后台LSM压缩与原生value-log GC才可能缩小物理占用。既有后台GC每周期至多10轮；独立运维handle可在创建后、未绑定target时就显式限预算调用原生GC；这只回收原生垃圾，不能代替逻辑转换或删除仍存活的旧快照。完整逻辑维护后再按实际物理占用推进：

```go
store, admin, err := nosql.NewReliableWriteStore[Model](identity, cfg)
// 正常绑定、完成逻辑维护，保留上述store生命周期。
result, err := admin.ReclaimStorage(ctx, 10)
```

`maxRounds=1..100`，沿用本库`GCDiscardRatio`，仅调用Badger原生GC，不删存活key。返回Attempts/RewrittenFiles/NoRewrite；NoRewrite不表示所有历史已物理释放，重写次数也不是释放字节。取消只在原生单轮之间生效；并发原生GC可能返回ErrRejected，应保留结果并在后续维护窗口重试，不能忽略它宣称成功。操作作用于共享物理库，应在明确资源预算的维护窗口执行，不进入请求路径。

实际磁盘仍超过HardDiskBytes时，准入应继续拒绝，不能提阈值或清计数制造恢复。LSM/VLog原生大小采样可能滞后，应连同文件逻辑/分配字节和宿主可用空间观察。逻辑瘦身不保证立即降到阈值以内；仍需预留压缩/GC所需物理空间。不能用清业务目录恢复。

## 兼容和回退

公开旧构造、Save、target、failure handler、配置policy签名不变；新配置/API为加性。原Snapshot.Key/UpdatedAt及指纹不变，但终态Snapshot.Item默认可空，ListWriteBehindFailures不再是永久审计档案。读取终态的应用必须按新合同处理可空载荷和历史已回收。

普通legacy卷不转换。有界旧卷自动导入是单向转换：已实测v1.8.1在绑定阶段因mode不符拒绝转换后的卷。禁止新旧进程混开同卷、禁止删格式门禁降级。旧程序不认识引用/退休批次，不能安全直接回退；使用支持该格式的后续修复版本。若必须回退只能依据经过演练的升级前备份和完整业务事实对账制定恢复，不能丢弃升级后新事实。

正式可靠模式仍要求SyncWrites=true、DetectConflicts=true、CorruptionPolicy=fail。本次不接纳消费方私有SyncWrites=false试验，不调整Badger版本或其他配置；恢复可靠配置不反向保证历史异步刷盘阶段的完整性，仍需消费方独立对账。

Core组件通过不等于Bitzoom、Full-E4、业务UAT或容量通过；这些由消费方独立执行。正式证据见[验收报告](WRITE_BEHIND_TERMINAL_RETENTION_2026-11-03.md)。
