# ReliableWriteStore 前台空组核验修复与验证（2026-10-31）

## 范围、源码与现场证据

基线HEAD `dffc1ff6d2bd0da2be358206f806c430c1d184a7`，分支 `fix/write-batch-ready-collection`。启动时191项既有WIP逐文件封存；不还原、清理或混入本次提交。仅修改Core的现有本地写与组索引核验、对应测试、正式文档和skill；未修改Bitzoom/Full-E4、消费方配置、模块缓存或依赖版本，未部署、发压或发布。

只读核对用户提供的中文报告及 `/tmp/bitzoom-core-save-root-cause-20261031/` 六项指定原件，记录path/bytes/SHA256。现场源码清单中的Core disposition store、operations、batcher以及Badger v4.9.6 iterator/txn共5项，与本地保存的修复前源码逐项匹配。没有在消费方执行命令、重新采集现场CPU或改动原件。

R185的23.046%、93.516%、67.830%等属于重叠的包含子调用CPU占比，不相加、不换算某笔请求毫秒，也不与R187另一次267.069ms样本相乘。现场两次GetLocal合计0.115ms，不能把本问题归为GetLocal或JSON主导。本报告另列Core本机合成对照，不代替Full-E4证据。

## 根因与最小修改

原调用链：BatchCommitter → applyWriteOperationBatch → 本地Badger事务 → ensureSyncQueueEntry → ensureDispositionEntry → saveDisposition(Superseded) → 立即deleteEmptyDispositionGroups → NewIterator/Seek/prefetch/parseItem → 随后才保存新版本队列项。

旧Ready、Attempts=0版本被替换时，即使新版本在同事务进入相同实际组，也先检查一次会被补新抵消的临时空组；合批内逐项重复。原检查只设PrefetchValues=false，在Seek之后ValidForPrefix，未通过IteratorOptions.Prefix限制内部查找，可能跨到相邻组的不可见版本和删除标记。这不是“每次完整扫描整个Badger库”，也不是Core持锁等待MySQL。

生产修改如下：

- `writebehind_disposition_store.go:ensureDispositionEntry`：复用内部可选deferredGroups；仍保存旧Superseded历史及原快照，比较旧新实际`dispositionGroup`，同组跳过临时空组核验。跨组保留待核验旧组；单项兼容入口也先补齐新版本再检查。
- `sharedbadger_operations.go:applyWriteOperationBatch`：在实际Badger事务闭包内建立集合，重复key和首次入队都传同一集合，所有写操作完成后统一核验。TooBig二分递归重新建立集合，失败事务不向子块泄漏。
- `sharedbadger.go:ensureSyncQueueEntry/BatchInsert`：传递同一内部集合；BatchInsert按实际事务收集。Set等原单项调用签名保持可用。
- `deleteEmptyDispositionGroups`：每个受影响组创建实际Prefix为`base+q:+group+:`、不预取value的迭代器，完成Seek判断后立即关闭。集合去重，仍在原事务内删组标记，不异步清理；不将旧迭代器复用到其创建之后的pendingWrites。
- `writebehind_disposition_runtime.go`：包内、实例级只读观测入口，默认nil，仅用于测试/诊断计数及核对真正传入Badger的Options。无公开配置、持久计数或业务标签，无额外worker。

保留原单提交worker、FIFO、锁、批次及并发预算、原件/版本/指纹/sequence/组队列原子性、attempt预算、暂停/接管/Release、成功提交后计数和清理、关闭及恢复。没有关闭SyncWrites、清历史、加TTL或升级Badger。

## RED与正确性覆盖

先只加入局部观测与测试，保留原算法。首个测试构建因夹具给ForceSyncAll多传参数失败，保留`red.log`，不计作RED；修正夹具后`red-behavior.log`出现真实断言失败：

