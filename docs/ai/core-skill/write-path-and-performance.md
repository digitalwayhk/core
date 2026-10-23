# 写路径、缓存与水平扩展

## 写路径分段观测

需要 write-behind 有界重试、未知结果确认或失败满窗口隔离时，显式配置 `BadgerDBConfig.WriteBehindFailure` 并使用 `UseWriteBehindWithFailureHandler`；正式 API 见[有界失败处置](../../codex/WRITE_BEHIND_FAILURE_DISPOSITION_GUIDE.md)。旧 target/零配置继续旧模式，不得宣称全框架已经有界。应用提供稳定 StoreID、顺序组、有限 target/handler 预算并决定分类，Core 不按 SQL 错误或 UserID 猜业务。

未知结果先确认；重试须应用明确确认原事务未提交，超时/一次 not found 不构成证明。接管须应用先幂等持久保存完整原件或独立引用，Core 后绑定原版本回执；`HandedOff` 不是 `IsSynced` 或 APPLIED，不自动解除组屏障。恢复/Release 核对原 ID、版本、指纹，不重置预算；旧决定不能 ACK 新版本。失败历史不设 TTL、不自动删除，禁止 DeadKeys、清目录或吞错制造收敛。

同组只执行本 store 最早未解决版本，无关组有界轮转；不承诺跨副本/主题业务全局顺序。固定回调 worker 在原执行互斥之外，超时不等于实际退出，同组重试仍等待旧回调退出，不另开替代 goroutine。前缀冻结 policy，禁止旧二进制回退打开或删 mode 标记；消费方独立验收未知事务、业务顺序、接管及存量卷迁移。

本地准入、排队和提交必须分开：`WriteAdmissionMetrics.Attempts/TotalDuration`是准入调用累计值；`BatchCommitMetrics.TotalQueueWaitDuration`按本地操作累计进入提交队列前至开始提交的等待，包含排队发送等待；`TotalCommitDuration`按批累计实际提交耗时。这些累计和SQL池`WaitDuration`都不是壁钟耗时，比较时按同一窗口取增量并检查进程/连接池重置。

07通过现有Runtime提供者暴露`write_path`低基数指标，包含上述阶段和已有MySQL池的WaitCount/WaitDuration、InUse/Idle、关闭原因计数。Outbox组件增加取批、发布、确认的`*_seconds_total`与`*_attempts_total`；分key发布累计时间允许大于壁钟。不得使用订单ID、requestID或EventID作指标标签，也不得为了取指标触发建连、DDL或强制GC。

07负载实际发送由独立stdlib进程执行；凭证只经权限受限文件传入，run/stage命名空间避免预热和主档幂等键碰撞。排空起点取发送进程全部在途结束的时间，不能延后到结果序列化或父进程读完文件。窗口采样用有界索引EXISTS和独立SQL/Broker/metrics预算，缺失为null并保留错误；计时前后仍全量逐键核验。profile仅显式`core_benchmark_profile`构建及本机测试端口启用，正式容量不带此标签。统计入口GC的微基准不是业务容量收益，不据此自动改变全局GC策略。

## RouterInfo 缓存与高性能写

API 只通过 `info.UseCache(ttl)` 声明启用结果缓存。未配置 `RouteCache` 时默认使用 local L1；Badger L2 和 shared Redis L3 才需显式配置。

- Public 缓存键覆盖所有筛选维度；Private 键中的 UserID 只取自 Token 解析后的认证上下文。
- L1/L2/L3 命中统一返回 `json.RawMessage`。L1 `MaxBytes=0` 按进程/容器有效内存 2% 解析为 16–256 MiB 共享预算；`MaxEntries=0` 自动解析；超过 `MaxValueBytes` 的响应正常返回但不进入任何缓存层。
- 商品、供应商、支付类型、订单状态变更后，通过 ServiceContext 专属 EventBridge 执行主动失效；TTL 只是兜底。
- 同键冷加载使用 RouteCache/`syncx.SingleFlight`，不在 API 自建锁和队列。
- shared 缓存内部通知由组合根使用原生广播；每 1 秒读取路由 generation 并推进本地记录版本，覆盖 key 级漏通知，单轮预算 3 秒、5 秒无成功对账即旁路。版本写在记录内，不扩展物理 key、不每秒全扫 Badger；L1/L2 原 TTL/容量仍负责回收。通知不健康时禁止旧 L1/L2 命中，显式 `RouteCache.Redis.OnUnavailable=bypass` 仍可完全旁路。详见 [内部通知标准](../../codex/CORE_INTERNAL_NOTIFICATION_LIFECYCLE_GUIDE.md)，不得把可重建缓存的失效方式复制到业务 pending。

