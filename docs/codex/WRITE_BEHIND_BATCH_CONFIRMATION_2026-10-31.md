# 有界 write-behind 本地确认与公平提交（2026-10-31）

## 结果与范围

基线为正式 v1.6.1 / `46b7551bff0ee2d760993592e6f37519fd63e5a8`，分支 `fix/writebehind-batched-confirmation`。独立修复提交为 `63123e630988250a3af73ac2cb840bd2ad08b522`，固定小批预算修正为 `bcc186ea81f792297fd47f70218585916d394ac1`，发布提交另见 [v1.6.2说明](RELEASE_V1_6_2_2026-10-31.md)。

真实 Badger RED→GREEN：直接PrefixedBadgerDB的128条确认从129次提交降为5次，正式ReliableWriteStore固定8项预算为17次（含选批）；后台不能绕开已接受前台FIFO；有前台时及请求波次间隙，确认块都保持最多8项。版本、指纹、实际Commit、有限预算、组首、清理及进程恢复保持。没有关闭SyncWrites/DetectConflicts、删除pending/历史、放宽配额或升级依赖。

**性能不是全绿：** 本机保存平均、事务数和排空改善；持续窗口P99仍约60–85ms，部分P95回归。未证明60–70ms尾部全部消失，不能宣称任意机器Save低于10ms或整笔726ms问题解决。Full-E4同档复测NOT_RUN。发布修复不等于消费方UAT/容量认证。

只操作Core和本任务合成卷/副本。189个已有WIP逐文件SHA256保留；不改模块缓存源码、Bitzoom代码/配置/依赖，不使用SSH、Full-E4、业务MySQL/Redis或身份卷，不替消费方升级、部署或发压。

## 准确根因与调用链

前台：`Save → admission → BatchCommitter有界chan → ApplyWriteOperations/applyWriteOperationBatch → updateWriteBehindTransaction → storageMu → Badger.Update`。

原后台：`selectDispositionRound持久预占attempt/batch → target → 遍历selected，每条updateWriteBehindTransaction/completeDisposition → 每条可靠提交`。storageMu是本prefix运行时互斥，不是Badger单文件锁，不跨SQL target或HandleFailure。原syncExecMu仍协调同步轮次，前台不取该锁，不能说Core拿storageMu等待MySQL。

改后：`选批 → target（提交器外）→ 同轮结果分块 → 原BatchCommitter FIFO → 逐项核验/本地事务 → 提交成功 → 计数/清理候选/失败通知`。ReliableWriteStore固定最多8项、约1MiB编码软预算；直接PrefixedBadgerDB无facade时最多32项。瞬时没有前台请求也不放大facade块。实际Badger硬限独立生效，ErrTxnTooBig二分，不重调target；单原件不可拆分，超软预算时单项隔离，仍受Badger硬限。

失效项独立报错而有效项可提交；真正存储/Commit失败整块回滚，不泄露tentative确认、通知或hook。已提交块不因后块error丢失。sequence/cursor/组首和原件仍同事务、同prefix互斥保护，不按UserID分锁。最终写集复用一个不预取value的迭代器清理完成组标记，避免逐项重建和无关原件预取。

没有第二套队列、提交worker或每项goroutine。SQL、网络、重试计时、HandleFailure不进入唯一提交器。Close先停止前台准入/等待已接受写屏障，再关闭原prefix后台，最后关闭提交器；超时不能提前回复已接受命令或证明外部代码已停止。初始化失败与资源逆序关闭继续走原生命周期。

FIFO不抢占已开始的本地I/O；超大SaveBatch、其他prefix共享Badger、压缩/GC/系统调度仍可产生等待。直接PrefixedBadgerDB没有facade提交器时仍用原互斥和有界块；legacy/零配置不启用内部FIFO接线。

## 修改文件与函数

