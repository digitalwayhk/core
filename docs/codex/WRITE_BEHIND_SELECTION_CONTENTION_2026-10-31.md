# 有界write-behind后台选批争用修复与验证（2026-10-31）

## 结论与边界

本补丁修正原选批范围，并减少重复扫描和重复事务写集。固定资源Linux历史场景三轮中，包含提交队列等待的选批轮次平均从17.19–17.49ms降至7.83–9.01ms；跨实例、CPU配置和业务模型均未修改。

**Save整体P99没有一致改善，不能宣布前台长尾完全修复或性能全面验收。** Linux历史场景P99旧版150.30–155.34ms、候选144.56–170.54ms；Mac候选还保留了一轮18.06秒最大值的异常，其确切原因未能由已有数据确定。本报告保留所有轮次，不挑最好一轮、不把未观测项填零。共享FIFO、确认提交、前台事务和运行时/I/O仍可能产生尾部等待。本次仅发布兼容的选批热点修正，不保证固定Save毫秒上限。

未修改、升级、部署或压测Bitzoom/Full-E4；Core组件结果不代表消费方功能、UAT或容量通过。只读取用户给出的本机证据，未访问远端、业务库或消费方卷。

## 基线与根因

当前正式基线是`v1.6.3 / e6e613979bc172e70e7b9654d0a9193b04451cd9`，Badger保持`v4.9.6`，Go使用本机已有1.26.6工具链。起始main已与origin/main相同，191项原WIP另存hash并保护。修复分支`fix/writebehind-selection-range`。

真实调用链：

```
后台 selectDispositionRound
 → updateWriteBehindTransactionKind
 → queueLocalCommit / submitLocalCommit
 → 原BatchCommitter FIFO
 → finishLocalCommit
 → updateLocalTransactionDirect
 → Badger.DB.Update（查找、原件核验、InFlight/attempt/batch/cursor原子写集）

前台 Save → SubmitBatch → 同一FIFO → applyWriteOperationBatch → 本地可靠提交 → 回复
```

`syncTarget.SyncBatch`、SQL和`HandleFailure`在上述本地事务之外；storageMu是进程内同prefix互斥，不是Badger文件锁，更没有跨MySQL调用持有。后台选批占用提交器时，已入队Save等待其退出，这是受控屏障测试真实证明的现行合同。

旧target/group/queue迭代器仅禁用value预取，Seek返回后才ValidForPrefix。固定Badger源码的Seek内部已经预取/parseItem及跳过不可见版本和删除标记，事后检查不限制这段范围；ScanLimit只限外层见到的组数。不能称为“每次完整扫描整个库”，主要问题也不是创建迭代器。