`PrefixedBadgerDB` / `ReliableWriteStore` 的 write-behind 与 RouterInfo L2 是两种不同能力：L2 可重建；write-behind pending 在远端权威库确认前是业务事实。高 TPS 路径必须等**本地可靠写成功**后才向调用方确认，再异步同步远程；远端 ACK 后才删除 pending。

标准业务热路径（示例 04/07，**不是** Manage/`ModelList`）：

1. public/private → business → 实例级 `OrderWriteRuntime`（或等价注入），不使用包级全局 store registry。
2. `Start` 中创建 store，调用 `UseWriteBehind(WriteBehindTarget)` 绑定**远程权威库**汇合目标；`ServiceContext.UseResource` 管理关闭。
3. 远程权威库类型由 models 的 DataAction/`WriteBehindTarget` 决定：开发可用 SQLite；多进程/Docker 应用共享 MySQL 等网络库。04 可用 `ModelListWriteBehindTarget` 作示例目标适配；07 订单权威库应用真正共享 remote。
4. `EnableWriteBehind(ModelList)` / `SetSyncDB` 仍存在但已标记 Deprecated，仅为 ModelList/IDataAction 兼容层；示例 04/07 的 `StartOrderWriteStore`/`StopOrderWriteStore` 已在 v0.0.250 删除，代码中不存在可调用版本，替代品是 `OrderWriteRuntime` + `ServiceContext.UseResource`。

事务内按唯一键点查或只需要有界结果、不需要分页总数时，显式设置
`SearchItem.SkipCount=true`。此模式只执行查询本身，`Total` 保持零；零值仍执行
`COUNT(*)` 后查询，供 Manage 分页和确实需要总数的业务读取使用。不得在调用方仍依赖
完整 `Total` 时开启，也不得用它绕过结果上限。

