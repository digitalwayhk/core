# Write-behind 自适应批量提交设计

日期：2026-09-11。代码基线：`origin/main` / `b5b15a83c9319407957a2669c9b08d81f0fe2ddc`（v1.1.2）。

## 目标和范围

优化 Core 内置 write-behind worker 的主动等待：低流量有界收集，高流量按数量提前提交，持续积压不重复收集等待。保留本地可靠写、远端确认和崩溃恢复契约。只修改 Core，不修改或部署 Bitzoom/Trades；本任务不包含推送、合并或发布授权。

当前 `sharedbadger.go` 的触发分支在每轮执行前 `Sleep(SyncBatchDelay)`；有剩余时重新发送触发信号，因此每批再次等待。`DefaultSharedConfig` 的延迟为 100 ms、批次为 500；`BadgerDBConfig.SyncBatchDelay` 的“默认 10ms”注释与共享配置不符。生产默认配置未设置该延迟，不能把所有默认构造器都描述为 100 ms。

选择加性、自适应收集策略。单纯改为固定 10 ms 仍有积压重复等待，且可能缩小批次；完全取消收集会增加小流量下的微小批次。自适应策略同时提供数量和时间两个触发条件，但不承诺任意负载下都比 100 ms 策略更少事务。

## 配置契约

在 `BadgerDBConfig` 增加三个字段，沿用现有同步配置命名和序列化约定：

| 字段 | JSON/YAML 名称 | 显式启用示例 | 语义 |
| --- | --- | --- | --- |
| `SyncFlushThreshold` | `sync_flush_threshold` | `32` | 当前不同 pending 记录达到此数量时提前结束收集 |
| `SyncMaxCollectDelay` | `sync_max_collect_delay` | `20*time.Millisecond` | 从空闲后首条 pending 成功入队起，允许主动收集等待的上限 |
| `SyncBacklogDrainDelay` | `sync_backlog_drain_delay` | `0` | 成功且有进展的积压批次之间等待；零表示不主动等待 |

复用 `SyncBatchSize`，示例设为 512，也允许应用设为 1000；它仍是单轮批次和 `ForceSyncBatch` 的硬上限。阈值是触发条件，不是批次上限：实际取数最多 `SyncBatchSize`。

- 三个新增字段全零：关闭自适应策略，保留旧调度与 `SyncBatchDelay` 行为，各默认构造器不自动启用新策略。
- 启用时阈值、最大收集等待必须同时为正，积压间隔必须非负，阈值不得大于规范化后的 `SyncBatchSize`；负数或不完整组合直接拒绝，不静默填补。
- 启用时新策略替代 `SyncBatchDelay`，不叠加旧等待；保留旧字段的配置和源码兼容性。
- `AutoSync=false` 仍不启动后台 worker；手动 `ForceSyncBatch/All` 不等待收集窗口，仍受现有互斥和批次限制。
- 配置属于共享 Badger manager 的启动配置，不引入按请求变更、服务自建循环或新的包级 registry。

## 调度与并发边界

内置 worker 使用空闲、收集、排空、失败退避四种状态，同一实例不增加并行远端写者。

1. 空闲后首条 pending 成功入队时记录本轮起点；以已提交的本地 pending 为准，不能在本地持久成功之前向远端提交。
2. 收集时检查 O(1) pending 数，达到阈值或期限到达即提交；后续信号、同 key 更新和周期兜底不得推迟该期限。不同 key 才增加阈值计数，不把 API 请求数量当 pending 数量。
3. 每批最多取 `SyncBatchSize`。成功、有确认进展且仍有 pending 时进入排空，按 `SyncBacklogDrainDelay` 继续。恢复的历史 pending 不再等待新收集窗口。
4. 确认队列为空后恢复空闲；并发新写入不得被旧一轮的状态清理覆盖。陈旧或合并信号不应制造空事务，也不能造成漏唤醒。
5. 出错或零确认进展时进入退避。首轮按现有 `SyncMinInterval` 等待，连续无进展指数增长至 `SyncMaxInterval`（上界不低于下界）；成功有进展后复位。新写入信号不得绕过正在进行的失败退避。部分成功但同时返回错误也先退避，避免故障期间持续打库。
6. 定时器等待可由关闭信号中断，每批之间检查关闭。仍复用 worker、手动同步、关闭同步共用的执行互斥及原有关闭返回契约；不把本次改动扩展成远端驱动取消机制重写。

时间含义：20 ms 仅约束主动收集窗口，不是硬实时端到端 SLO。Go 调度、执行锁、远端事务、故障退避等可以使完成时间更长。空闲周期的首条时间无需新增磁盘格式：重启后按已有同步索引恢复并直接排空；无需精确持久化每条消息年龄。

## 可靠性不变量

- 本地 Badger 持久成功后才确认调用方；不改变 `SyncWrites`、冲突检测与损坏策略要求。
- 远端确认后才清除相应 pending；不改变条件确认、防止并发新版本被旧 ACK 清除、删除标记及同 key 合并语义。
- 部分确认只处理确认集合；失败、未确认和离线积压继续保留。保持 at-least-once 和远端幂等要求。
- 不修改 MQ/EventBridge，不合并不同存储目标的业务事务，不将本地 group commit 与 MySQL 原生 group commit 混为一谈。
- 不重建 pending 目录、队列索引或数据库，不新增破坏性迁移。

## 验证和证据

先增加失败测试再实现。调度状态尽量使用可控时间验证，真实 Badger worker 测试验证接线；不得仅用 sleep 后“没有报错”判定正确。

- 配置：全零兼容、合法阈值/窗口、负值、不完整组合、超过批次上限、JSON/YAML 字段接线。
- 调度：低流量到期、阈值提前、连续写入不延长期限、同 key 不重复计数、成功积压连续排空、排空后重新收集、旧信号无空事务。
- 故障：远端失败和零进展退避、持续写入不能绕过退避、部分确认错误、恢复后排空、历史 pending 重启恢复。
- 并发：写入/确认/手动同步/关闭竞争，同 key 更新保护；旧模式和新模式均做定向 race，确认无新增远端并发事务。
- 真实 MySQL：隔离测试库、相同数据和负载，对照旧 100 ms、固定 10 ms、新 32/20 ms/0 策略，固定批次上限；分别覆盖低流量、突发和持续积压。记录环境、有效样本数、吞吐、同步延迟分位数、每批条数、事务计数、pending 收敛及可采集的 redo/fsync 差值。数据库初始化和预热不计入窗口；全局指标必须排除其他负载干扰。
- 无法采集的指标明确写未采集；未运行的真实 MySQL 或 Bitzoom R60 验收写 `NOT RUN`，不能由 mock 事务次数推断 fsync 改善。
- 执行 `gofmt`、持久化定向测试及 race、`persistence-unit`、`performance-contract`、`config-contract`、`release-contract`、日志和 skill 检查。

## 文件与交付

实现集中在 `pkg/persistence/database/nosql/`：配置校验、私有调度文件、现有 worker/入队接线和测试；不重写存储事务层。

同步更新 `docs/ai/core-skill/write-path-and-performance.md`、`docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md` 及 `CHANGELOG.md`。当前只有相关历史计划而无独立 write-behind 同步指南，因此新增 `docs/codex/WRITE_BEHIND_SYNC_GUIDE.md` 并从 skill 引用。说明这是一种通用收集策略，Trades 仅在组合根配置并绑定 `WriteBehindTarget`。

交付包含实现差异、测试日志与明确的性能证据边界。本设计文档不代表功能已经实现或验证通过。