| 断言 | 原算法实际结果 | 修复后 |
| --- | --- | --- |
| 同组替换1/64笔不做临时空组核验 | 分别1次/64次，FAIL | 均0次，PASS |
| 32笔同一旧组改组，按事务去重并设置底层Prefix | 32次且Prefix均为空，FAIL | 1次且Prefix精确匹配，PASS |
| 目标空组后有其他组历史/删除标记，实际Options限制范围 | Prefix为空，FAIL | 实际Prefix匹配，PASS |

真实Badger边界测试还覆盖：同事务1→2→1改组后的最终队列/组标记、最后一项确认后清空组、回滚后旧记录和下一事务组标记、实际TooBig拆分与后块失败的连续提交前缀、各子事务独立集合、在途/unknown/paused/retrying及已尝试Ready不被Superseded、迟到旧target不能确认新值、Set/BatchInsert兼容入口。

主要测试扩展于 `sharedbadger_operations_test.go`，复用已有合成模型/target及真实临时Badger。未新建测试框架，不用耗时阈值证明核验去重。原版本、指纹、部分确认、同步后清理、暂停/交接、Close及真实硬杀边界由所属包现有测试继续覆盖。

`TestDispositionGroupVolumeCompatibility`实际执行三个独立进程：旧算法创建32个原件和16次替换；新算法以相同StoreID/policy打开原卷，核对pending恢复、改组并排空；旧算法再打开，核对32个完成原件、80条原始/Superseded/完成历史，无已完成重投。这里的旧算法已支持有界处置，不授权更老、不识别该模式的版本读取此卷。

## 普通、race、门禁与既有失败

实际命令统一经RTK运行，完整参数/环境/退出码/耗时保存在`.temp/disposition-group-hotspot-20261031/*-command.json`：

```bash
rtk proxy go test ./pkg/persistence/database/nosql ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -v -count=1 -timeout=300s
rtk proxy go test -race ./pkg/persistence/database/nosql ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -v -count=1 -timeout=300s
rtk proxy ./scripts/test.sh persistence-unit
rtk proxy ./scripts/test.sh config-contract
rtk proxy ./scripts/test.sh performance-contract
rtk proxy ./scripts/test.sh release-contract
rtk proxy ./scripts/check-logging.sh
```

新增兼容入口后的完整普通与race均退出0，各455 PASS、51 SKIP、0 FAIL（含子用例；298个顶层PASS），race无DATA RACE。所属包原有确认前/确认后/结果未知及交接前后SIGKILL恢复均实际运行；外部MySQL等未配置用例仍SKIP，不算通过。五项脚本均退出0，release-contract是candidate检查，未创建tag或push。

必须保留的FAIL：第一次完整普通回归在`TestCrashBeforeAndAfterHandoffConfirmation/after`重开Badger时出现`while opening memtables ... Create a new file`，退出1。用原算法二进制单独20轮复核，相同失败3轮、通过17轮。失败也存在于修复前，不能算本次引入，也没有修复或跳过它。首次失败、旧算法复核日志和最终完整通过记录分别保留；最终通过不证明该既有偶发恢复失败已经消失，不以重试覆盖成全绿。此问题不纳入本次窄修复，不修改Badger绕过。

## 持续输入性能对照

同一台macOS arm64、同一APFS本地SSD、Go1.26.6、Badger v4.9.6；显式GOMAXPROCS=2/GOMEMLIMIT=1GiB，未使用Linux硬CPU配额或独占核心。实际Options保持SyncWrites=true、DetectConflicts=true、memtable16MiB、vlog1MiB、compactor2、Snappy，本地MaxBatch128/CollectBacklog1/CollectWindow1ms/Queue1024，Admission与有限失败policy不变。

每轮50并发、2028字节合成Gob、40000次Save，名义2000/s持续约20秒，复用2000个可合并状态key。无后台组隔离前台Ready替换路径；另有空目标后台组验证持续确认。跨组场景每轮在两套50组间切换；历史场景在两组间切换，并在计时外为相邻高位组构造4096个队列key的三次历史版本及删除标记，修复前后规模相同，不清历史换成绩。

