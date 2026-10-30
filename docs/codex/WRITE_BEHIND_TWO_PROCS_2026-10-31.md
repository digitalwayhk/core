# 本地SSD、Go并行度与可靠写合批验证（2026-10-31）

## 范围

基线281e7e2665c023b1c7af7f8335cc3f7aff0186da，沿用独立分支fix/write-batch-ready-collection。用户授权按官方SSD/并行/事务摊销的说明修复试验。主Agent独立执行，保留190项WIP；不修改或操作Bitzoom、Full-E4、远端、业务SQL/Redis、Docker或共享Go缓存，不自动合并或发布。

实际查明：Core生产源码没有设置GOMAXPROCS=1，之前单P由显式测量命令指定。因此本轮是资源与用法组合验证，不伪造框架强制单P的源码缺陷，不在库初始化时擅改消费方的全局Go并行度。已有有限chan、单本地提交worker与同prefix事务保护继续保持；SQL/target仍在本地提交器之外。

## 物理条件与可靠性

本机16逻辑/16物理CPU，64GiB物理内存，工作区所在APFS卷SolidState=true、Internal=true。实际数据仍写该SSD上的独立合成目录，非tmpfs。物理SSD属性不是无I/O竞争或无限IOPS的证明。本轮没有Linux cgroup CPU硬配额/亲和性约束，GOMAXPROCS=2是Go并行度上限，不是2个专用核；GOMEMLIMIT=1GiB是Go软预算，非容器RSS硬限。

保持Badger v4.9.6、SyncWrites=true、DetectConflicts=true、16MiB memtable、1MiB vlog、compactor2及Snappy；50前台并发、2028字节合成Gob，MaxBatch128、CollectBacklog1、QueueCapacity1024。自动同步批128/阈值32/最长20ms、原有限失败policy保持。唯一可控变量为Go P数及本地CollectWindow；不将远端20ms与本地1ms窗口混为一谈。

## 试验与事实核验

复用TestConfirmationPerformanceComparison和原run-check.py，不建立第二套测试/业务worker框架。单一测量二进制绑定源码SHA256；每轮独立进程/目录、串行执行，不与本任务构建/其他测试并行。两轮先正向后反向比较1P原1ms、2P原1ms、2P250µs；再复测候选、比较4P并补三模式控制。

持续轮40000次Save、名义50笔/25ms、约20秒，复用2000个可合并状态key。成功Save不意味着每个中间状态单独写入远端；该负载不能代表不可合并流水容量。短轮2000个唯一key另列。

本轮加强测量窗口外核验：逐件检查2000个本地模型key、组、完整合成载荷、非删除和IsSynced标记，并只读扫描持久pending索引与缓存pending比对。自动组须verified_synced_keys=2000、persistent_pending=0；无后台控制组须verified_synced_keys=0、persistent_pending=2000。不能只凭pending=0或exit0宣称正确。输出runtime.NumCPU、实际GOMAXPROCS、显式内存软预算；缺失硬配额、CPU限流/纯fsync/OS I/O指标继续为null。

核验在Save、排空、CPU与内存计量窗口之外；不是测量期间额外全量扫描。本机试验无target SQL/网络，target立即返回ConfirmedKeys。原始失败和波动结果保留，不用短窗P99代替持续窗口。

## 实际结果与采用决定

15组共562000次Save，Save错误0、同步轮次Failures也为0；11个有后台组最终2000个状态模型全部IsSynced=true、持久pending=0；4个无后台控制组2000原件全部IsSynced=false、持久pending=2000。每轮2000模型的内容/组/同步标记均实际核验，总核验30000个最终状态模型。所有合成卷封存后删除，不称无后台组同步收敛。

下表为独立运行范围，不是合并分位数或置信区间。同一二进制、同一SSD、相同可靠性与内存软预算；core为原1ms窗口。

