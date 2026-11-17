# 同租约独立SQL组执行与共享fence验收（2026-11-18）

## 范围、基线与根因

本轮只操作Core隔离工作区`/Users/vincent/orca/workspaces/core/internal-menu-snapshot`、`feat/owned-sql-independent-groups`。基线`4de280f26dbf417ee283455b523fd298a3d7824a`（v1.14.1），计划提交`e001e7d7a343a60ae8accbefa8dcff836461aed4`。消费方交接8729 bytes、SHA256 `d5d2ab7df4c423456faefa3b17e82b960f9103ccad5142f96701448adf0c5ba4`。默认工作区和锁专项WIP的HEAD/分支/完整untracked状态/二进制diff指纹均保全；初次核对因没有-uall导致目录折叠误报，v2用一致命令确认未变。没有操作消费方代码、依赖、配置或Full-E4。

旧调用链为`StartRuntime → runKey → stage(Step) → 应用ConcurrencyTasks.Run → workers.Wait → Step返回 → LastConfirmed/下一次Step`。A1未退出，即使B1真实完成也无法进入下一轮B2。原Step合同有意同步，不能直接删等待或取消保护。另一个串行点是旧`CheckFence → FenceTransaction.LockFence`要求排他guard：独立组SQL也要等同一guard事务退出。它不是Core持Badger锁等SQL，也不是MQ跨读取窗口问题。

新增显式`Options.GroupWork → runGroupWork → 实例GroupWorkBudget固定worker → StepGroup → 实际退出/当前凭据复核 → 更新进展/归还本组`。B1真实完成即可推进B2，A2仍不得越过A1。没有应用公开Submit、第二份消息/任务原件、第二套Broker/服务或业务状态机。

新公开API：GroupWorkBudgetOptions、NewGroupWorkBudget/Close/Done、GroupWorkRequest、GroupWorkOptions、Options.GroupWork、ServiceContext.NewOwnedWorkBudget；另新增SharedFenceTransaction/CheckFenceShared。完整接入、预算、SQL示例、默认兼容和回退见[SQL任务指南](OWNERSHIP_SQL_TASKS_GUIDE.md)。本轮按MINOR候选v1.15.0；正式发布与否以远端main/tag和官方Proxy/SumDB回执为准，不从本文件编造发布SHA。

## 生产文件与职责

- `ownership/options.go`、`runtime.go`：Step/GroupWork二选一、copy冻结、原生命周期接线；原Step与原时间默认保持。
- `group_work_budget_options.go`、`group_work_options.go`、`group_work_budget.go`、`group_work_identity.go`：Workers1..64、Prefetch Workers..1024；全部选择/排队/执行身份共用上限，最多4096登记租约；无每条goroutine。
- `runtime_group_work.go`：有限发现与独立推进；NextGroups只发现身份，真实组首在worker接纳后查/claim。恢复revision变化才等本key实际退出的维护屏障，无普通整批Wait。
- `group_work_job.go`、`group_work_lease.go`：真实回调退出前不释放组/worker；单元独立Execution，取消/超时/迟到/失去Ready不确认；控制/观测/续租/Stall仍生效。
- `key_status.go`、`runtime_key_progress.go`：实际在途及最早执行时钟；补齐Delivered下限，不与应用重复相加、不把未知填零。
- `fence_shared.go`：显式共享锁定当前读；旧Check/Activate仍排他，后者等待全部旧共享事务真实commit/rollback。账户/任务锁、业务前序、attempt、未知提交和幂等仍归原SQL。
- `group_work_metrics.go`、`observability/provider.go`：实例一次性固定预算组件，不按scope重复相加，不用group/UserID/订单标签。
- `router/owned_work_budget.go`、`servicecontext.go`：每ServiceContext唯一预算、资源和指标；重复/关闭/身份冲突拒绝，逆序真实关闭。

没有修改Redis Provider、MQ生命周期、WS/认证、业务表格式或任何依赖。

## RED、GREEN及失败原件

原始证据根：`/private/tmp/core-owned-sql-independent-groups-20261118/`。每个name对应`.log`和`.json`，后者含真实argv、退出码、耗时、bytes与SHA。RED探针源码3372 bytes，SHA256 `40b9c09206ff5454bd5fe6f72aab1d607cae98c3bf70f96c49ae3e1ec81f7390`；旧源码下两个真实断言失败，不是缺API编译失败。保留旧Step默认，没有将失败探针改弱放回包内冒称通过。