基准必须与对照示例同机、同口径、多轮运行，同时报告 QPS/TPS、p50/p95/p99、错误率、pending 收敛和磁盘上限。
数据库热路径的连接健康由真实 SQL 错误驱动，不得每操作先 `Ping`；详细的只读单次恢复、写入不重放和事务不换连接契约见 [models.md](models.md#mysql)，容量失败案例见 `docs/codex/cases/MYSQL_PER_OPERATION_PING_POOL_AMPLIFICATION.md`。

详细运行时契约见 `docs/codex/ROUTERINFO_RUNTIME_GUIDE.md`，容量契约见 `docs/codex/PERFORMANCE_SLO_BASELINE.md`。

## 订单水平扩展（示例 07）

以 `examples/07-shop-order-scale` 为标准模板，在 06 的多服务边界之上演示可扩容 order 副本：

- `AutoMachineID=true`：legacy 使用原有 ClusterProvider 编号契约；显式 stable 使用独立持久编号账本，发现 lease 不分配或释放编号。不得为可扩容副本硬编码固定 MachineID，详见 [稳定身份指南](../../codex/STABLE_INSTANCE_IDENTITY_GUIDE.md)。
- 每副本本地 pending / Outbox / Inbox / 投影目录隔离；最终订单权威库是**共享**远程库（Docker/多进程下为 MySQL 等），不是每进程 SQLite remote。
- `UseOutboxWithOptions`可显式传入`OutboxRuntimeOptions{KeyConcurrency: 4, BatchSize: 512}`；`BatchSize<=0`保持底层默认100，扫描间隔不变，满批且有进展时继续drain。批量上限与并发数分开测量，不能只用100ms/100条推断固定吞吐，07的512试验未通过长窗，当前示例仍默认100，不能将参数入口可用当容量认证。
- “有序可靠”不等于全局串行。高吞吐 Outbox 只有在 Store 能按 ordering key公平组成有界 batch、同 key按持久顺序恢复、消费者以 Inbox/业务 sequence收敛重复时，才可显式开启 key concurrency。单纯把 `LIMIT 1` 改大仍会被 hot key占满，不构成容量修复。失败案例与认证证据见 `docs/codex/cases/KEYED_RELIABLE_GLOBAL_SERIALIZATION_FAILURE.md`。
- 下单热路径：public/private → business → `OrderWriteRuntime` → 本地可靠写 → `UseWriteBehind` 同步远程权威库；Manage 继续用 `ModelList` 做后台视图/配置（服务级 `GetList` 绑定同一权威库 DataAction 亦可），但不得替代业务写路径。
- `OrderRule`等规则由Manage更新共享权威事实。当前07内部订单服务和原生对照每次校验直接读取权威规则，不保留正向快照，不依赖配置事件或手工失效；旧`OrderRuleCache/Invalidate`调用面仅为示例兼容。数据库失败必须返回错误，不能用旧规则继续放行。未找到启用规则时仍按既有语义回退内建默认值，本次未重新定义禁用规则的业务含义。只有确需增加副本缓存时才另行接入并验证版本/失效机制，不复制历史未接线的缓存模板；见[修复验证](../../codex/ORDER_RULE_AND_OUTBOX_FIXES_2026-10-19.md)。
- MySQL/SQLite可在`SearchItem.Context`传入查询执行上下文；普通COUNT/读取、免COUNT和统计分支均传给GORM。nil保持旧行为，JSON忽略；不承诺其他适配器或初始建连支持取消。07 Outbox继续使用独立action的`Load`保留连接恢复，不为传ctx绕过框架读取或增加事务/建表。
- 多实例诊断字段记录 `TraceID`、`ServiceName`、`ServiceInstanceID`；`ServiceInstanceIP` 仅诊断。
- 幂等边界必须写进 README：只扩展 order 时 user 入口幂等策略的限制；远程幂等探测在 MySQL 不可达时 fail-closed 或明确文档化降级风险。
- 部署侧提供 Prometheus scrape 配置（如 `deploy/prometheus*.yml`），标签至少稳定暴露 `service` 与 `service_instance_id`，供 Runtime Aggregator 查询。
- 集成测试：`examples/integration/07-shop-order-scale` 与 `07-shop-order-scale-multi-process`；多副本 UAT 应采样 discovery 确认 `MachineID`/`ServiceInstanceID` 唯一。

pending 缓存计数与持久同步索引必须分开核对；幂等 ACK 成功数不一定等于本次移除的索引数，重复确认不能再次扣减。出现负值或计数不一致时，保留原卷并核对 `GetPendingSyncCount()`，不得清零指标或删除记录制造排空。显式长压证据目录还须保留身份与本地 pending；不能让测试临时目录清理销毁失败后的恢复事实。

真实容量测试必须区分成功接单、远程事实、Outbox 发布与消费 ACK。07 压测基线中的 100ms/100 条业务调度曾有约 1000 条/秒上限；当前服务已接回框架自适应 worker，使用 512 条上限、32 条触发阈值及最长 20ms 收集，并保留 target 的 stable 准入。框架公共 `ForceSyncBatch` 仍保留索引重建兼容语义，正常自动同步不逐批重建。消费组 pending=0 不代表未读取 lag=0。显式压测入口为 `TestStableOrderWriteLoad`，历史基线见 [07 真实链路压测](../../codex/ORDER_WRITE_LOAD_2026-10-18.md)，本次实施与回归见 [事务消息与吞吐审计](../../codex/TRANSACTIONAL_MESSAGE_PLAN_REVIEW_2026-10-18.md)；不得把候选参数或功能回归当作稳态容量认证。

## PrefixedBadgerDB / ReliableWriteStore

Badger参数选择先读[配置与调参](badger-configuration.md)：区分官方默认、工厂默认和实际Options；可靠性参数不可降级，候选性能值不等于最优。当前16MiB memtable/1MiB vlog不代表全部内存预算，block/index缓存等仍可能继承依赖默认。

- 自适应 write-behind 只在组合根显式配置：复用 `SyncBatchSize`，设置 `SyncFlushThreshold=32`、`SyncMaxCollectDelay=20*time.Millisecond`、`SyncBacklogDrainDelay=0` 可按数量/期限收集并连续排空。新三字段全零保持旧模式；不能只改 `SyncBatchDelay` 就宣称解决积压等待。有真实本地确认进展（含部分确认并返回错误）时重置退避、按既有 drain 间隔继续，原错误仍返回并记录；零确认（包括无错误、本地确认失败或并发更新使旧快照失效）指数退避，新通知不能绕过。`Attempts`、`Failures`、`SyncedItems` 分别累计，部分成功可同时增加失败次数和确认数；`LastSuccessAt` 仅在无错误且有确认时更新。自动 worker 轮次日志提供 confirmed、has_error、next_wait_ms 和 progress/no_progress 原因，不新增采样线程或业务标识标签。
- write-behind 进展必须来自原本地确认事务，不能用 target 自报列表长度替代；重复确认不重复计数，版本校验、未确认 pending 和 ForceSync 错误契约不变。取批仍是索引前缀窗口，失败项占满 `SyncBatchSize` 时后方健康项仍会被挡住，本修复不提供公平 lane 或业务 chunk 排序。业务不自建同步循环；20 ms 不包含远端执行时间，也不保证低流量 fsync 下降。完整配置与升级边界见 [同步指南](../../codex/WRITE_BEHIND_SYNC_GUIDE.md)。

- 纯缓存默认损坏策略为 `CorruptionPolicyFail`；只有确认数据可从远端完整重建时才显式使用 `CorruptionPolicyResetCache`。
- **新业务默认**：`ReliableWriteStore` / `PrefixedBadgerDB.UseWriteBehind(WriteBehindTarget)` 绑定远端汇合目标；配置必须满足可靠写要求（含 `SyncWrites=true`、冲突检测与 fail 策略，以 `EnableWriteBehind`/`UseWriteBehind` 校验为准）。
- 示例适配：04 使用 `ModelListWriteBehindTarget`；07 使用订单专用 `WriteBehindTarget` 指向共享远程权威库。
- `DefaultSharedConfig` 默认 `SyncWrites=false`，面向共享缓存；write-behind 必须显式启用持久写。
- `SetSyncDB`、`EnableWriteBehind(ModelList)` 是仍可编译的 Deprecated 兼容路径，不得作为新热路径设计中心。
- 待同步记录禁止 TTL。`Close` 返回 `PendingSyncError` 表示本地仍是临时事实源，不能把目录当缓存删除。
- 语义为 at-least-once，远端操作必须幂等。同 key 写入会合并状态，不适用于资金流水或审计事件；不可合并事件使用唯一事件 ID 的 JetStream/outbox。

### 排空观测与结果文件处理

容量测试以独立发送器的 `inflight_end_at` 为排空起点；后台每秒观测继续运行。排空终点取该起点之后连续两次完整收敛观测的实际完成时间，不取父进程解码结果文件后的当前时间。结果文件写盘、传回及解码耗时单列，不能算成服务排空，也不能通过后移起点隐藏积压。负数、缺失、SQL/Broker/metrics错误仍打断连续收敛；最终全量核对保留，5秒门槛保持。结果使用 `drain_timing_source=completed_background_samples` 标识该测量口径。

观测错误按字段计数与连续失败轮数分别记录；`pending`上限守护和观测缺失分开判定，零观测错误仍可能因真实积压而失败。SQL探测超时不直接等于索引缺失：先核对真实索引和执行计划；排空副本的快速查询也不能替代负载时的MVCC、I/O和调度现场。07最终六轮测量中Core/原生各0/3通过，结果与边界见[最终容量验收](../../codex/ORDER_QUEUE_FINAL_CAPACITY_2026-10-19.md)，不能将依赖升级或短窗通过当作2000/s稳态保证。

应用facade的请求幂等映射、事件去重集合与可靠pending分别核算。07及原生对照的这些进程内集合当前没有回收路径，不能用pending排空或有限窗口内GC后堆下降宣称长期有界。是否采用持久权威、保留多久及如何淘汰须与业务使用者确认，不能在Core猜测TTL而改变重复请求或重投语义。

### 长尾故障取证

业务下单容量轮先明确订单规则是否已配置：缺失规则会沿现行语义每次查询后回退，不能用负缓存隐藏该路径。07可显式设置`CORE_TEST_LOAD_SEED_RULE=1`，在计时外经既有Manage写入`NewOrderRule()`的相同默认值，Core与原生对照共用该事实，并留存fixture。Manage仅用于准备数据，不纳入业务下单吞吐目标。配置完整与规则缺失的结果应分别标注。

07负载入口可显式设置`CORE_TEST_LOAD_DIAGNOSTICS=1`，仅向证据目录输出本轮进程的二进制、metrics端口与诊断目录，不输出凭证。需要故障前运行时trace时，另加`GOFLAGS=-tags=core_benchmark_profile`和`CORE_TEST_LOAD_FLIGHT=1`；仅Core三进程在loopback开放`/debug/pprof/flight`，原生对照尚不支持此开关。默认构建不接入该端口，未显式开启flight时端点不存在。

FlightRecorder的`MaxBytes`是运行时提示值，采集方仍须限制下载大小，并用`go tool trace`验证快照；HTTP 200、trace前缀或无错误trailer均不能单独证明快照完整。`WriteTo`会推动trace刷新，诊断活动也会影响调度，必须与无flight对照分开，不能将诊断轮当容量认证。Go 1.26 syscall/STW的[上游问题](https://github.com/golang/go/issues/81759)及[候选修复](https://github.com/golang/go/pull/81835)在2026-10-19查询时仍开放；`schedtrace`可能掩盖该问题的复现，不得用开关后的未复现宣称修复，也不得据此认定所有业务长尾同因。使用进程外时钟、Redis探针和系统采样区分Go进程停顿与依赖延迟，不关闭GC、刷盘或stable准入来换取成绩。