| 有后台持续窗口 | Save平均ms | P95ms | P99ms | 最大ms | 排空秒 |
| --- | ---: | ---: | ---: | ---: | ---: |
| 1P / core / 2轮 | 12.122–18.478 | 34.332–56.076 | 97.183–103.527 | 161.672–175.294 | 0.875–1.493 |
| 2P / core / 3轮 | 9.121–11.208 | 16.950–23.621 | 26.696–38.986 | 62.385–107.640 | 0.394–2.169 |
| 2P / collect250us / 3轮 | 6.447–18.085 | 11.651–38.136 | 21.750–54.662 | 42.544–96.483 | 0.019–0.831 |
| 4P / core / 2轮 | 9.357–21.284 | 20.451–36.273 | 26.168–54.171 | 47.090–170.456 | 0.598–0.745 |

短窗2P/core的2000笔：Save平均4.668ms、P99 8.747ms、最大10.195ms，最终pending=0、原件内容与同步标记全部匹配。但这不能代替持续窗，不能宣称15ms目标通过。

本轮采用的是**显式2P资源用法，保留原1ms窗口**。2P/core三轮P99约27–39ms，好于1P/core约97–104ms；250µs窗口约22–55ms，存在回归轮次，不改默认；4P约26–54ms，也不自动推荐更多CPU。与前轮1P数据不是同一次负载，不将不同轮数据混成提升倍数。没有缩短可靠性边界或修改本地提交FIFO，15ms持续窗目标仍未达。

本地事务/前台批次范围：1P/core为3847–6796 / 901–1134；2P/core为6909–7666 / 923–942；2P250µs为4282–7976 / 900–949；4P/core为3136–7293 / 823–859。事务少并不自动更快。2P/core前台平均每操作队列等待2.814–3.908ms，1P/core为3.575–3.839ms；每批提交均值及队列没有全面同步下降，不能把收益表述成纯I/O变快或所有排队消失。

由于本负载允许同key状态合并，不同速度下target实际接到的中间状态数不同：2P/core为32390–40000，1P/core为9262–31095，2P250µs为12202–40000，4P/core为5035–40000。最终2000状态全部确认，但不能写成每轮40000个不同业务事件都远端提交成功。完整CPU/RSS/GC、队列/批次/事务、target次数和pending趋势保留于summary.csv/json与原result.json。

### 三模式控制

下表每项仅一轮40000次Save，不作为长期或统计显著性认证；持续状态工作集和前台并发保持，改变模式会改变处置元数据、历史和确认活动，不能做简单差值相加。

| 2P控制 | 原1ms平均/P99 ms | 250µs平均/P99 ms | 最终持久pending |
| --- | ---: | ---: | ---: |
| 普通可靠写，无后台 | 3.383 / 9.919 | 2.481 / 9.422 | 2000 |
| 有界处置，无后台 | 11.953 / 40.466 | 9.050 / 20.551 | 2000 |

250µs对单轮无后台控制有改善，但有后台重复实验存在更差轮次，不据此修改整个框架或07默认值。后续如仍要追持续窗15ms，应继续定位Core有界元数据/本地事务成本，不能靠不断放大CPU或队列承诺达标。

## 具体交付与使用修正

- 07 README先构建同一二进制，再给出1P/2P配对持续命令，明确CPU硬配额与Go P数、短窗与持续窗、软内存与RSS的区别。
- 新增可选deploy/docker-compose.write-2p.yml，为三个订单服务模板提供cpus=2、GOMAXPROCS=2、GOMEMLIMIT=1GiB。不修改默认Compose；只执行本地config解析，未启动Docker。配额是上限而非专用CPU预留，实际硬配额下P99尚未测。
- 同时解析主Compose/叠加Compose，逐项断言只改变三个订单模板的CPU与Go环境，其余字段、持久卷与其余服务完全一致。只保留白名单资源摘要，完整公开演示口令输出不保留。
- 修正07旧说明把本地确认上限统一写成32的错误：正式ReliableWriteStore为固定8项，直接prefix无facade为32项。它们不是远端同步批预算。
- 强化现有性能夹具的最终本地事实/索引核验和资源观测，不建立新worker、调度器或并行业务队列。
- 更新权威skill和正式指南：复用store、并发Save/SaveBatch、有限合批、真实Commit后确认；不能把减少fsync解释成关闭SyncWrites或提前响应。