| 原件名 | 退出码 | 墙钟 | 日志bytes | 日志SHA256 |
| --- | ---: | ---: | ---: | --- |
| legacy-real-red | 1 | 4.713s | 1010 | `850df1d04a58bbf8676806f9a0219d0c7a1da305affad9312e0c24952876802f` |
| group-first-green | 0 | 3.724s | 379 | `ad8fe02875013155e9caaa3ccf0cd9b34836dd7535cf0a8a9b769ba958e9f8b1` |
| group-boundaries-race | 0 | 45.823s | 1387 | `7bf165af6db0675b4e0494f6426b57662b6f433a08aca6899d652b6d41650015` |
| shared-fence-mysql-green | 0 | 6.824s | 1793 | `23dbd0f6f9a4b8b10d4309edcb43c7fe24e114522e0c7659fd456b65639f34f2` |
| sql-group-first-green | 0 | 4.195s | 1055 | `ec5664e293bc5fb128dbb1da6da0904f297ffa24335c0d186002768d6f4fcceb` |
| group-sql-process-first | 1 | 0.602s | 475 | `0bdb529fa80b43ae72914eaf6b354e5ee691853b5423ec185b970048febe688a` |
| group-sql-process-v2 | 0 | 20.308s | 1361 | `bccc84228ef8706dd7bdbeb0eee7a842c886fc72ab3c22670b955d97c95d144a` |
| group-lifecycle-race | 1 | 5.506s | 491 | `250e7d1fad8a29f3ca4f040bda9e7a0cacc5233587ea6c83f34a862975d50726` |
| core-four-packages-normal | 0 | 232.745s | 305 | `2258394fb8ab42a47f49ce807b5524724c84e2e754b05b56bb4090fa05583966` |
| core-five-packages-race | 0 | 253.091s | 305 | `664f56012068338debed7a86759d69cd9d4393e9bcc42a119c7d6b71ab174f96` |
| new-contracts-final-normal | 0 | 59.518s | 6977 | `724c5559c0775c26c7296c8329ada559346d2030f0297f7abf2c44dbdb947b0b` |
| new-contracts-final-race | 0 | 62.900s | 6975 | `193e2d20d9ac2a933a4ceea42f91f54394ab394f062da7cd08fe0d19b4880466` |
| example11-normal | 0 | 297.627s | 2088 | `e50a314e0042e4823d8612113df2a391af3cc88bd8f5ebfabaf2b431a0bd640f` |
| example11-race | 0 | 298.155s | 2088 | `634406b038c9d02860de5c1bda1e1eef7efaeea0ed19d427e85dcdda2216dd01` |
| sql-sustained-performance | 0 | 36.812s | 20191 | `370d21cce6432bcf6b1c13f88b0526b28ff8373e7a23eac3b6d4a4889b36683c` |
| sql-sustained-eight-seconds | 1 | 159.584s | 18935 | `b435b4815bbbc6a007ed0a08945579b88b09bb83ecc45aea18adfd56b7b74ed1` |
| sql-native-eight-seconds | 0 | 186.453s | 22712 | `c4b5f56424f8ffd56aa47dca84cf0effa20386a7581e85882f655d7bdacdb00a` |
| api-compat | 0 | 11.641s | 113 | `ef805e3f94194cb52f7e7501ce5d4428777cb2bb3407b8fc7c21f3a988802ed5` |
| public-api | 0 | 13.045s | 1052 | `c0524fa86645d37aca95fc75b73955cfc6fc7923d3109e56109dbf0ebd736895` |
| config-contract | 0 | 8.713s | 353 | `d9c3e7034cd7180b27e81abada5329ffb49d6566aa34d324b45c8e826c6d9613` |
| security | 0 | 28.918s | 568 | `46df4f0f7e4fb0aa7fc024c0ed024609383a029a27e0e9c0103a87455934b050` |
| ai-skill | 0 | 0.793s | 93 | `e3e89b08ce2d4169dcf749f860fea30d7937a72de8e183b551d43c7a5a5182eb` |
| web-dist-sync | 0 | 7.871s | 176 | `cb998eea575d2a081076ce4898622a10c25c2b925a6cde1b414c63a956e9fb5b` |
| logging-standard | 0 | 0.223s | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| quick | 0 | 6.303s | 170 | `9402c7bcedd2c74eada165c51790f163e2ed5e24652ef957ac13355b8446a7b7` |
| go-vet | 0 | 1.088s | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| release-contract | 0 | 59.329s | 2201 | `ba08ff3e54cc0ddc101ca3bf98fa6702a725953861b77057196ecaa4bd3ff2bb` |

