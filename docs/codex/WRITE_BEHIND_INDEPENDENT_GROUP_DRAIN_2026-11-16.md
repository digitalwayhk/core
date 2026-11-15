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
