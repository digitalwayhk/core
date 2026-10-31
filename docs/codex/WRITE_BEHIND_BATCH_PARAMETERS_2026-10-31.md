# 本地可靠写合批与官方参数对照（2026-10-31）

## 范围与结论填写规则

基线v1.6.2 / `3d8eade96c24e1738174b842ad059cfad5e3d546`，独立分支`fix/write-batch-ready-collection`。只操作Core，保留190项原WIP；不操作Bitzoom、Full-E4、远端、业务库或共享缓存。不发布版本。

本轮先尝试调度微调，未看到可靠收益，已撤回自己的生产代码改动。候选A删除无条件Gosched并在队列暂空时立即提交，造成合批碎片；候选B仅删除Gosched、保留原聚合窗口，首轮也未改善。不能凭旧trace看到Gosched就推出删它必然更快：该让出也给尚未入队的前台goroutine合批机会。原生产算法继续作为参数实验基线。

## 官方资料与实际用法

固定依赖Badger v4.9.6。已联网查阅官方Options、WriteBatch与慢写排障，并只读核对本机相同版本源码；未改模块缓存。官方排障说明同步逐key事务会降低吞吐，纯盲写可用WriteBatch；需要读取旧状态的读改写应保留Transaction。Core有界处置需要版本/指纹/attempt/组索引原子核验，不能直接换成不支持读取的WriteBatch，也不能提前返回Save成功。