## 普通、race、进程恢复与门禁

nosql及04/07普通和race各438 PASS、50 SKIP、0 FAIL；race没有DATA RACE。两者均实际执行TestConfirmationHardCrashRecovery的确认前、确认后和结果未知，以及TestCrashBeforeAndAfterHandoffConfirmation的交接前后故障，使用SIGKILL和不同恢复进程打开原目录。组件恢复不是物理断电保证。

persistence-unit、config-contract、performance-contract、release-contract和check-logging全部exit0；发布契约为candidate模式，没有创建tag、push或发布。可选Compose经过只读解析和逐字段比对，不启动容器。没有发现框架强制单P的源码缺陷，本轮不宣称生产缺陷RED→GREEN；原生产实现作为同二进制参数对照基线，未设置脆弱的固定毫秒测试断言。

实际命令（run-check.py绑定本机Go1.26.6，GOPROXY=off/GOSUMDB=off/GOFLAGS=-mod=readonly，清除外部测试开关）：

```bash
rtk proxy go test -c ./pkg/persistence/database/nosql -o .temp/writebehind-two-procs-20261031/resource.test
rtk proxy env CORE_CONFIRM_PERF=1 CORE_CONFIRM_PERF_VARIANT=core CORE_CONFIRM_PERF_OUTPUT=本轮独立目录 GOMAXPROCS=2 GOMEMLIMIT=1GiB .temp/writebehind-two-procs-20261031/resource.test -test.run 测试选择器 -test.v -test.timeout=90s
rtk proxy go test ./pkg/persistence/database/nosql ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -v -count=1 -timeout=300s
rtk proxy go test -race ./pkg/persistence/database/nosql ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -v -count=1 -timeout=300s
rtk proxy ./scripts/test.sh persistence-unit
rtk proxy ./scripts/test.sh config-contract
rtk proxy ./scripts/test.sh performance-contract
rtk proxy ./scripts/test.sh release-contract
rtk proxy ./scripts/check-logging.sh
```

上面中文字段为用法占位；真正持续选择器为`^TestConfirmationPerformanceComparison$/^3$/^bounded_fast_sync$`，短窗/控制模式分别选择对应round/mode。各name-command.json保留原始完整参数、环境覆盖、路径、退出码及seconds，不用此处简写替代。binding.json绑定源码与二进制SHA，summary.csv/json及原result.json保留所有轮次，不覆盖差轮。

## 兼容、清理和NOT_RUN

Core公开运行时API、默认并行度、默认合批窗口、可靠性和存量卷格式不变；没有改Redis、WS、事务消息或业务状态。可选Compose仅在显式叠加时生效，未自动部署；不为新配置改写已有身份路径或清pending，退出和恢复仍按原合同。

15轮合成卷封存后逐轮删除，关闭后文件累计已分配890056704字节；最终删除2个专用二进制、已分配104194048字节。这里是分轮累计回收，不是同时占用或磁盘净增加。cleanup-final.json确认合成数据文件剩余0，小结果与原始日志保留；190项原WIP逐文件SHA保持。未清共享Go缓存、其他项目数据、容器或卷。

证据包`.temp/writebehind-two-procs-20261031/evidence.tar.gz`，249134字节，SHA256 `43958af6eed60ce7abcec1bc40d36c17fcb0fa666ab286ed9d79a74c3b87cfd5`。包内evidence-manifest.json逐文件列path/bytes/SHA256，包含测量、逐件核验、主机白名单信息、全部普通/race/门禁和清理记录。

NOT_RUN/NOT_PROVEN：可选Compose的实际Linux 2CPU硬配额运行及容器资源曲线、消费方/Full-E4同档复测与部署/UAT/容量、直写MySQL同档比较、外部Broker/SQL、不同SSD介质对照、OS逐次fsync/msync/纯I/O与CPU节流、全仓普通/全仓race、长期稳态与15ms持续窗目标。没有把短窗8.747ms或组件通过写成消费方业务达标。
