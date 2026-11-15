# 同store独立组排空修复与验证（2026-11-16）

## 根因与最小实现

基线正式main/官方v1.12.1为 `cec637fa9e09fee41c678b273423a5314ab6aba8`。先核对源码与消费方既有只读交接；没有操作消费方仓库、Full-E4、压力、身份卷、业务数据库或消息。原消费方测试和四份封存证据保持不变。Core自己的真实Badger RED并不依赖该消费方测试包。

旧链路是 `ForceSync/自动worker → processDispositionRound → syncExecMu → selectDispositionRound → target.SyncBatch → commitDispositionResults`。应用target内部独立事务已经成功的组，其结果仍需等整次SyncBatch返回才能交回Core。此时B2 Save已完成也不能越过B1，形成同store跨组完成屏障。这是原批次结果合同的性能边界，不是已证明业务丢失或SQL在本地提交器执行；不能认定全部现场10秒等待同因。

显式新链路：原绑定入口加 `WriteBehindExecutionOptions{IndependentGroups:N}` → 原短互斥选择/预留组和原版本 → 固定N个worker独立调用原target → 原版本/指纹/attempt/BatchID核验及有界本地确认 → 真实提交后计数/hook/释放原组槽 → 可补健康组后继。原SyncBatch接口和默认整批行为保留。原同key前序、有限失败handler、原错误、未知先确认、scope、FIFO、可靠刷盘和终态nil Item均保持。target/SQL/失败回调不进入本地提交器事务。

具体修改：

| 文件 | 函数/责任 |
| --- | --- |
| `writebehind_execution_options.go` | 加性执行选项与1–64预算校验；0保留旧模式 |
| `writebehind_group_execution.go` | 固定worker/总槽、手动结果等待、自动持续调度；TooBig只缩小已回滚的本地选批 |
| `writebehind_disposition_runtime.go` | 新绑定接线、生命周期、原handler保持、准备期诊断observer |
| `writebehind_disposition_schedule.go` | 复用target/确认函数；每次实际target独立BatchID；在途target不误转Unknown；handler超时仍暂停 |
| `writebehind_confirmation_batch.go` | 明确BatchID核验、真实提交后包内确认时钟；原分块/清理保持 |
| `sharedbadger.go` | 独立组共享条带读锁；不同key碰撞不互挡，强制删除与普通路径仍排他 |
| `reliable_write_store.go` | 门面显式绑定方法，原调用不变 |
| `writebehind_disposition_store.go` / `writebehind_failure.go` | TargetActive/Queued/Limit与既有Handler计数分开 |
| 示例07组合根/订单store | 显式传递执行选项，缺policy拒绝；默认0、资源逆序保持 |

正式API、启用条件、有限槽背压、ForceSync语义及同代卷升级/回退见[指南](WRITE_BEHIND_INDEPENDENT_GROUP_DRAIN_GUIDE.md)。没有新依赖、公共业务协议、配置字段、StoreID/policy指纹或卷格式；不自动启用SyncMaxConcurrency。不能混入跨组原子事务target，不能并发用不同wrapper接管同一prefix。

## RED、失败原件与GREEN

- `red-real-badger`：exit 1；A1/B1在不同应用组，B1 target已返回，但pending仍2，`B1已返回，但本地确认仍等待A1`。真实语义失败，非零观察量，非编译错误。
- 最小 `green-initial-fixed`：同一核心断言PASS；B1真实确认后B2在A释放前执行，最终3确认。第一次候选编译命名冲突记录为编译FAIL，不算RED。
- 夹具不完整自适应参数、把全前缀key当业务key、解引用已瘦身终态Item等测试失败均保留，修正夹具后未放宽原件/确认断言。
- `final-targeted` 捕获候选实际回归：把全部active组过早略过，会阻止超时handler按原合同持久暂停。修正只对InFlight target提前保护；handler先持久暂停，实际回调仍持组，人工Retry也不重入。原FAIL保留，后续 `final-targeted-handler-fixed` PASS。
- 新模式覆盖自动/手动、B后继、未知/暂停/应用Confirm、取消不合作target、确认入口失败、旧版本跨组更新、哈希碰撞、原错误加真实确认、独立BatchID/错误链、handler超时未退出、强制删除排他、实际Badger TooBig缩块、旧入口/零值及同代原卷重开。
- 真实进程恢复：旧PID在A应用事实已可靠保存而Core未确认、B1/B2已真实本地确认时SIGKILL。新PID打开同一Core/app目录、无新Save，A走应用事实确认，B1/B2不重做，原Attempts=1且三项Completed。原应用事实库是独立真实Badger，不伪装SQL或金融验收。原有target成功前后、Save结果未知、历史/清理硬杀等回归同时保留。

