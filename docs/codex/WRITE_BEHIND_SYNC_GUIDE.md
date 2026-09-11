# Write-behind 自适应同步指南

## 能力和使用边界

`PrefixedBadgerDB` / `ReliableWriteStore` 先可靠写入本地 pending，通过 `UseWriteBehind(target)` 绑定远端权威库；框架 worker 调度批次，远端确认后才处理 pending。它不是 MQ，也不是 MySQL 原生 group commit 参数。相同 key 合并状态的既有语义不变，不用于不可合并的资金流水事件。

业务仅在实例级组合根配置 store 和 target，不重新实现 Trades 等服务级同步循环。Manage 的 models 数据源工厂不参与此调度。

## 加性配置

```go
cfg := nosql.DefaultProductionConfig(pendingPath)
cfg.AutoSync = true
cfg.SyncBatchSize = 512
cfg.SyncFlushThreshold = 32
cfg.SyncMaxCollectDelay = 20 * time.Millisecond
cfg.SyncBacklogDrainDelay = 0
// 用 cfg 创建 ReliableWriteStore/PrefixedBadgerDB，再 UseWriteBehind(target)。
// target 必须指向应用实际的远端权威库；由 ServiceContext.UseResource 管理关闭。
```

三个新字段的 JSON/YAML 名称分别为 `sync_flush_threshold`、`sync_max_collect_delay`、`sync_backlog_drain_delay`。沿用 `BadgerDBConfig` 的 duration 序列化：标准 JSON 数字为纳秒，Go 配置使用 `time.Duration`；不要在 JSON 中把 20 当作 20 ms。

| 配置 | 契约 |
| --- | --- |
| 新增三字段全零 | 保持旧模式，包括原来的 `SyncBatchDelay`，升级不会自动启用 |
| `SyncFlushThreshold` | 启用时必须为正且不超过规范化后的 `SyncBatchSize`；统计不同 pending，不是写入次数 |
| `SyncMaxCollectDelay` | 启用时必须为正；空闲后首条 pending 成功入队时开始计时，后续写入不重置 |
| `SyncBacklogDrainDelay` | 必须非负；零表示成功、有进展的积压批次之间不主动等待 |
| `SyncBatchSize` | 继续作为单轮和手动 `ForceSyncBatch` 的硬上限；不是达到阈值就只取阈值条数 |
| `SyncBatchDelay` | 自适应启用时不叠加；旧模式共享默认 100 ms，生产/快速构造器默认零 |
| `AutoSync=false` | 不启动 worker；手动同步仍可执行，且不等待收集窗口 |

只设置部分新增配置、负值或阈值超过批次上限都会返回配置错误。配置在共享 manager 首次创建时确定，不用于运行中热修改；同一个 pending 前缀应由一个实例级 store 管理，不应重复构造写者。

## 调度和可靠性

- 空闲后收集，达到数量阈值或首条期限即提交；不为每个信号重新启动完整窗口。
- 成功且仍在同一 pending 周期时按积压间隔继续。队列清空再出现新数据，重新收集。
- 目标出错或没有确认进展时，从 `SyncMinInterval` 开始指数退避，上限为 `max(SyncMinInterval, SyncMaxInterval)`；新写入和阈值不能绕过退避。部分确认同时返回错误也退避；成功有进展后复位。
- 显式启用自适应模式时，构造 store 在返回前重建兼容同步索引并初始化 pending 计数，避免后台恢复扫描覆盖并发写入。已有积压绑定目标后直接排空，无需清目录或改变存储格式；启动扫描成本仍取决于历史本地数据量。
- worker、手动同步、关闭同步继续复用执行互斥与条件确认。旧快照不能清除并发更新后的 pending；未确认数据没有 TTL。
- 收集与退避等待可由关闭信号中断。`Close` 返回 `PendingSyncError` 时本地仍有业务事实，不能删除目录；远端驱动的既有执行/取消边界不因调度变化而消失。

**20 ms 是主动收集上限，不是端到端完成 SLO。** 本地提交、Go 调度、锁等待、远端事务和失败退避另计。缩短低流量窗口必然可能增加小事务；阈值策略不能保证任意负载下 commit、redo、fsync 都下降。

## 验证和观测

使用已有 `GetSyncMetrics()` 的 Attempts、Failures、SyncedItems、耗时和 `GetCachedPendingSyncCount()`；必要时用 `GetPendingSyncCount()` 对账。没有采集的队列等待分位数不能填零。

真实测试需隔离 MySQL、框架自动建表及预热在计量窗口外；固定数据和批次上限，对照旧 100 ms、固定 10 ms、自适应配置，记录每批数量、远端行数、pending 收敛、事务和可支持的资源指标。数据库全局计数可能包含后台刷盘，不能当作每次业务 commit 的严格归因。

```bash
go test -race ./pkg/persistence/database/nosql -count=3
CORE_ADAPTIVE_MYSQL_ADDR=127.0.0.1:<专用端口> \
  go test ./pkg/persistence/database/nosql -run TestAdaptiveMySQLComparison -count=3 -v
```

比较测试使用专用、空密码 root MySQL，创建唯一的 `core_adaptive_*` 测试库并保留数据。不得指向应用数据库。不提供地址时明确跳过，不能算真实 MySQL 验收。结果见 [自适应同步验证记录](WRITE_BEHIND_ADAPTIVE_VERIFICATION.md)；Bitzoom R60 同负载验收独立执行。