同一性能夹具分别构建原算法和新算法二进制；主对照三轮，第二轮交换运行顺序，所有轮次串行执行，不并行编译/测试。固定相同的诊断覆盖仅对前台DB.Update闭包做计时：事务体单列，Update剩余段标为“提交及框架余量”，包含事务创建/提交/销毁等，未单独剥离纯Commit或物理msync。正常生产源码与普通/race构建没有这段覆盖；没有修改模块缓存。CPU采样另开两轮，不混入无采样统计。

25个窗口含1个修复前初始基线、18个三场景配对、4个后台配对及2个CPU诊断，共1000000次Save，错误0。计量外逐件核对50000个最终模型的key、组、完整载荷、删除和同步状态，并扫描真实pending索引；4个后台组共8000个最终模型已同步、pending=0；21个无后台组共42000个原件及pending保留，不能称同步收敛。合成卷核验封存后才删除。

下表为各轮统计值的中位数，最大值列取该条件各轮最坏最大值；不是把不同轮分位数合并成总体P99。全部逐轮值保留在summary.csv/json和原result.json。

| 场景 | 算法 / 轮数 | Save平均ms | P95 ms | P99 ms | 实际Save/s | 三轮/两轮最坏最大ms |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| 同组替换，无后台 | 修复前 / 3 | 10.226 | 14.655 | 25.049 | 2001.5 | 46.130 |
| 同组替换，无后台 | 修复后 / 3 | 9.641 | 14.467 | 20.710 | 2001.4 | 58.157 |
| 50组交替改组，无后台 | 修复前 / 3 | 10.186 | 15.233 | 20.434 | 2001.5 | 73.459 |
| 50组交替改组，无后台 | 修复后 / 3 | 10.617 | 14.268 | 21.365 | 2001.5 | 60.710 |
| 相邻组删除历史，无后台 | 修复前 / 3 | 23.937 | 45.482 | 51.005 | 1903.9 | 84.208 |
| 相邻组删除历史，无后台 | 修复后 / 3 | 9.941 | 15.070 | 21.145 | 1999.0 | 127.361 |
| 同组，空目标后台确认 | 修复前 / 2 | 7.259 | 14.115 | 28.229 | 2001.9 | 146.127 |
| 同组，空目标后台确认 | 修复后 / 2 | 7.355 | 14.411 | 25.536 | 2001.6 | 166.827 |

历史场景核验次数38000→785–793，同组替换38000→0；新算法所有组核验均有底层Prefix。50组分散跨组负载每批去重机会很少，次数38000→37992–38000，平均值约回退4.2%，P99中位数也未改善。每组独立Prefix迭代器会重新收集事务pendingWrites，属于这次收窄范围的成本取舍；未单独归因该场景的每一毫秒，不把它包装成所有负载均提速。空目标后台足够快时，本负载没有形成大量Ready替换，总核验数均40000，包含后台成功确认的核验，不能当成40000次前台重复核验。

历史场景P99改善没有消除所有长尾：修复后该条件仍记录127.361ms最大值，后台组最大值166.827ms。不能宣称持续15ms目标或消费方下单延迟已达标。输入有名义2000/s上限且受50个worker返回限制，实际Save/s是本轮完成数除以写入时间，不是最大容量；2000个最终状态不代表40000个不可合并业务事件逐一远端提交。

### 排队与事务分段

| 场景 | 算法 | 每Save平均排队ms | 每实际事务平均事务体ms | 每实际事务平均提交及框架余量ms |
| --- | --- | ---: | ---: | ---: |
| 同组替换，无后台 | 修复前 | 1.217 | 7.245 | 1.178 |
| 同组替换，无后台 | 修复后 | 1.294 | 6.487 | 1.283 |
| 50组交替改组，无后台 | 修复前 | 1.230 | 7.312 | 1.141 |
| 50组交替改组，无后台 | 修复后 | 1.221 | 7.705 | 1.169 |
| 相邻组删除历史，无后台 | 修复前 | 1.219 | 20.468 | 1.134 |
| 相邻组删除历史，无后台 | 修复后 | 1.210 | 7.141 | 1.106 |
| 同组，空目标后台确认 | 修复前 | 1.900 | 3.437 | 0.839 |
| 同组，空目标后台确认 | 修复后 | 1.946 | 3.294 | 0.884 |