所有普通、race、进程与门禁命令、退出码、时长、bytes/SHA256保存在本轮私有原件目录，交接回执提供路径和校验；不公开原始日志、内部路径或消费方原件。实际门禁与命令见下表；全部跳过项原理由保留在私有逐测试原件中。

## 首轮有效持续对照

同一台macOS、本地APFS独立临时目录、GOMAXPROCS=1（不是1CPU硬配额认证）、SyncWrites=true、DetectConflicts=true。每方案16个应用组并发持续输入120轮，每50ms各组1笔，共1920笔；约2KiB载荷，1组target受控40ms，其余1ms。两者target并发预算均4、SyncBatchSize=4、ScanLimit=16、原FIFO 128/1ms/CollectBacklog=1/Queue1024、准入128/2s；不同方案各自新库，不修改历史或放宽可靠性。

0表示候选保留的旧整批路径，4表示候选独立组；此表不是未经仪器的现场v1.12.1二进制，也不等于MySQL容量认证。逐项确认时钟在本地确认事务真实提交之后采集，未使用异步删除hook计时。Save计时包含原准入/排队/提交；分段累计在原始Sync/Batch指标中，不与请求壁钟相加。pending排空观测每5ms，结果指标刷新单列，不后移排空起点。

| 轮 | 独立组数 | Save平均/P50/P95/P99/最大(ms) | 健康组Save开始→真实本地确认平均/P50/P95/P99/最大(ms) | 确认/总墙钟秒 | 输入停止时pending | 排空秒 |
| --- | ---: | --- | --- | ---: | ---: | ---: |
| 1 | 0 | 4.300 / 3.997 / 7.298 / 7.694 / 38.099 | 460.33 / 423.51 / 986.79 / 1034.21 / 1088.66 | 272.57 | 276 | 1.085 |
| 1 | 4 | 4.087 / 4.155 / 6.651 / 7.821 / 9.364 | 12.53 / 11.77 / 20.48 / 31.87 / 56.64 | 319.17 | 17 | 0.060 |
| 2 | 0 | 4.014 / 3.671 / 7.204 / 13.975 / 14.693 | 425.72 / 397.21 / 887.65 / 947.15 / 992.38 | 276.34 | 256 | 0.990 |
| 2 | 4 | 4.801 / 4.519 / 6.999 / 17.551 / 19.673 | 14.40 / 13.54 / 22.40 / 55.45 / 91.88 | 316.97 | 18 | 0.100 |
| 3 | 0 | 3.970 / 3.661 / 5.584 / 8.545 / 17.663 | 489.78 / 455.06 / 996.34 / 1049.35 / 1097.99 | 272.40 | 288 | 1.094 |
| 3 | 4 | 4.065 / 4.140 / 5.728 / 6.817 / 7.903 | 12.68 / 12.31 / 20.94 / 34.41 / 47.63 | 318.37 | 17 | 0.075 |

六方案均1920确认、错误0、最终pending0。健康组确认长尾显著下降；Save P99不是每轮改善，不能承诺稳定15ms。代价是确认事务约481→1920，target调用与元数据事务增加；本机测得壁钟确认率约272–276/s→317–319/s，不说明所有更快target、组数量或SQL场景都会获益。

原始失败性能轮不能认证：最初未显式配置准入，落到默认1并发/1ms，一轮1920 Save含1629次拒绝。随后一次pending刚到0时同步计数还为1919，断言失败；已改为独立等待真实轮次指标完成，排空时间仍取先前pending=0时点。不能删差轮或用零错误要求覆盖原件。CPU为当前进程Getrusage用户/系统累计增量；MaxRSS为进程至今高水位，堆/GC为测得快照，不能冒充每方案独占峰值或稳态内存。CPU限流、物理I/O、纯fsync与生产SQL在本机未采集，保留null/NOT_RUN。

## 兼容与未执行边界