- [固定版官方Options](https://github.com/dgraph-io/badger/blob/v4.9.6/options.go)：SyncWrites默认false，MemTableSize64MiB，NumCompactors4，ValueLogFileSize约1GiB；不是可靠pending的生产承诺。
- [固定版官方WriteBatch](https://github.com/dgraph-io/badger/blob/v4.9.6/batch.go)：通过有界并发事务与回调摊销成本，Flush才等待已提交结果。
- [固定版官方慢写排障](https://github.com/dgraph-io/badger/blob/v4.9.6/docs/troubleshooting.md)：逐key同步提交的陷阱、读改写边界。

Core Production为SyncWrites=true、DetectConflicts=true、memtable16MiB、vlog1MiB、compactor2、L0 4/8、ValueThreshold64KiB，仍继承5个memtable预算、256MiB block cache与Snappy。NumGoroutines作用于Badger Stream，不等于Save并发。

该基线持续卷关闭后仅一个20字节vlog、分配4096字节；2KiB模型及本轮元数据内联LSM。不能因为配置1MiB就断言vlog轮转导致此轮长尾，更不能把所有Badger写当成vlog写。

## RED/GREEN及失败候选

证据根`.temp/writebehind-ready-collect-20261031/`。原始失败、源码快照和命令保留，实验不自动算生产验收。

A的就绪积压立即提交测试为真实RED→GREEN，race定向通过；A性能否决后，该新增设计测试随候选撤回，不删除或放宽原仓库测试。B单P就绪完整批次测试初版用观察者先运行判断yield，在race下把异步抢占误判，原candidate-b-targeted-race失败保留且不称race通过。修正为只识别实际停在Gosched的BatchCommitter运行栈：精确旧生产overlay连续20次RED，B定向race 30轮通过。B未显示性能收益，测试与生产变更均保存于候选快照，不纳入最终生产代码。

保留的正式新增回归仅验证就绪前台请求不能越过FIFO内部本地命令；配置1小时窗口不靠放宽超时通过。其余正式改动为显式参数实验夹具与指南，不改变公共API、持久格式、生产默认值或业务模型。

## 性能口径

本机macOS arm64/APFS，Go1.26.6、GOMAXPROCS=1、GOMEMLIMIT=1GiB软预算，非1CPU/1GiB容器硬限；2028字节Gob、50并发、真实Badger。SyncWrites/DetectConflicts始终true。持续轮40000次Save、名义50笔/25ms、2000个可合并状态key；不是不可合并流水容量认证。短轮2000个唯一key。

同一测量源码、精确旧生产overlay二进制；参数实验一次只改一项，第二轮反转次序。计量串行，不与本任务构建/其他测试并行；每轮封存result/命令/数据文件摘要后回收合成卷。输出实际DB.Opts安全字段，未采集OS同步调用数、纯fsync/限流等仍为null。事务与I/O调用不混用，嵌套累计时间不相加冒充壁钟。旧版pilot与新增参数观测夹具分组报告。

## 两轮实际参数结果

两轮均40000次Save、错误0、有后台最终pending=0；范围为独立运行结果，不是合并分位数/置信区间。参数试验全部沿用精确v1.6.2生产算法，不叠加已否决候选。

| 单项改变 | Save平均ms | P99ms | 最大ms | 本地事务 | 排空秒 |
| --- | ---: | ---: | ---: | ---: | ---: |
| 原配置 | 10.097–10.443 | 67.757–69.247 | 113.100–123.141 | 7792–7794 | 0.270–0.535 |
| memtable16→64MiB | 16.311–18.227 | 95.866–122.125 | 181.932–208.675 | 4709–5057 | 1.938–1.984 |
| vlog1→64MiB | 10.133–10.538 | 67.247–71.475 | 111.139–128.082 | 7716–7777 | 0.289–0.689 |
| CollectWindow1ms→250µs | 9.947–10.307 | 63.906–64.890 | 106.689–114.065 | 7744–7895 | 0.266–0.418 |
| compactor2→4 | 10.500–10.613 | 75.039–77.174 | 101.058–156.538 | 7755–7800 | 0.164–0.225 |

memtable放大时GC从原首轮44次升至657次，第二轮651次；进程峰值RSS约1.61–1.68GB，原首轮约1.26GB。实际Go堆接近/超过1GiB软预算，支持内存预算压力的判断；不能将全部慢请求都归于STW，GC总暂停仅约17–24ms，且未逐请求重新trace。事务变少而P99变差，证明不能只用事务数评估延迟。

250µs在这两轮仅有约4–5ms的P99改善，仍约64ms，未消除独立长尾。vlog和compactor未显示稳定收益，不能据官方默认直接放大全局参数。没有关闭SyncWrites、冲突检测、压缩/GC或清pending改善数字。

## 无后台控制组与建议

以下均40000次Save，单轮控制只用于边界观察，不能替代多轮置信判断；错误均0。无后台组保持2000个合成pending，不称同步收敛。测试卷在结果封存后全部回收。

| 模式 | 原1ms平均/P99 ms | 250µs平均/P99 ms | 原/250µs本地事务 |
| --- | ---: | ---: | ---: |
| 普通可靠写，无后台 | 4.625 / 14.949 | 3.728 / 14.218 | 822 / 855 |
| 有界处置，无后台 | 11.899 / 46.295 | 11.217 / 48.460 | 830 / 829 |

参数有影响，但250µs在无后台有界控制组P99反而增加约2ms，不能宣称全面最优。普通可靠写本轮P99约15ms，有界无后台约46ms、空target后台约68–69ms，支持Core处置元数据及后台路径存在额外开销的判断；不是Badger单次事务的统一速度、不是I/O纯耗时，也不是字段级CPU归因。

**本轮不调整生产默认、不采纳两个调度候选。** 可在有相同条件和回归的独立项目中试250µs，而不是全局强推。下一步应按内存/CPU预算核查Core有界元数据读取与编码、Badger实际缓存/堆占用、每轮选批和确认开销；仍须真实采样后决定，不从本轮数据推定某个字段就是全部长尾根因。不能要求消费方长期关闭有界处置或可靠刷盘。

22组共728000次Save、错误0；18个有后台组最终pending=0，4个无后台组合成pending=2000。包含被否决候选与pilot，不能把这些混成同一实验分布。逐轮结果含CPU user/system、RSS、GC、队列/提交累计、target与pending趋势。结果来自本机短/20秒窗口，不保证所有机器Save低于10ms。

## 候选原始数值

| 实验 | 旧/候选Save平均ms | 旧/候选P99ms | 旧/候选事务 | 旧/候选前台批次 |
| --- | ---: | ---: | ---: | ---: |
| A短窗 | 4.789 / 4.483 | 19.155 / 25.619 | 369 / 443 | 47 / 122 |
| A持续 | 11.593 / 11.218 | 87.616 / 70.815 | 6955 / 7085 | 1110 / 1702 |
| B短窗 | 4.345 / 4.615 | 19.684 / 20.195 | 370 / 399 | 48 / 78 |
| B持续 | 10.504 / 10.568 | 68.958 / 72.060 | 7765 / 8271 | 1096 / 1599 |

A、B均未达到采用条件；候选A的单项持续P99改善不能掩盖合批碎片化，B的无条件yield删除也未带来收益。最终生产文件reliable_write_batcher.go与基线逐字相同。

## 最终回归与可重复命令

主Agent独立执行并自审，遵守仓库禁止子Agent。最终生产算法逐字保留v1.6.2；新增公开的仅测试环境参数，不增加运行时业务API，不改变配置零值或存量卷格式。测试覆写固定白名单，不打印payload/凭据或DSN。

- nosql及04/07普通：438 PASS、50 SKIP、0 FAIL，exit0。
- 相同包race：438 PASS、50 SKIP、0 FAIL，exit0，无DATA RACE。
- 真实进程硬崩溃恢复：普通/race都实际执行TestConfirmationHardCrashRecovery的确认前/确认后/结果未知，以及TestCrashBeforeAndAfterHandoffConfirmation的交接前后；恢复进程打开原目录，无新请求。不是同进程对象重建。
- persistence-unit、config-contract、performance-contract、release-contract、check-logging全部exit0。release-contract为candidate模式，未创建tag、push或发布；兼容门禁通过不代表本轮发布新版本。

实际普通/race命令（工具链由run-check.py固定为Go1.26.6，清除外部依赖开关，GOPROXY=off/GOSUMDB=off/GOFLAGS=-mod=readonly）：

```bash
rtk proxy go test ./pkg/persistence/database/nosql ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -v -count=1 -timeout=300s
rtk proxy go test -race ./pkg/persistence/database/nosql ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -v -count=1 -timeout=300s
rtk proxy ./scripts/test.sh persistence-unit
rtk proxy ./scripts/test.sh config-contract
rtk proxy ./scripts/test.sh performance-contract
rtk proxy ./scripts/test.sh release-contract
rtk proxy ./scripts/check-logging.sh
```

精确旧生产与新参数夹具通过baseline-overlay.json绑定；原生产文件不直接回退工作区。构建与单轮计量实际形态为：

```bash
rtk proxy go test -overlay .temp/writebehind-ready-collect-20261031/baseline-overlay.json -c ./pkg/persistence/database/nosql -o .temp/writebehind-ready-collect-20261031/baseline-b.test
rtk proxy env CORE_CONFIRM_PERF=1 CORE_CONFIRM_PERF_VARIANT=mem64 CORE_CONFIRM_PERF_OUTPUT=独立合成目录 GOMAXPROCS=1 GOMEMLIMIT=1GiB .temp/writebehind-ready-collect-20261031/baseline-b.test -test.run '^TestConfirmationPerformanceComparison$/^3$/^bounded_fast_sync$' -test.v -test.timeout=90s
```

逐轮name-command.json保存真实完整参数、路径、环境覆盖、exit_code与seconds；上面中文独立目录为用法占位，不替代这些原始命令。参数实际Options、完整逐轮结果及CPU/RSS/GC/批次均在performance-results.json。二进制SHA和源码SHA见candidate-b-parameter-binding.json；失败候选A另见performance-binding.json。实验二进制已按授权删除，重新执行需先重建。

## 证据、清理及未验证项

所有22轮测试卷逐轮封存文件SHA后删除，关闭后文件累计已分配1217150976字节；这是分轮累计，不是同时占用或df净增加。最终又删除5个专用二进制、已分配260239360字节。cleanup-final.json确认合成数据文件剩余0；小结果、日志、源码快照和失败记录保留。保护的190项WIP逐文件SHA保持，main/tag均不移动，未清共享Go缓存或其他项目数据。

证据压缩包：`.temp/writebehind-ready-collect-20261031/evidence.tar.gz`，323045字节，SHA256 `968e3818637ef66e029fb6eb7cdf1ad022bb9d09dc1fb1b9f073c3d51a390a30`。包内evidence-manifest.json逐项列path/bytes/SHA256，包含真实RED、未采纳源码、普通/race/门禁、参数与清理记录。

NOT_RUN/NOT_PROVEN：Full-E4及消费方同档硬资源复测、Bitzoom接入/部署/UAT/容量、外部MySQL/Redis/MongoDB、物理断电、全仓普通/全仓race、OS逐次同步调用/纯fsync/限流、全部长尾消失。未证明哪个元数据字段贡献了多少CPU，也不保证所有Save低于10ms。未替消费方改配置、依赖或发压。
