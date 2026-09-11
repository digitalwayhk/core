# 写路径、缓存与水平扩展

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

- `AutoMachineID=true`：MachineID 由 ClusterProvider lease 分配，不得为可扩容副本硬编码固定 MachineID。
- 每副本本地 pending / Outbox / Inbox / 投影目录隔离；最终订单权威库是**共享**远程库（Docker/多进程下为 MySQL 等），不是每进程 SQLite remote。
- “有序可靠”不等于全局串行。高吞吐 Outbox 只有在 Store 能按 ordering key公平组成有界 batch、同 key按持久顺序恢复、消费者以 Inbox/业务 sequence收敛重复时，才可显式开启 key concurrency。单纯把 `LIMIT 1` 改大仍会被 hot key占满，不构成容量修复。失败案例与认证证据见 `docs/codex/cases/KEYED_RELIABLE_GLOBAL_SERIALIZATION_FAILURE.md`。
- 下单热路径：public/private → business → `OrderWriteRuntime` → 本地可靠写 → `UseWriteBehind` 同步远程权威库；Manage 继续用 `ModelList` 做后台视图/配置（服务级 `GetList` 绑定同一权威库 DataAction 亦可），但不得替代业务写路径。
- `OrderRule` 等可配置规则走 Manage + 可靠事件同步到副本本地缓存；下单校验读本地规则快照，不在热路径同步打远程权威库。
- 多实例诊断字段记录 `TraceID`、`ServiceName`、`ServiceInstanceID`；`ServiceInstanceIP` 仅诊断。
- 幂等边界必须写进 README：只扩展 order 时 user 入口幂等策略的限制；远程幂等探测在 MySQL 不可达时 fail-closed 或明确文档化降级风险。
- 部署侧提供 Prometheus scrape 配置（如 `deploy/prometheus*.yml`），标签至少稳定暴露 `service` 与 `service_instance_id`，供 Runtime Aggregator 查询。
- 集成测试：`examples/integration/07-shop-order-scale` 与 `07-shop-order-scale-multi-process`；多副本 UAT 应采样 discovery 确认 `MachineID`/`ServiceInstanceID` 唯一。

## PrefixedBadgerDB / ReliableWriteStore

- 自适应 write-behind 只在组合根显式配置：复用 `SyncBatchSize`，设置 `SyncFlushThreshold=32`、`SyncMaxCollectDelay=20*time.Millisecond`、`SyncBacklogDrainDelay=0` 可按数量/期限收集并连续排空。新三字段全零保持旧模式；不能只改 `SyncBatchDelay` 就宣称解决积压等待。错误/零进展仍退避，业务不自建同步循环；20 ms 不包含远端执行时间，也不保证低流量 fsync 下降。完整配置与升级边界见 [同步指南](../../codex/WRITE_BEHIND_SYNC_GUIDE.md)。

- 纯缓存默认损坏策略为 `CorruptionPolicyFail`；只有确认数据可从远端完整重建时才显式使用 `CorruptionPolicyResetCache`。
- **新业务默认**：`ReliableWriteStore` / `PrefixedBadgerDB.UseWriteBehind(WriteBehindTarget)` 绑定远端汇合目标；配置必须满足可靠写要求（含 `SyncWrites=true`、冲突检测与 fail 策略，以 `EnableWriteBehind`/`UseWriteBehind` 校验为准）。
- 示例适配：04 使用 `ModelListWriteBehindTarget`；07 使用订单专用 `WriteBehindTarget` 指向共享远程权威库。
- `DefaultSharedConfig` 默认 `SyncWrites=false`，面向共享缓存；write-behind 必须显式启用持久写。
- `SetSyncDB`、`EnableWriteBehind(ModelList)` 是仍可编译的 Deprecated 兼容路径，不得作为新热路径设计中心。
- 待同步记录禁止 TTL。`Close` 返回 `PendingSyncError` 表示本地仍是临时事实源，不能把目录当缓存删除。
- 语义为 at-least-once，远端操作必须幂等。同 key 写入会合并状态，不适用于资金流水或审计事件；不可合并事件使用唯一事件 ID 的 JetStream/outbox。