旧入口/零值、同步前原件、未解决历史、有限预算、同key旧结果不确认新版本、真实本地提交、原异步清理和nil终态保留继续原合同。无格式迁移；同代卷停稳后显式启用，回退先关闭选项并核对副本，不能回到不认识当前终态格式的旧二进制，也不能删mode/pending。当前进程取消可先返回，但旧worker持续占槽至真实退出，关闭超时不能当成功。全N槽被不合作代码占满时仍背压；每个组自身超出处理能力仍积压。

本轮不操作Bitzoom/Full-E4。消费方原业务target真实MySQL幂等/死锁/事务结果、Broker积压、跨实例业务全局顺序、金融事实、现场延迟归因、UAT和容量均NOT_RUN。没有把组件进程测试当Redis/MySQL/WS全链路；相关未配置外部用例维持SKIP。未验证64worker长稳态、物理断电和服务端Broker崩溃。正式skill已同步，消费方升级模块后还需按README重新同步或确认其skill链接，go get不会自动安装skill。

## 实际普通、race与发布候选门禁

| 命令（均经rtk proxy） | 结果 | 实际时长/边界 |
| --- | --- | --- |
| `go test ./pkg/persistence/database/nosql -json -count=1 -timeout=300s` | PASS | 37.02s；顶层276 PASS/51 SKIP；随后取消错误分类的小修由最终race和persistence-unit覆盖 |
| `go test -race ./pkg/persistence/database/nosql -json -count=1 -timeout=300s` | PASS | 63.91s；顶层276 PASS/51 SKIP；真实独立组硬杀PID68355→68356 |
| `go test -p 1 ./examples/04-shop-performance/... ./examples/07-shop-order-scale/order-service/... -json -count=1 -timeout=300s` | PASS | 105.36s；68 PASS/7 SKIP |
| 上一示例命令增加 `-race` | PASS | 184.17s；68 PASS/7 SKIP；07的0/2组模式资源逆序均覆盖 |
| `go test -race ./examples/integration/04-shop-performance -json -count=1 -timeout=300s` | PASS | 90.30s；4项顶层PASS，真实HTTP/进程恢复/默认模式 |
| `bash scripts/test.sh persistence-unit` | PASS | 35.17s；最新源码全持久层普通回归 |
| `bash scripts/test.sh config-contract` | PASS | 16.53s |
| `bash scripts/test.sh performance-contract` | PASS | 14.23s；契约门禁，不冒充性能压力 |
| `bash scripts/test.sh release-contract` | PASS | 94.00s；CI、API兼容、public-api、配置、安全；仅加性API，无基线覆盖 |
| `bash scripts/test.sh quick` | PASS | 16.00s；logging、server vet及基础包 |
| `bash scripts/check-web-dist-sync.sh` | PASS | 0.66s；没有修改前端或内嵌产物 |

正式clean提交的 `CORE_RELEASE_VERSION=v1.13.0 bash scripts/release-check.sh --release` 在发布步骤单独执行并记回执；表内candidate通过不等于已经发版。

按仓库禁止子Agent约束，开发、测试和五轴代码检查由当前主Agent完成，不把自审称独立审计。审查修正还包括：调用者取消仅返回取消语义，不能误报store已经关闭；真实关闭另外拒绝迟到成功；target实际调用分开BatchID，不把各自事务的未知集合混成一批。

## 最终固定二进制与官方旧源码对照

全部其它测试进程结束后串行测量：官方v1.12.1生产文件从精确提交逐字读取，以Go overlay仅加入相同的本地确认提交后observer，不改选批/target/确认/锁/可靠性；不修改原工作树或模块缓存。新API/测试从基线构建中删除，测量文件只调用旧绑定。完整逐文件官方SHA/overlay SHA及两个二进制绑定保留在私有原件中。该基线是有明确计时插桩的旧源码，不冒称原发布二进制。

- 基线二进制：61,869,106 bytes，SHA256 `2b03d95c514cfee2062da6d4c72d4c4d4da32c346ff04c509b558c120b212c0d`。3轮实际执行PASS，22.55s。
- 最终候选二进制：62,457,890 bytes，SHA256 `4661021713e7ee012eb51dcea38d875447b207c5404b63915ab583205665ad07`。3轮旧/新成对实际执行PASS，41.40s。
- 两者均通过 `go test -c` 构建，运行时显式 `CORE_TEST_GROUP_PERFORMANCE=1`，同一测试函数、载荷、可靠性、CPU并行度、FIFO和准入参数。没有其它本轮压力并跑。每一方案均1920确认、0错误、最终pending0；结果差轮保留。