排队按Save归一，事务体与提交余量按实际前台事务归一，分母不同，不能相加冒充单请求耗时。所有原始累计值、事务数量、CPU user/system、RSS/GC、pending趋势与排空时间均保留。无后台组pending增长到固定2000状态工作集后保留；后台组最终持久pending为0，不用缓存计数单独证明完成。

### 单独CPU诊断

| 包含子调用的采样累计CPU | 修复前 | 修复后 |
| --- | ---: | ---: |
| ensureDispositionEntry | 1.89s / 9.46% | 0.65s / 7.24% |
| deleteEmptyDispositionGroups | 1.28s / 6.41% | 0.07s / 0.78% |
| Badger Iterator.Seek | 1.16s / 5.81% | 0.05s / 0.56% |
| Badger Iterator.parseItem | 1.11s / 5.56% | 0.05s / 0.56% |

诊断覆盖负载及收尾，原算法样本CPU总量19.97s、新算法8.98s，分母不同；包含关系仍重叠，不能相加或按比例拆某个请求，也不将CPU总差全部认作上述函数直接消耗。原始pprof及完整top累计表保留。这个合成场景的热点比例不等于现场R185比例，未在现场复跑。

可复跑命令由证据目录的run-check.py/measure.py/performance-all.py给出，配合本提交的正式TestConfirmationPerformanceComparison。场景开关仅测试使用：`CORE_CONFIRM_PERF_GROUP_SCENARIO=same_group|cross_group|history_tombstones`；默认空值仍是原负载。CPU样本只在显式指定`CORE_CONFIRM_PERF_CPU_PROFILE`时写文件。消费者无需添加这些测试环境变量。

## 兼容、限制与交付

API、配置、StoreID/policy指纹、事件身份、原件版本/指纹、sequence、队列/组key布局和卷格式不变；不需新增应用开关或迁移。仍使用原ReliableWriteStore/UseWriteBehindWithFailureHandler。单实例同prefix提交串行和底层资源竞争仍在，修复只降低重复或越界的空组核验成本，不承诺所有负载吞吐或尾延迟改善，也不修改后台跨批调度、业务排序或SQL。

NOT_RUN：Bitzoom/Full-E4升级、部署、业务UAT、同档容量及真实SQL/Broker；物理断电；不可合并事件最大TPS和长期稳态；独占机器/Linux CPU硬配额；逐次fsync/msync/I/O及纯Commit分离；全仓普通/全仓race。Core组件通过不代表消费方通过。既有偶发Badger硬杀重开失败仍未修复。

相关现行说明更新在write-path-and-performance skill、WRITE_BEHIND_SYNC_GUIDE与WRITE_BEHIND_FAILURE_DISPOSITION_GUIDE。本报告随独立修复提交交付，具体SHA以git元数据和最终交付为准；本任务不自动合并或发布。

证据目录`.temp/disposition-group-hotspot-20261031/`保留原始RED、初始编译失败、既有恢复FAIL、实际GREEN/race/门禁、版本/源码绑定、全部性能差轮及CPU样本、各轮卷文件指纹和清理记录。最终归档与清理量见completion.json；不删除191项既有WIP、共享Go缓存、其他项目数据或消费方原件。

最终归档：`.temp/disposition-group-hotspot-20261031/evidence.tar.gz`，460178字节，SHA256 `19adb5a35d74817452c1f0c42269352ea484268cc5b2595015e2f4f19190daec`，148个证据文件逐成员核验。全部25轮合成卷及3个专用二进制已删除，剩余数为0。合成文件关闭后已分配空间分轮累计1649733632字节、二进制155598848字节；累计值不是同时占用或df净释放。其他项目或系统同时回收的空间不计本任务收益。