| 文件 | 函数/职责 |
| --- | --- |
| reliable_write_batcher.go / reliable_write_local_commit.go | 原FIFO内部本地命令、真实结果、前台/内部关闭域与屏障 |
| reliable_write_store.go | 构造接线、固定8项量子、Metrics复用原计数、Close顺序 |
| sharedbadger_operations.go | 前台事务分类计量，原可靠Save/Delete不变 |
| writebehind_disposition_schedule.go | 保留选批/target/原错误，委托有界结果落地，记录有界target时钟 |
| writebehind_confirmation_batch.go | 条数/编码预算、实际过大二分、逐项复核、提交后效果 |
| writebehind_disposition_store.go | 原事务包装、组索引迭代器复用、completeDisposition版本核验保持 |
| sharedbadger.go / writebehind_local_commit_metrics.go | 原SyncMetrics扩展本地事务快照，不加观测线程 |

公共方法签名、配置、policy指纹和持久格式不变。加性只读观测及采集边界见[同步指南](WRITE_BEHIND_SYNC_GUIDE.md#有界后台本地确认的提交预算)、[失败处置指南](WRITE_BEHIND_FAILURE_DISPOSITION_GUIDE.md#本地确认批提交与公平性)、权威skill及07 README。确认数仍用真实SyncedItems，块输入Items不是业务成功证明。

## RED/GREEN与正确性

| 场景 | 实际测试与结果 |
| --- | --- |
| 非逐条提交 | TestDispositionConfirmationUsesBoundedTransactions：直接prefix旧129>5 RED；同断言新5 GREEN。正式facade128确认17次（含选批），不混用两条路径 |
| 统一提交面 | TestBoundedLocalConfirmationRespectsAcceptedQueue：旧绕过前台直接完成RED，新等待原FIFO GREEN |
| 波次间隙 | TestReliableConfirmationKeepsSmallBudgetBetweenRequests：无在途Save时旧动态量子32>8 RED；固定8项 GREEN |
| 不只转移排队 | TestConfirmationYieldsSmallQuantumToForeground：首块32>8 RED，新前台存在时<=8 GREEN |
| 实际事务过大 | TestDispositionConfirmationSplitsActualTransactionSize：157286字节真实Badger硬限，1次ErrTxnTooBig、拆2个提交、8项确认 |
| 部分块成功 | TestDispositionConfirmationPartialBlocks：后半四个8项块的事务闭包失败均整体回滚，首32项/32个hook保留；恢复原字节后剩32项收敛 |
| 真实Commit失败 | TestConfirmationBlockRealCommitConflict：另一事务制造ErrConflict，4项未确认、pending=4、hook=0；原版本本地确认后收敛 |
| 失效/重复 | TestDispositionConfirmationStaleEntryIsolation及原清理测试：失效指纹/旧版本不确认新值，其他项前进，重复不扣数 |
| 前后台与停流 | TestDispositionForegroundAndBackgroundProgress；36组显式计量自动worker排空，无手动ForceSync替代 |
| 关闭真实结果 | TestLocalCommitCloseWaitsForRealResult：Close超时不提前回复；释放后真实提交、原worker退出 |
| 原清理合同 | LocalCleanupContract/BoundedCleanup系列：false→true、旧target/决定、指纹、并发Save、部分成功、事务失败、暂停/接管/Release、迟到及重开 |
| 原有限处置合同 | 完整nosql普通/race包含原预算、未知不盲重放、组屏障、健康组、等待handler实际退出及关闭超时 |

中间固定预算后，原“后半32项失败”夹具仅损坏最后一个原32项块的末项，实际只能回滚8项、导致56确认，fixed-final-unit之前的fixed-budget-unit与green-between-waves保留失败；现按后半四个8项块分别注入末项故障，原32确认/32待恢复断言不放宽。fixed-volume-backward首次命令二进制相对路径错误exit127，v2修正路径后实际通过，未更改断言。

首次red-transactions为测试字段名错误导致编译失败，不计逻辑RED；red-transactions-v2才是129次真实提交失败。首个拆分夹具未处理游标轮转，取到空集合，原green-batch-boundaries保留失败，v2修正准备条件后通过；未改生产游标规则。

中间批量化、仅FIFO及索引候选的性能结果全部保留，不算最终验收；部分中间短窗与构建并行，正式对照排除。默认volume-forward未设环境时只SKIP，不计兼容通过；实际guarded命令显式打开Core合成副本，验证非零原件/初始pending。

## 独立进程与原卷

TestConfirmationHardCrashRecovery的三个场景均真实SIGKILL，不同恢复PID打开原目录、没有新Save：

1. target已写应用独立Badger、本地确认前：恢复unknown，应用查询原事实后Confirm，target效果一次。
2. 本地确认后、hook物理删除前：pending=0、完成记录保留，不重投；内存清理队列不补删。
3. 前台可靠提交完成、Save结果未返回：原件在新进程自动同步，不丢件。

原TestCrashBeforeAndAfterHandoffConfirmation交接前后故障也在普通/race执行。SIGKILL恢复不是物理断电保证。

另用关闭的Core测量卷真实副本：新进程从v1.6.1的2000个pending无新请求自动恢复；旧v1.6.1进程初始化并核对新版2000个已完成原件/历史、attempt=1、不重调target。副本有显式marker；原测量卷SHA256不变。仅覆盖本组件合成模型，不授权在线回滚生产金融卷。

## 同机性能

现场只读：probe.go的SHA确为`0342c6ac435a3b4708f730a66993b2bfa1f2de699f405a3bf01bea5935eae9d0`。现场107/4199次fsync/msync与6.666ms属于另一次strace，不混入下表；未运行该Linux二进制或进入Users宿主，其SHA仅按原构建绑定参考，不伪称复跑。

本轮macOS arm64/APFS、已有Go1.26.6，GOMAXPROCS=1、GOMEMLIMIT=1GiB、SyncWrites/DetectConflicts=true、2028字节合成完整Gob、50并发、相同Batch/Admission/Failure参数。Go内存为软预算，**不是1CPU/1GiB容器硬限**，RSS可超过1GiB。未调Badger参数或依赖。精确旧生产树46b7551只附加同一计量测试；两版计量源码相同，源码/二进制/文件系统指纹见fixed-budget-performance-binding.json。

每版三模式各3轮2000笔短窗，再各3轮40000笔持续输入（50笔/25ms，名义2000/s、约20秒）。持续窗口复用2000个可合并状态key，避免无后台控制组撞原pending硬限；不是不可合并流水或长期稳态容量认证。最终计量串行、不与其他本任务构建/测试并行。36组共756000次Save，错误均0；有后台组最终pending=0；无后台组每卷仍2000个合成pending，不冒称同步收敛。

下表为3次独立运行范围，不是合并样本分位数或置信区间。candidate为最终固定8项量子实现。完整逐轮结果及CPU/RSS/GC、批次、排队、pending趋势/排空在附件performance-summary.csv/json。

| 模式 | 窗口 | 版本 | 平均ms | P50ms | P95ms | P99ms | 最大ms | 本地事务 | pending |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| plain_no_sync | 2000短窗 | v1.6.1 | 1.616–1.628 | 1.578–1.599 | 1.720–1.781 | 2.203–2.640 | 2.307–2.646 | 43–43 | 2000 |
| plain_no_sync | 2000短窗 | candidate | 1.606–1.653 | 1.579–1.602 | 1.742–2.136 | 2.540–2.616 | 2.637–2.650 | 43–45 | 2000 |
| plain_no_sync | 40000持续 | v1.6.1 | 3.970–4.490 | 3.540–3.732 | 6.165–8.713 | 14.142–21.003 | 37.105–40.704 | 821–827 | 2000 |
| plain_no_sync | 40000持续 | candidate | 4.090–4.273 | 3.567–3.979 | 7.344–7.967 | 13.929–18.731 | 27.096–46.802 | 818–824 | 2000 |
| bounded_no_sync | 2000短窗 | v1.6.1 | 3.561–4.500 | 3.460–4.433 | 4.385–4.847 | 4.454–6.949 | 5.013–6.967 | 41–42 | 2000 |
| bounded_no_sync | 2000短窗 | candidate | 3.640–4.520 | 3.546–3.847 | 4.565–7.135 | 4.712–10.527 | 4.844–10.548 | 41–42 | 2000 |
| bounded_no_sync | 40000持续 | v1.6.1 | 16.259–16.886 | 13.675–14.266 | 20.175–24.647 | 38.606–78.725 | 328.505–481.627 | 822–833 | 2000 |
| bounded_no_sync | 40000持续 | candidate | 11.636–11.895 | 9.395–9.901 | 26.105–27.810 | 47.880–51.206 | 88.550–107.488 | 836–849 | 2000 |
| bounded_fast_sync | 2000短窗 | v1.6.1 | 6.499–6.614 | 4.964–5.215 | 9.559–10.117 | 31.529–36.047 | 31.628–36.290 | 2083–2086 | 0 |
| bounded_fast_sync | 2000短窗 | candidate | 4.426–4.508 | 3.150–3.346 | 7.466–7.894 | 21.938–22.866 | 29.207–31.937 | 368–369 | 0 |
| bounded_fast_sync | 40000持续 | v1.6.1 | 14.859–15.998 | 12.216–12.758 | 23.883–25.136 | 106.291–117.536 | 381.928–422.722 | 26223–28329 | 0 |
| bounded_fast_sync | 40000持续 | candidate | 10.181–10.786 | 6.892–7.103 | 28.068–32.067 | 60.212–85.258 | 106.976–122.494 | 7549–7778 | 0 |

短窗前台每操作排队约1.37–1.49ms→0.47–0.55ms，平均提交约4.62–4.83ms→2.08–2.13ms，Save平均同步下降，不只把Mutex等待改名。持续窗口前台每操作排队约2.75–3.21ms→2.77–3.18ms，没有明显下降；平均前台提交10.25–11.31ms→3.64–3.82ms，Save平均14.86–16.00ms→10.18–10.79ms。不能把排队代价隐去，须与更短提交和实际Save共同判断，不能将嵌套或并行累计时间相加冒充壁钟。

**性能未完全验收：** 持续后台P99仍60–85ms，部分P95高于旧版，不能说独立60–70ms尾部完全消失。剩余时间涉及本地事务/元数据、历史、压缩、Go GC/调度及I/O，未逐次系统调用归因。空target只排除该探针SQL/网络等待，不排除所有本地争用；没有新设业务100ms门槛。

fsync/msync次数、纯fsync、I/O调用、CPU限流本轮未采集，JSON为null/NOT_RUN，不将Badger事务数乘2猜测。user/system CPU、堆及GC为窗口实际数据；rusage RSS是进程历史峰值，Darwin原生单位bytes，不能当每组独立最大。Badger.Size可滞后，0不表示无物理文件；物理manifest为关闭后快照，不是窗口I/O速率。

## 实际命令与证据

全部shell经rtk/rtk proxy。run-check.py逐条保存真实参数、Go PATH、GOTOOLCHAIN=local/GOPROXY=off/GOSUMDB=off/GOFLAGS=-mod=readonly、退出码和壁钟；清除外部数据库开关。不把默认SKIP算PASS。

```bash
rtk proxy go test ./pkg/persistence/database/nosql -run '^TestDispositionConfirmationUsesBoundedTransactions$' -v -count=1
rtk proxy go test ./pkg/persistence/database/nosql ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -v -count=1 -timeout 300s
rtk proxy go test -race ./pkg/persistence/database/nosql ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -v -count=1 -timeout 300s
rtk proxy go test ./pkg/persistence/database/nosql -run '^(TestConfirmationHardCrashRecovery|TestCrashBeforeAndAfterHandoffConfirmation)$' -v -count=1 -timeout 120s
```

性能绑定baseline-final.test/fixed-budget.test：`-test.run '^TestConfirmationPerformanceComparison$'`为三短窗及首持续，`-test.run '^TestConfirmationPerformanceComparison$/3/' -test.count=2`补两次持续；各*-command.json含实际独立输出目录和环境，不用此处简写替代。

证据根为`.temp/writebehind-batch-confirm-20261030/`，保存原始RED、所有未达标候选与最终普通/race/进程/计量日志。主要文件：

| 文件（证据根目录下） | bytes | SHA256 |
| --- | ---: | --- |
| red-transactions-v2.log | 3762 | 01f55c8e09476f9a263fc09429f717bd23b9eec00f8e6befb86d43f7f0b16cdd |
| red-shared-queue.log | 2455 | 8f8d309b5fe6ca015269b5b42c6e5e065904fa34358ecbd08d5ebd56e3323259 |
| red-foreground-quantum.log | 2443 | df67f6db2280ee904afaca4679d99f6e6ea14fe61fac1ddc4017bebc194370af |
| red-between-waves.log | 2286 | fec07c62a0a02b5a0f4bfc2a726ce8706cef5ec4d258a741a34b1778d669a413 |
| fixed-final-unit.log | 753854 | 317818785e0720a05948d56655e26e869dfad79c1cfdd818e09b208bfc6c68ce |
| fixed-final-race.log | 760362 | 98d6b393a2aa72ad5e4851ec9854c7bf0a563deec8852b66531687e7a8cb7d97 |
| fixed-budget-boundaries.log | 21825 | 5aff5c4f3a88b0745861f300a70abf10db7354207b5572a2dc8821d8451d5d88 |
| baseline-final-perf.log | 241260 | c25aa510869d4126fa19ad6098ea47ab85816602094de2ab40375db1260130ce |
| fixed-budget-perf.log | 403743 | 851bccc953254d9aa4d9bc71e2cc10d875ee12fbe392335ec6603717e5f040c6 |
| baseline-sustained-repeat.log | 317875 | e632cc575da5b2a24416a38db3ee652473e33db9a10d084d5b09ad9decb4c0f5 |
| fixed-budget-sustained-repeat.log | 586685 | 48e9a767167fa57f016fd387d13422bdde22321e39d873085f742d15674af142 |
| fixed-volume-forward.log | 12483 | 4762cf447013914ba7ff28a2c0dfb046d80444fb12a8e22a1b071c70697ccc14 |
| fixed-volume-backward-v2.log | 1966 | f50b2ecbc0856fd901d774d1b35f29adcd4e69ab3ec867ba01d9fc37ff0f463d |

完整manifest包含path/bytes/SHA256和实际退出码；发布附件另保全命令、原始失败、逐轮结果及正式Go代理核验。最终固定预算普通/race各436 PASS、50 SKIP、0 FAIL；独立硬退出子进程也在两者执行。persistence-unit/config-contract/performance-contract/quick/security/release-contract/web-dist-sync/日志门禁以最终manifest为准，不用计划冒称执行。

## 升级、回退、NOT_RUN

升级正式tag后继续原UseWriteBehindWithFailureHandler和policy，无需新配置或临时提交器。存量原卷保持身份路径/StoreID/policy；不清mode/pending/history迁移。加性观测字段不改变方法签名、业务协议或状态格式。

旧已确认残留不自动补删，hook仍在原版本本地确认提交后执行；false保留、后续true须新版本确认。接管不等于业务成功或解除顺序屏障。物理清理和应用失败决定仍可逐项提交，没有宣称所有后台写均无放大。原卷不并发开旧/新进程；停稳target/handler、保留备份、副本验收后再决定回退。组件双向兼容不取代生产卷/领域模型验收，禁止回到不识别有界状态的旧Core。

NOT_RUN/NOT_PROVEN：Full-E4同档硬资源复测、消费方接入/部署/业务UAT/容量、真实MySQL/Redis/MongoDB、物理断电与全部I/O故障、OS同步调用/限流拆分、全仓普通/全仓race、所有60–70ms尾部消失。未操作Bitzoom/Full-E4，不宣称解决Users保存前排队、用户锁或MySQL交错写表死锁。主Agent自审，遵守仓库禁止子Agent，未冒称外部独立审计。