| 方案 / 轮 | Save平均/P50/P95/P99/最大(ms) | 健康组真实确认P50/P95/P99(ms) | 确认/总墙钟秒 | 排空秒 | 进程CPU用户/系统累计秒 |
| --- | --- | --- | ---: | ---: | --- |
| 官方1.12.1+计时 / 1 | 4.513 / 4.188 / 7.139 / 12.655 / 34.896 | 381.47 / 1065.68 / 1127.60 | 269.10 | 1.175 | 1.445 / 0.493 |
| 官方1.12.1+计时 / 2 | 4.650 / 4.022 / 6.773 / 13.733 / 59.121 | 527.51 / 1155.15 / 1217.51 | 265.81 | 1.265 | 1.516 / 0.500 |
| 官方1.12.1+计时 / 3 | 3.815 / 3.702 / 6.530 / 14.645 / 25.075 | 411.19 / 736.39 / 806.52 | 281.76 | 0.860 | 1.249 / 0.425 |
| 候选0 / 1 | 4.053 / 3.507 / 5.852 / 17.117 / 48.222 | 319.78 / 835.77 / 881.05 | 278.90 | 0.930 | 1.185 / 0.428 |
| 候选4 / 1 | 3.951 / 4.098 / 5.439 / 8.216 / 11.347 | 12.22 / 20.97 / 42.59 | 318.54 | 0.060 | 1.281 / 0.527 |
| 候选0 / 2 | 4.399 / 3.984 / 6.607 / 10.421 / 18.359 | 476.70 / 1156.02 / 1213.55 | 266.28 | 1.254 | 1.501 / 0.501 |
| 候选4 / 2 | 4.314 / 4.384 / 6.377 / 7.802 / 10.780 | 12.48 / 20.72 / 25.93 | 319.11 | 0.060 | 1.304 / 0.497 |
| 候选0 / 3 | 4.000 / 3.536 / 6.165 / 12.966 / 28.191 | 476.35 / 1138.28 / 1211.87 | 265.94 | 1.262 | 1.404 / 0.508 |
| 候选4 / 3 | 4.304 / 4.175 / 6.790 / 10.342 / 11.105 | 13.34 / 21.98 / 30.71 | 319.15 | 0.060 | 1.378 / 0.550 |

最终候选健康组确认长尾和排空改善，但Save长尾有波动，不能承诺所有环境改善或15ms硬目标。前台/元数据/确认的队列等待、锁等待、事务数量、提交耗时、pending峰值与GC/堆完整数值在原始JSON；它们是各自累计值，不能与表内请求分位数相加。真实SQL、限流、物理I/O和稳态容量仍NOT_RUN。前表有效轮与本轮均保留，不挑选单一最好轮作结论。

## 交付前追加缺口与v1.13.1补丁

在v1.13.0正式发布、尚未向消费方宣称完成的最终检查中，新增真实受控RED：AutoSync=false、A仍在途、B1已暂停，手动调用选批未找到可执行B；应用在选批结束到等待前确认B1，但仅原自动syncTrigger收到通知，独立组代际等待未被唤醒，B2继续等A。`manual-notify-red` exit1，1秒受控窗口内B2不进入target，原日志保留。这是实际SDK缺口，不归因消费方，不能交v1.13.0当完整手动/自动合同。

v1.13.1仅把原triggerSync接到独立组代际通知，同时删除worker单独重复通知。所有原新写/决策/完成通知均复用同一入口，选批前捕获代际防止丢唤醒，无新goroutine/API/预算/格式。本地确认和实际退出屏障不变。相同决策断言GREEN，另补新Save在同一间隙唤醒的独立回归；重新运行普通、race、恢复、示例及发布门禁。v1.13.0保持原tag/提交并公开说明此边界，最终接入版本使用v1.13.1或更高。前述1.13.0与补丁失败/通过证据分别保留，不覆盖成PASS。

### v1.13.1最终补丁回归与持续对照