- `legacy-real-red` exit1是预期真实双RED：真Redis正式Runtime和真MySQL排他锁；与编译失败分开。
- 首次进程测试两个多余import导致构建失败，修正后v2实际执行独立PID；首次Restore测试误把初始空引用恢复当后续revision，修正为只核真实新引用，原断言失败仍保留。编辑期一次gofmt括号错误在工具记录中保留，不算产品RED。
- 普通与race最终新增合同覆盖B2在A1未退出前真实SQL提交、A2未提前组首查询、多Runtime总预算、排队取消、全部槽实际退出、30s默认单元超时而健康流持续、暂停/回滚/未知事实、迟到凭据与关闭、真实Collector。
- 真实MySQL两个S事务并存；新X在两个旧事务都提交前不能完成，RR旧快照不能通过当前读；旧排他Check仍串行。受控未知是实际SQL已提交后返回合成丢回执，不冒充真实网络截断COMMIT ACK。
- 新模式独立PID恢复：最终race oldA50353/newA50382、oldB50354/newB50381，原生15s lease、原schema；硬杀与正常退出均有实际Wait退出证明。新Activate在受控旧共享SQL事务提交前不能Ready。原paused/unknown/pending、attempt和writes保留，应用确认原事实后原后继无新请求也能推进。父进程受控远端SQL事务用于证明租约/进程死亡不等于远端业务停止，不伪称旧PID自身已断开的SQL连接仍存活。
- 示例11普通/race都覆盖固定、动态、固定→动态保全迁移、两个处理进程/两个Gateway/三个认证WS、用户隔离/退订、mTLS和免重订恢复。原生硬杀到Ready+relay约17.2–17.5s，包含替代进程构建/启动，不是纯Broker或SQL耗时。
- 性能用例在普通/race默认SKIP，但已经单独显式实际执行；子进程角色入口不能单独当PASS。NATS/其他SQL引擎/HA/生产环境见NOT_RUN。

实际命令前缀统一`rtk proxy`，普通例如：

```bash
env CORE_TEST_REDIS_ADDR=127.0.0.1:62957 \
  CORE_TEST_MYSQL_OWNERSHIP_CONFIG_FILE=<本轮0600私有配置> \
  CORE_SQL_TASK_PROCESS_ROOT=<本轮独占进程证据目录> \
  go test -p 1 ./pkg/server/ownership ./pkg/server/mq ./pkg/server/event ./pkg/server/router ./pkg/server/observability -count=1 -timeout=600s
# 同命令加-race；最终新增合同用-run 'Test(GroupWork|SharedFence|ServiceContextOwnedWorkBudget)'再普通/race复验。
# 示例11：go test [-race] ./examples/integration/11-scoped-event-ownership -v -count=1 -timeout=600s
```

发布门禁实跑api-compat、public-api、config-contract、security、ai-skill、web-dist-sync、logging、quick、go vet和release-contract；正式clean release-check另在提交后执行并取证，不以候选脚本代替实际tag/main或官方包。

## 固定资源多轮SQL对照

本轮独占MySQL8.0.43容器2CPU/1GiB，原可靠默认提交；测试Go GOMAXPROCS=2、SQL池上限8、worker3、总身份6、原生15/3/250ms/30/45/15时钟。原同一SQL模型/8个不透明组，每40ms写入各组1行，持续8s、每模式每轮1600行；相同4ms/24ms受控事务工作。旧模式使用保留Step及旧ConcurrencyTasks批次6、并发3；不是偷偷升级SQL schema或给新模式更多连接。三模式在同一候选源码内运行旧兼容路径/新路径，不能冒称这是分别部署旧tag与新tag的Full-E4对照。

延迟从应用SQL Create开始到实际业务事务返回，含排队/发现；结束前另等Core真实确认。不是ReliableWriteStore.Save或HTTP延迟。吞吐是总完成数/包含排空的总墙钟，不是生产期间峰值。

| 轮 | 模式 | 平均ms | P50ms | P95ms | P99ms | 最大ms | 完成/s | 峰值pending | 排空s |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | legacy_exclusive | 8153.59 | 6704.34 | 18091.61 | 18445.40 | 18589.28 | 65.64 | 1043 | 16.412 |
| 1 | legacy_shared | 494.83 | 33.94 | 3042.13 | 3545.29 | 3658.47 | 166.38 | 189 | 1.651 |
| 1 | group_shared | 68.22 | 35.45 | 294.31 | 521.72 | 612.68 | 199.53 | 60 | 0.055 |
| 2 | legacy_exclusive | 20844.10 | 20956.71 | 44191.43 | 44512.77 | 44526.22 | 30.55 | 1204 | 44.399 |
| 2 | legacy_shared | 587.06 | 38.37 | 3740.80 | 4180.27 | 4311.43 | 155.45 | 230 | 2.325 |
| 2 | group_shared | 107.80 | 41.06 | 421.04 | 611.45 | 692.35 | 191.37 | 60 | 0.395 |
| 3 | legacy_exclusive | 20617.51 | 20893.52 | 43556.82 | 43775.15 | 43793.25 | 31.01 | 1192 | 43.627 |
| 3 | legacy_shared | 350.87 | 32.90 | 2172.52 | 2700.92 | 2855.37 | 172.46 | 150 | 1.314 |
| 3 | group_shared | 99.32 | 40.75 | 366.11 | 514.44 | 657.68 | 192.48 | 62 | 0.350 |