只加Prefix的真实对照没有解决历史场景：同target前缀内保留大量删除历史，仍每轮重查。故沿原函数继续缩减工作，不把Prefix-only结果当完成。Badger依据：[v4.9.6 iterator.go](https://github.com/dgraph-io/badger/blob/v4.9.6/iterator.go)。

## 生产修改（无新提交器/公共配置/磁盘格式）

| 文件与函数 | 实际变化 |
|---|---|
| `writebehind_disposition_schedule.go:selectDispositionRound` | target/group/具体组queue分别设置底层精确Prefix；group Prefix不含cursor，Seek才包含；每组创建自己的迭代器，不复用看不到后续pendingWrites的旧快照。 |
| 同函数 | 选中项的InFlight/attempt先在本轮内存形成，BatchID就绪后同一事务只save一次；失败预算、原件/PreviousID及组首核验不省略。 |
| 同函数 | 只有首次cursor、推进或回绕时写cursor；值未变化不重复Set，其它事务写集仍真实提交。 |
| `dispositionTargetID` | O(1)复用target范围结果，缓存绑定既有mode的Badger提交版本；每次仍Get mode进入冲突读集合。原target marker仍权威。 |
| `writebehind_disposition_store.go:saveDisposition / invalidateDispositionTargetRange` | ScopeTarget相关变更在原事务内重写mode原字节，使提交版本推进；普通ScopeKey Save不新增target存在性Get，保留原删除写集和历史。 |
| `writebehind_disposition_decision.go:applyDispositionDecision` | 从target scope退出时同事务失效；决策仍先核验版本和指纹，旧回执不改新原件。 |
| `writebehind_disposition_runtime.go:bootstrapDispositions` | 原恢复结束、绑定返回前预热冷范围；重开重建缓存，不靠首笔Save做冷查，不重置预算。 |

缓存只在既有storageMu内使用，最多一条ID与一个版本，不加TTL/线程/磁盘key。其它wrapper改变target后，mode版本使缓存失效；并发提交由Badger冲突检测拒绝旧快照。回滚不发布新版本。ScopeTarget状态变化也可保守失效；首次绑定或失效后的冷查仍可能受历史成本影响，不能承诺高频target变化无长尾。

包内可选observer只观察真实NewIterator/Seek和整轮，不记录payload或业务标识；nil时不采这些时钟。`target_cached`的零值只表示没有NewIterator/Seek，不能说已计量mode点读为零。v1.6.3的前台空组去重和底层Prefix保留；前台FIFO、1ms窗口、8项确认量子及原可靠结果边界均未改变。

## RED与正确性

原始证据位于`.temp/selection-range-20261031/`。每条命令与退出码另存`*-command.json`，失败日志不会覆盖成PASS。

| 原始文件 | 实际结果 |
|---|---|
| `red-behavior.log` | 真实Badger选批的三类底层Prefix为空，范围断言FAIL；不是编译失败。 |
| `red-repeated-target.log` | 三轮target Seek实际3次，期望1次；跨wrapper变更后mode版本未推进，FAIL。 |
| `red-duplicate-selection-write.log` | 8项×10KB的真实事务预算因同轮快照重复Set触发ErrTxnTooBig，原子选批成功断言FAIL。 |
| `red-startup-target-range.log` | 绑定后的首轮仍冷Seek一次，预热断言FAIL。 |
| `red-redundant-cursor.log` | 空轮/全暂停轮连续三轮Badger提交版本各增加3，不变cursor不重复写断言FAIL。 |
| `green-noop-cursor.log`及完整普通/race | 同一真实范围/版本/TooBig/无重复cursor写断言GREEN。 |

保留夹具失败：初次observer使用require.FailNow导致后台Goexit超时；初次大value走vlog未真正TooBig；初次跨wrapper错误地使用各自缓存pending而非持久索引；新增新PID测试曾关闭输入base而非解析后的实际manager目录，造成锁拒绝。均已修正夹具，原FAIL保留，不能当生产RED或抹去。

新增回归覆盖：同物理库另一合法prefix、target缺失/存在/指定恢复项；空/活跃/暂停/未知/等待前序组；cursor中间/末尾/回绕；未知旧版本及改组后继屏障；健康组推进；真实提交冲突与回滚；前台TooBig二分与选批TooBig整体失败；Callbacks在提交器之外；ScopeTarget跨wrapper/并发冲突/回滚；绑定预热及不变cursor实际写版本。旧未解决项不变为成功，原件/attempt/BatchID保持；旧确认不确认新版本。

新PIDtarget恢复测试从原卷重建缓存：暂停的target仍阻全部相关选取，原attempt=1和BatchID保留；只有应用显式真实Confirm后其它两组才恢复。既有SIGKILL测试继续覆盖target成功但本地未确认、确认后未回收、前台提交结果未知、接管前后硬杀；没有用同进程对象重建冒充进程恢复。

## 固定可靠性与工作量

A=仅前台；B=前台+空target自动后台；C=历史+持续新输入。三组各40000次Save、50并发、供给2000次/秒、512个可合并状态key、50个稳定独立组，**不是40000个不可合并订单**。合成完整Gob载荷2028B。全部可靠正式组保持SyncWrites=true、DetectConflicts=true；无false性能组。

C通过实际SaveBatch/DeleteBatch/确认/hook回收生成9000条成功删除历史，再9000条保留原件两次更新形成Superseded/删除历史；128个真实组首经未知结果及应用Pause持久化。没有清失败/完成历史或改真实消费者卷。新输入在独立健康组。

所有成功窗口均40000完成、0错误、512个当前模型/载荷核对，通过新PID打开原目录核验。A最终512 pending为故意保留；B最终0；C最终9000历史pending/128暂停保留，新输入完成同步，**不把历史pending写成全部排空成功**。较快后台可能在版本被后续Save合并前处理更多版本，target确认数量不同，必须连同实际工作量看，不能只按Save数量假定两组后台相同。

配置固定：Batch MaxBatch128/CollectBacklog1/CollectWindow1ms/Queue1024；SyncBatch128/Threshold32/Collect20ms/Drain0；准入128/2s/soft5000/hard10000；policy MaxAttempts3/MaxElapsed5min/retry1–10s/handler8次、2s、2worker/Scan128。实际BadgerOptions完整见[分段指标](evidence/writebehind-selection-20261031/metrics.json)，包括memtable16MiB、vlog1MiB、value阈值64KiB、2compactor和缓存预算；没有升级Badger。

Mac为本机APFS、GOMAXPROCS1、Go软内存1GiB，无硬CPU/内存隔离、非独占主机。Linux另用本机Docker现成arm64镜像，覆盖entrypoint运行静态Core测试，network=none、1CPU硬配额、硬内存4GiB/Go软1GiB，前后完全一致；4GiB容纳父测试及新子进程重开，不能写成1GiB硬限。使用自己的独占持久卷（非tmpfs），不改Docker全局资源、不操作现有容器、不启动Redis或连接SQL。平台分别对照，不能把换平台当修复收益。

Linux14轮全生命周期cgroup峰值内存约1.03–2.30GB；OOM/oom_kill均0；限流累计0–5.821ms（含准备/核验/重开），不能冒充单笔或纯20秒窗口。I/O原始io.stat随卷证据保存，非fsync计数。Mac既有swap后验快照不能用来证明18秒异常原因。

## 多轮持续结果

以下毫秒为每次Save完整墙钟分布，后者不除以所在批次大小。原始全轮分段见metrics.json；所有探索/差轮保留在本地证据归档。LinuxA各一轮校准，B/C各三轮；MacA/B/C各三轮并另有旧算法后置控制，不能把跨平台数据混成多轮中位数。

### Linux固定资源（三轮顺序交替）

| 场景/轮次 | 版本 | 平均 | P50 | P95 | P99 | 最大 | target处理项 | 实际写事务版本增量 |
|---|---|---:|---:|---:|---:|---:|---:|---:|
| foreground/0 | before | 12.501 | 9.604 | 30.491 | 56.616 | 83.249 | 0 | 827 |
| foreground/0 | after | 11.536 | 9.072 | 26.755 | 58.052 | 101.268 | 0 | 816 |
| background/0 | before | 11.368 | 8.154 | 31.611 | 67.558 | 99.811 | 32738 | 6233 |
| background/0 | after | 12.547 | 8.648 | 32.697 | 87.463 | 138.384 | 32912 | 6239 |
| background/1 | before | 11.677 | 8.380 | 26.977 | 81.405 | 207.079 | 30862 | 5999 |
| background/1 | after | 12.141 | 8.342 | 31.477 | 80.918 | 252.055 | 35258 | 6637 |
| background/2 | before | 14.460 | 9.672 | 41.562 | 104.184 | 167.614 | 24812 | 5011 |
| background/2 | after | 12.535 | 8.703 | 33.414 | 89.219 | 188.516 | 32660 | 6202 |
| history/0 | before | 16.884 | 9.980 | 44.211 | 152.696 | 280.011 | 12352 | 3219 |
| history/0 | after | 15.844 | 9.215 | 51.917 | 144.565 | 255.862 | 19893 | 4582 |
| history/1 | before | 18.419 | 10.448 | 58.011 | 155.341 | 328.041 | 11102 | 3013 |
| history/1 | after | 15.841 | 9.524 | 42.847 | 160.632 | 353.915 | 19442 | 4512 |
| history/2 | before | 17.819 | 10.200 | 53.892 | 150.303 | 287.773 | 11463 | 3059 |
| history/2 | after | 18.228 | 9.784 | 63.997 | 170.545 | 351.743 | 17952 | 4178 |

选批轮次含metadata排队，旧C均值17.185/17.334/17.495ms，候选7.827/8.051/9.009ms；target Seek累计旧2.89–3.46秒，候选该窗口0次冷查，分别806/782/722次命中。C的MetadataLocal累计事务时间旧6.55–7.45秒、候选5.05–5.22秒，同时候选做了更多轮/更多target版本。该项证明重复查询工作下降，不能与内嵌Seek累计时间相加。

C的Save平均两轮改善、一轮回退；P99一轮改善、两轮回退。B也没有一致尾部改善。组件热点减少与整体Save性能达标是两种结论，后者本次不通过。

### Mac最终候选三轮与相邻旧算法控制

| 场景/轮次 | 版本 | 平均 | P50 | P95 | P99 | 最大 |
|---|---|---:|---:|---:|---:|---:|
| foreground/0 | before | 11.262 | 9.065 | 24.554 | 47.751 | 105.845 |
| foreground/0 | after | 11.214 | 8.340 | 25.860 | 44.130 | 77.119 |
| foreground/1 | before | 11.220 | 9.302 | 23.979 | 42.525 | 82.451 |
| foreground/1 | after | 11.419 | 8.884 | 24.984 | 47.822 | 172.444 |
| foreground/2 | before | 11.542 | 8.172 | 25.795 | 50.111 | 84.865 |
| foreground/2 | after | 12.104 | 9.105 | 29.442 | 56.566 | 94.696 |
| foreground/后置控制 | before | 11.498 | 9.213 | 24.993 | 47.093 | 91.888 |
| background/0 | before | 11.458 | 7.527 | 30.975 | 70.753 | 140.025 |
| background/0 | after | 11.296 | 9.796 | 25.898 | 64.628 | 123.336 |
| background/1 | before | 11.440 | 7.776 | 34.191 | 66.187 | 112.923 |
| background/1 | after | 11.497 | 9.804 | 28.752 | 58.210 | 103.209 |
| background/2 | before | 11.064 | 7.394 | 28.264 | 63.811 | 117.631 |
| background/2 | after | 13.245 | 8.685 | 40.058 | 94.541 | 152.541 |
| background/后置控制 | before | 11.765 | 7.973 | 36.588 | 66.249 | 149.633 |
| history/0 | before | 12.885 | 6.843 | 44.370 | 116.280 | 167.560 |
| history/0 | after | 12.769 | 7.061 | 48.566 | 102.292 | 203.146 |
| history/1 | before | 13.796 | 6.766 | 55.305 | 121.318 | 239.696 |
| history/1 | after | 124.587 | 29.065 | 405.988 | 1497.876 | 18062.208 |
| history/2 | before | 13.389 | 6.929 | 55.238 | 120.388 | 203.442 |
| history/2 | after | 23.509 | 14.067 | 77.464 | 172.206 | 583.863 |
| history/后置控制 | before | 14.241 | 7.584 | 45.572 | 142.746 | 312.587 |

`settled-r1-history`的40000次Save耗时99.858秒、进程user+system约23.57秒，P99=1497.876ms、最大18062.208ms。未采集到该轮逐笔内部事件，不能补造分段或判定为fsync/GC/磁盘/选批CPU单一原因；它是未解释异常，保留为性能限制。没有仅重跑覆盖成PASS。后置旧算法C的P99=142.746ms、最大312.587ms，也说明常态长尾未消失。

Mac旧选批算法通过Go overlay恢复旧schedule/runtime，仅增加同口径包内观察；当时store中ScopeTarget失效的额外逻辑在全ScopeKey负载不触发。Linux、分段诊断及后续跨版本兼容另覆盖四个生产文件的原v1.6.3内容。不能把观察性覆盖二进制称作逐字官方发行二进制；各源码/overlay/二进制SHA均保留。

## 分段诊断（不计正式性能成绩）

使用仅测试Go overlay的有界事件记录器，把Save goroutine内的合成operation与真实result channel关联，再关联实际FIFO batch和事务边界，记录器零丢事件。正式源码没有这套逐笔记录器。旧221718、新232396个事件，分别40000 Save/1066批；每笔五段相加与完整Save误差为0ns。

| 排队与实际选批事务重叠 | 旧 | 候选 |
|---|---:|---:|
| 平均 | 0.432ms | 0.206ms |
| P99 | 12.579ms | 4.278ms |

两轮最大Save为208.851/206.958ms；它们排队与选批重叠都为0，候选该笔等待中76.908ms与确认事务重叠、119.793ms为未进一步拆出的调度间隙，所在前台事务约4.471ms。不能称全部来自选批，不能把事务全长重复加到该单等待上；这些是新的组件样本，不是现场70.847ms旧请求补分段。间隙包含尚未逐因区分的调度/合批/调用间隔，不能直接叫CPU限流或GC。

Admission、Batch队列、ForegroundLocal/MetadataLocal/ConfirmationLocal的QueueWait/LockWait/CommitDuration、前台批次均值、实际版本事务数、confirmed/target/pending趋势、CPU、heap/GC及Options已在metrics.json逐轮保留。累计phase为各调用之和，不等于进程壁钟。CommitDuration含本地事务框架/等待，不是纯fsync；Update成功含只读空写集，Committed调用数不等于物理同步次数。最大值类累计指标和RSS可能含C准备阶段，不能减出窗口最大值。没有采集物理fsync/msync调用数或独立纯fsync耗时，均为null/NOT_RUN。未额外建立观测线程。

## 命令与验证结果

所有shell遵守RTK。代表命令（实际命令数组、退出码和时长以原件为准）：

```bash
rtk proxy go test ./pkg/persistence/database/nosql ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -v -count=1 -timeout=300s
rtk proxy go test -race ./pkg/persistence/database/nosql ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -v -count=1 -timeout=300s
rtk proxy ./scripts/test.sh persistence-unit
rtk proxy ./scripts/test.sh config-contract
rtk proxy ./scripts/test.sh performance-contract
rtk proxy ./scripts/test.sh release-contract
rtk proxy ./scripts/test.sh web-dist-sync
rtk proxy ./scripts/check-logging.sh
# 显式窗口：不设环境变量时只SKIP，不能当性能通过
rtk proxy env CORE_SELECTION_PERF=1 CORE_SELECTION_MODE=history CORE_SELECTION_OUTPUT=/独占合成证据目录 GOMAXPROCS=1 GOMEMLIMIT=1GiB go test ./pkg/persistence/database/nosql -run '^TestDispositionSelectionPerformance$' -v -count=1 -timeout=300s
```

| 检查 | 实际结果 |
|---|---|
| nosql+04/07完整普通（最终） | PASS，退出0，478条PASS记录、53条SKIP记录，37.21秒。包含输出的子进程记录，不把它们称为478个唯一顶层测试。 |
| 同范围race | PASS，退出0，478条PASS、53条SKIP，54.59秒，DATA RACE报告0。 |
| 新PIDtarget重开 | PASS，原卷屏障/attempt/BatchID保持，显式Confirm后恢复。 |
| 真实硬杀三个确认窗口 | 普通/race最终轮均PASS，日志记录被杀及恢复的不同PID；未靠新Save触发。 |
| persistence-unit | PASS，退出0，36.83秒。 |
| config-contract / performance-contract / release-contract / web-dist-sync / logging | 均PASS，退出0。performance-contract含编译契约，不能当本次负载性能验收。 |
| 默认性能/子进程helper及外部MySQL等 | SKIP分别保留；性能显式运行原件另列，SQL外部依赖NOT_RUN。 |
| 整体Save P99全面改善 | 未通过；不能以组件热点下降替代。 |

第一次完整普通退出1是新增夹具错误的关闭目录，修正后新PID用例通过；第二次完整普通退出1为`TestCrashBeforeAndAfterHandoffConfirmation/before`的既有Badger打开memtable错误：`while opening fid: 1 err: Create a new file`。后续单轮完整普通、race及persistence通过不抹去该FAIL。

为区分回归另执行原选批算法和候选各20轮同一真实硬杀测试，**均17轮PASS、3轮FAIL，命令最终退出1**，均为上述打开恢复错误；不能写成“硬杀恢复稳定通过”，也不以相同比例断言绝对没有影响。该错误在v1.6.3此前证据也存在。本补丁未升级Badger、未改变此恢复实现，原始两份20轮日志保留。


## 兼容、升级与恢复

公共API/选项/StoreID/policy指纹/卷格式/现有默认不变；无需新增配置或重建卷。mode字节始终是原policy指纹，仅利用Badger自身提交版本。首次绑定/重开可能有冷查，发生在接受请求前；损坏marker明确返回错误，不当作空范围。保持原件、有限预算、未知结果、接管、同组屏障、部分成功及IsSyncAfterDelete版本保护。确认和异步物理回收仍是两个边界，重启不承诺补删已确认的内存回收候选。

另外以四文件v1.6.3原算法和当前候选分别创建同一类型的独占历史卷，在真正关闭manager后由另一二进制的新PID打开：`compat-old-to-new`与`compat-new-to-old`均退出0，9000原件、128暂停及attempt/BatchID、512同步输入保持。该两轮是离线格式/状态兼容验证，未计入正式性能成绩；不允许两个进程同时打开同一卷，也不豁免低于有界状态版本的旧二进制回退限制。

消费方在自己的维护流程升级`go get github.com/digitalwayhk/core@v1.6.4`并执行自己的旧卷副本恢复/功能回归。本轮不替消费方执行。`go get`不会自动安装skill；更新消费方既有链接或复制的权威分片，见发布说明。不能让不识别v1.5.0有界状态的旧二进制打开卷，不删除mode、pending、历史或索引绕过；同一卷只由一个实际进程打开。

## 证据、审查与未运行项

独立修复提交：`5d1b4e1a03b50bf49ed69d7af038195052d90ee1`。正式标签及实际发布检查另见[v1.6.4发布说明](RELEASE_V1_6_4_2026-10-31.md)。

| 证据 | bytes | SHA256 |
|---|---:|---|
| 本地`.temp/selection-range-20261031/evidence-prepublish.tar.gz` | 4642875 | `b6a6362c937506220e13c7d7c6b157bc0bc7cbd833f7e03712b29d351e39cdae` |
| 已提交`evidence/writebehind-selection-20261031/metrics.json` | 105355 | `0693447acb29ccdafd87c91890ef097c4d8a892b6158998151a70fdc643908c9` |

归档有793个成员（含成员清单），正文文件792项、75902319 bytes，逐成员恢复读取并校验大小/SHA通过；不含可再编译的大二进制，源码/overlay和二进制SHA绑定保留。归档仅本地，不上传GitHub原始附件。`*-command.json`保留精确命令/时长/退出码，`validation-summary.json`、`performance-all-summary.json/csv`及两份diagnostic-summary便于核对。原始FAIL、Prefix-only无收益及异常轮均在归档内。

`.temp`是本机证据路径，不随Go模块安装；公开仓库保留源码、可运行夹具、分段指标、RED及回归结果摘要（原始日志仅本地归档），消费方可按正式测试入口重新生成独占卷。原191项WIP逐文件hash核对不变；未改go.mod/go.sum和指针skill副本。

主Agent直接完成源码审查，遵守CLAUDE.md禁止子Agent规则；没有假称独立外审。检查了原子快照写入时序、跨组PreviousID、mode同事务失效/冲突、回滚缓存、cursor无写集、Close/重开、目标回调边界及FIFO。源码未涉及Redis/WS/MQ、业务数据库或依赖升级。

NOT_RUN：Bitzoom/Full-E4接入、部署、业务UAT/容量/同档现场复测；真实业务SQL/Broker；物理断电与文件系统损坏；长期稳态容量；Mac异常轮的逐笔根因；物理fsync调用次数与纯fsync耗时。原报告manifest只读验证匹配，未重新采现场：46项15446283 bytes；其physical-manifest.json自身7471 bytes，SHA256 `5b46885937ab5ba16e5dd4d6a3de3fb9127c4d2cd45b21ae01de5bd77d5a964b`，不混淆清单自身与成员总大小。