- 完整nosql普通最终PASS：40.62s，278 PASS/51 SKIP；新PID6441→6442原卷恢复。完整race最终PASS：68.89s，278 PASS/51 SKIP；新PID6895→6896。两项新手动通知测试均包含在内。
- 普通初轮277 PASS/51 SKIP/1 FAIL原件保留：pending已经减少时B worker尚未执行完计数/槽归还，测试立即断言TargetActive=1实际为2。没有改变生产提交逻辑或放宽为任意活跃数；断言改为等待B真正归还槽后仍须精确1，继续保留A实际在途和B真实确认断言。
- 示例04/07 race再次PASS：100.62s，68 PASS/7 SKIP。04真实HTTP/进程恢复race再次PASS：53.03s，4 PASS。发布候选门禁再次PASS：61.54s。旧普通持久层、示例、配置/性能/quick门禁原结果保留；补丁对应的完整nosql普通/race和正式发布门禁重新执行，不用旧记录冒充。
- 补丁独立二进制三轮旧0/新4成对持续对照全部PASS，38.43s；每方案1920确认/0错误/最终pending0。参数、可靠刷盘、载荷与前文相同，测试函数不改，二进制SHA在私有清单。

| 轮 / 独立组数 | Save平均/P50/P95/P99/最大(ms) | 健康确认P50/P95/P99(ms) | 确认/总墙钟秒 | 输入末pending / 排空秒 |
| --- | --- | --- | ---: | --- |
| 1 / 0 | 2.484 / 2.262 / 3.401 / 7.256 / 34.062 | 52.24 / 134.03 / 154.56 | 314.26 | 52 / 0.155 |
| 1 / 4 | 2.803 / 2.549 / 4.913 / 9.913 / 18.944 | 7.26 / 13.06 / 32.31 | 319.87 | 16 / 0.050 |
| 2 / 0 | 3.276 / 2.812 / 4.809 / 14.034 / 41.555 | 231.37 / 584.06 / 603.90 | 294.58 | 195 / 0.565 |
| 2 / 4 | 2.263 / 2.383 / 2.818 / 3.204 / 4.040 | 6.89 / 10.98 / 17.16 | 319.90 | 16 / 0.050 |
| 3 / 0 | 2.158 / 2.214 / 2.717 / 3.688 / 6.886 | 41.59 / 89.66 / 118.92 | 316.04 | 36 / 0.122 |
| 3 / 4 | 2.115 / 2.201 / 2.807 / 3.310 / 9.138 | 6.66 / 10.69 / 15.68 | 319.83 | 17 / 0.050 |

补丁这一时段连零值旧路径也比先前时段更快，说明主机/持久化时段波动不能忽略；不能把跨时段差异都归因几行通知修复，结论以各自同轮旧/新对照为准。通知补丁的直接证据是受控的决策/Save丢唤醒RED→GREEN，性能不替代正确性断言。此前Save差轮、确认事务增加、CPU/I/O未采集及所有NOT_RUN边界继续保留。

### 到期重试等待的第二项受控RED

交付前再覆盖AutoSync=false、A仍阻塞且B已由应用明确NotCommitted授权Retry的边界：统一通知只发生在决策提交时，尚未到NextAttemptAt，之后无Save/target退出，手动调用不会再次选取到期B。`manual-retry-due-red` exit1，原件保留。最小补丁只在原手动等待内使用一个timer，沿policy.MinRetryInterval有界复查；仍须原状态/NextAttemptAt/预算允许，不能对未知结果盲重试，没有新增后台线程。

`manual-retry-due-green`是测试helper指针编译错误，不算语义RED；修正后`manual-retry-due-green-fixed`三项真实Badger测试全部PASS。两项通知回归将原重试间隔配置为2秒、断言窗口仍1秒，证明由通知推进而非备用timer偶然通过；到期回归使用原50ms间隔，A不退出、无新Save，B真实完成第二次target并本地确认。所有FAIL不覆盖成PASS。

最终到期补丁首次完整race `final131-nosql-race` exit1，无DATA RACE但自动排空测试在pending已减至0、worker尚未记录SyncedItems瞬间读到2而期望3。此FAIL不算race整体通过。与之前pending/释放槽的边界相同，测试改为等待pending=0且SyncedItems=3的两个真实条件，最终仍精确要求3、原1秒时限不变；生产确认路径未为此修改。

### 既有硬杀恢复限制：仍为FAIL，不因本专项改写