九组均1600个真实SQL写入、运行/关闭错误0；新模式三轮Core completed也均1600，最后active/reserved/queued归零。`legacy`的ownership_work完成计数0仅因为没有启用新预算，真实业务写入是1600，不能误当未处理。

新模式确认吞吐约191–200/s，P99约514–611ms；仍有默认Poll250ms的新输入发现边界、SQL查询/提交、有限队列与资源等待，不能宣称消除全部长尾。上述残余没有逐笔完整分段，不能把611ms全部归Poll。固定累计selection/handler/queue秒在原JSON中，嵌套/并行累计不相加当壁钟。应用事务/发现查询本身仍需符合原索引和预算；Core不拥有其业务SQL。

初始2s、fastTiming、旧批次3的探索轮新模式P99约48–94ms，只是不同窗口/时钟下短样本，不能替代正式原生8s结果。首次8s fastTiming、旧批次6中，旧排他模式第1/3轮各46/6个错误使整次命令FAIL；当时没有分类，原因未完全定位，不能把它改记为已修复。之后补运行/关闭分类、原生时钟与Core确认边界再对照，三轮均0；这不是将首次FAIL改成PASS，也没有修改生产原默认时钟。

未采集的CPU限流、进程CPU分段、I/O/fsync、SQL各内部锁/扫描分段和内存/GC峰值为null/NOT_RUN；go-zero自身周期usage日志不是完整CPU/GC观测。纯SQL测试没有本地Save，因此Save分位NOT_APPLICABLE，不能填0或拿消费方39.9/117ms冒充本轮测量。

## 兼容、启用、发布与回退

加性MINOR候选v1.15.0，远端核验最高正式v1.14.1，v1.15.0不存在。发布需真实clean release-check、main FF与annotated tag非force原子push、官方Proxy/SumDB全新缓存验证；发布回执和模块h1放本轮证据并向Root回传，不创建GitHub Release页面。

旧Step/零值、原X Check/Activate、Manifest/Fingerprint、SQL guard/任务行、StoreID、消息ID/OrderingKey/group/PEL/cursor/epoch不变。新模式必须显式GroupWork及共用预算；共享SQL guard必须显式新接口，不能偷偷降旧锁。没有新配置开关或自动消费方改造。

应用先升级正式模块与skill，停止准入并等待原回调实际退出，再按指南提供原模型FindExecutableGroups/CommitEarliestUnresolved及S当前读适配；这些是应用原模型方法占位名，不是Core已提供的金融SQL。Core不改attempt、业务前序、PAUSED、未知决策或业务锁。GroupWork不是Broker模式标记，不能凭本地启动证明其他副本全新，旧副本仍有旧串行成本；不认证混跑并发或在线热切换。

回退先停稳实际回调，再改回Step/X Check；原SQL/消息历史保留，不重置fence。新版没有新卷迁移，不需要删消息、重建scope或清历史。`go get`不安装skill，消费方须按README和link-consumer-skill.sh同步权威规范。

## 已知限制与NOT_RUN

- Bitzoom接入/升级/金融功能、Full-E4部署/压测/UAT及500/s容量：NOT_RUN，本轮没有操作，留消费方正式包复测。
- NATS新能力、其他SQL方言S/X适配、真实COMMIT ACK网络截断、MySQL/Redis HA/断网矩阵：NOT_RUN，不以受控回执未知或两个连接测试替代。
- 默认Poll发现和有限资源仍可增加延迟；全部槽被不合作回调占住时健康组背压，没有强杀或替补worker。回调/SQL必须线程安全、真实提交后返回，不能后台发SQL却先报true。
- 没有自动业务公平lane、自动重平衡、补偿、跳过前序或exactly-once承诺。应用错误的组选批或缺业务前序条件不会被Core自动修正。
- 本轮自建资源在原件封存、备份读回后精确回收；不清其他资源。最终path/bytes/SHA256和release-clean、published-refs、official模块验证、物理manifest/归档回执位于证据目录，失败原件保留。