最终普通全包`final131-nosql-normal-metrics-boundary`发生既有`TestCrashBeforeAndAfterHandoffConfirmation/before`重开错误：`while opening memtables ... while opening fid: 1 err: Create a new file`。单独使用固定官方v1.12.1源码加已披露计时观察的baseline.test与最终候选各20轮真实硬杀对照，双方均19 PASS/1 FAIL，两个命令都exit1（`legacy-handoff-crash-baseline`、`legacy-handoff-crash-candidate`）。旧测试文件及SharedBadgerManager与v1.12.1逐字相同；这些结果证明该错误无需独立组能力也能出现，不证明所有恢复故障均已排除，也不量化发生率。

本专项没有改Badger依赖、recovery选项或原硬杀断言，未将失败用例SKIP、重试包裹或放宽成功条件。上述FAIL原日志及二进制哈希保留；该旧恢复边界仍未修复，不能以之后单轮PASS宣称硬杀长期可靠性全绿。新独立组专用硬杀测试验证A未知由应用事实确认、B1/B2已确认不重放及无新Save新PID恢复，属于不同的具体边界。

### 最终到期补丁的完整回归与固定二进制持续对照

- `final131-nosql-normal-isolated`：exit0，33.37s，顶层{'PASS': 279, 'FAIL': 0, 'SKIP': 51}；实际命令`go test ./pkg/persistence/database/nosql -v -count=1 -timeout=300s`。writebehind_independent_groups_process_test.go:114: SIGKILL旧PID=29720，新PID=29721；A原Unknown确认，B1/B2不重放，原卷无新Save恢复
- `final131-nosql-race-metrics-boundary`：exit0，70.35s，顶层{'PASS': 279, 'FAIL': 0, 'SKIP': 51}；实际命令`go test -race ./pkg/persistence/database/nosql -v -count=1 -timeout=300s`。writebehind_independent_groups_process_test.go:114: SIGKILL旧PID=27934，新PID=27936；A原Unknown确认，B1/B2不重放，原卷无新Save恢复
- `final131-examples-race`：exit0，20.31s，顶层{'PASS': 94, 'FAIL': 0, 'SKIP': 14}；实际命令`go test -race ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -v -count=1 -timeout=300s`。
- `final131-release-contract`：exit0，60.92s，顶层{'PASS': 0, 'FAIL': 0, 'SKIP': 0}；实际命令`bash scripts/test.sh release-contract`。

最终候选固定二进制`candidate131-final-metrics.test`，构建与命令原件为`build-final131-performance-metrics` / `final131-binary-performance`（exit0，39.67s）。已结束其它本轮CPU回归再运行；使用前述同一可靠性、16组、50ms输入节拍、每组120笔、约2KiB、GOMAXPROCS=1、1组40ms/其它1ms目标、4并发预算及真实Badger。全部6组各1920真实确认、0错误、最终pending=0。

| 轮次/模式 | Save平均/P50/P95/P99/最大(ms) | 健康组确认P99(ms) | 确认/墙钟秒 | 输入结束pending | 排空(s) |
|---|---|---:|---:|---:|---:|
| 1/0 | 3.727/3.671/6.515/7.947/20.114 | 831.987 | 281.21 | 244 | 0.875 |
| 1/4 | 3.941/4.242/5.718/7.338/8.209 | 26.675 | 319.19 | 17 | 0.056 |
| 2/0 | 3.955/3.608/7.001/10.872/17.765 | 1059.281 | 272.24 | 296 | 1.096 |
| 2/4 | 3.303/3.371/5.092/6.508/6.958 | 24.056 | 319.29 | 17 | 0.055 |
| 3/0 | 4.517/3.725/7.660/21.378/40.580 | 1066.639 | 271.75 | 292 | 1.111 |
| 3/4 | 4.376/4.535/6.514/8.638/10.352 | 30.153 | 319.15 | 17 | 0.060 |

该轮同机对照健康组真实确认P99从832–1067ms降至24–30ms，确认吞吐约272–281/s至319/s；确认事务仍从481增至1920。Save本次同轮也改善，但前述较差轮和主机波动仍保留，不推断全部现场/业务SQL、单CPU硬配额或稳定15ms承诺。完整分段累计值、CPU用户/系统、GC/内存及未知的物理I/O/限流见私有`performance-final131-summary.json`；不能把嵌套/累计时间相加当请求壁钟。

代码及测试由当前主Agent直接审查，无子Agent；复核了槽预留、真实退出、取消迟到结果、通知代际、到期NextAttemptAt、固定worker及同key跨组前序。最终发布仍以干净精确提交执行的`release-check --release`和远端tag/Proxy/SumDB回执为准，不能从本文推断消费方已接入。
