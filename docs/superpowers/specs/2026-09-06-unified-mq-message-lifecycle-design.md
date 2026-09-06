# Core 统一 MQ 消息生命周期与安全回收设计

## 背景

设计开始时 Core 内置 Redis Streams 和 NATS JetStream 两个 MQ Provider。Redis 发布使用无界 `XADD`，可靠消费在 Handler 成功后 `XACK`，但没有安全保留与物理回收；NATS 已等待 JetStream publish ACK，但尚未接入统一可靠订阅、重试、死信和回收契约。本文后续章节定义并记录本次补齐后的目标契约。

现场内存压力说明无界保留必须受控，但在没有逐 Stream 占用证据时，不把全部 Redis 内存归因于已 ACK 历史。本设计的目标是建立可验证的统一语义，而不是用删除未完成消息换取资源稳定。

## 目标和非目标

本次必须：

- 统一区分发布请求、Broker 持久化确认、投递、Handler 成功、ACK、重投、DLQ、保留和物理回收。
- 由应用声明 Subject 的必需逻辑消费组、历史起点、保留、重试、死信、容量和回收预算。
- 由 Core 冻结并校验策略、调度有界回收、执行背压并输出可观测事实。
- 由 Provider 通过 Broker 原生管理面读取状态和执行 ACK、NAK/reclaim、DLQ 与回收。
- 无法满足声明的 Provider 必须 fail closed。

本次不：

- 不把单个消费组的 ACK 视为全部业务完成。
- 不对未完成消息施加无条件 `MAXLEN`、TTL、`MaxAge`、`MaxBytes` 或 ACK 后立即删除。
- 不把 Kafka、RabbitMQ 或 RocketMQ 配置占位写成内置 Provider。自定义 Provider 必须显式声明并通过 capability/conformance。
- 不修改 Bitzoom 工作区，不猜测 Bitzoom 的具体订阅服务。

## 统一生命周期语义

一条消息的阶段是：

```text
Publish requested
  -> Broker persisted
  -> Delivered
  -> Handler succeeded
  -> ACK persisted
  -> Retained
  -> Physically reclaimed
```

- `Publish` 返回 `nil` 表示达到该 Provider 声明的确认级别，不表示任何消费者已处理。Redis 当前只声明 `broker-accepted`；NATS JetStream 同步 publish ACK 声明 `broker-persisted`。应用要求更强级别时 capability 校验 fail closed。
- Handler 返回 `nil` 后 Provider 才能 ACK。Handler 返回 error、panic、超时或进程中断都不能产生成功 ACK。
- ACK 是一个消费组的状态。多组扇出中，只有所有必需组都建立连续 ACK 前沿，且消息不在任何必需组的 pending 中，该消息才算“消费完成”。
- 达到最大投递次数后，Provider 必须先确认 DLQ 持久化，再终止原消息重投。DLQ 使用原消息的稳定身份与消费组构成幂等键；契约是 at-least-once，可能重复但不得丢失。
- 消费完成不等于立即物理删除。还必须满足最小保留时间和策略代际一致性。
- 物理回收是独立、异步、有界的管理面操作，不放在发布或 ACK 热路径中。

## 应用声明

公开类型位于 `pkg/server/mq`，应用通过 `ServiceContext.RequireMessageLifecycle` 在组合根声明。同一 Subject 的所有参与进程从应用 `contract` 包的同一份 manifest 加载策略，避免多份配置漂移。

```go
mq.LifecyclePolicy{
	Subject: "trade.filled",
	RequiredGroups: []mq.ConsumerGroupRequirement{
		{Name: contract.RegisteredTradeFilledConsumerGroup, Start: mq.StartFromAllRetained},
	},
	Retention: mq.RetentionPolicy{MinAge: 24 * time.Hour},
	Retry: mq.RetryPolicy{
		MaxDeliveries:    10,
		Backoff:          []time.Duration{time.Second, 5 * time.Second, 30 * time.Second},
		DeadLetterSubject: "trade.filled.dlq",
	},
	Capacity: mq.CapacityPolicy{
		SoftMessages: 500_000,
		HardMessages: 1_000_000,
	},
	Reclaim: mq.ReclaimBudget{
		Interval:   30 * time.Second,
		BatchSize:  1_000,
		TimeBudget: 500 * time.Millisecond,
	},
}
```

`RequiredGroups` 必须来自应用实际的可靠 `SubscribeEvent` 注册点。不可靠订阅不能写入必需组。`Broadcast` 只有在 manifest 枚举稳定实例消费组时才能启用物理回收。

每个消费组必须选择起点：

- `StartFromAllRetained`：从当前仍保留的最早消息开始。
- `StartFromNew`：将创建时的末尾记为入组前沿，只消费后续消息。

策略按 Subject 规范化并计算 fingerprint。进程启动后策略冻结；同一 Subject 不同 fingerprint、必需组丢失、ACK 前沿倒退或 Broker 上存在不安全限额时，Core 停止回收并输出异常状态。

## 消费组与并发边界

- Core 在允许发布和回收前预建所有必需组，包括当前离线的组。
- 消费者上下线不改变组是否必需。一个组中没有在线消费者时，它的前沿停止，并自然阻断后续回收。
- 组注册、入组前沿、最高已确认前沿和策略 fingerprint 保存在 Broker 侧 Core 元数据中，不依赖单个应用进程内存。
- Provider 回收锁按 Subject 设置租约和 fencing。只有当前 owner 能推进元数据与物理回收。锁丢失、状态读取失败或 context 取消时立即停止本轮。
- 组删除、组重建、游标回退、历史重放和缩减必需组集合不是运行时自动操作。它们需要停止回收、提升策略 generation、校验现有保留边界，然后才能恢复。
- 组意外丢失时，Core 只能从已持久化的安全前沿重建；元数据缺失时不猜测，只停止回收。

## Core 与 Provider 责任

`MQManager` 持有 lifecycle controller。controller 负责策略冻结、capability 校验、周期调度、容量门禁和观测汇总。回收工作器使用 `Interval` 触发，每轮同时受 `BatchSize` 和 `TimeBudget` 限制，不持有发布或消费热路径全局锁。

Provider 可选实现：

```go
type LifecycleMQProvider interface {
	MQProvider
	LifecycleCapabilities() LifecycleCapabilities
	EnsureLifecycle(ctx context.Context, policy LifecyclePolicy) error
	InspectLifecycle(ctx context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error)
	ReclaimLifecycle(ctx context.Context, policy LifecyclePolicy, snapshot LifecycleSnapshot) (ReclaimResult, error)
}
```

`LifecycleSnapshot` 的未知值用可空字段和状态表达，不使用零伪装。`MQManager.RequireMessageLifecycle` 在 Provider 没有实现接口、capability 不匹配或 `EnsureLifecycle` 失败时拒绝启动。

## Redis Streams 机制

Redis 使用现有 Stream 和 consumer group 原生语义：

- `XGROUP CREATE ... 0` 实现 `StartFromAllRetained`；`$` 实现 `StartFromNew`，并把实际末尾 ID 写入 Core 元数据。
- 每个组根据 `XINFO GROUPS`、`XPENDING` 和已持久化前沿建立连续完成边界。有 pending 时使用最早 pending ID 之前的边界；无 pending 时使用连续 ACK 前沿。
- 全局安全前沿是全部必需组前沿的最小值。再与按 Redis Stream ID 时间计算的 `MinAge` 边界取更保守值。
- 物理回收在 Lua 中用有界 `XRANGE` 选择安全前沿前的消息，再执行 `XDEL`；不使用无条件 `MAXLEN` 或可能跨越安全边界的近似修剪。前沿统一表示第一条不可删除消息，避免 inclusive/exclusive 误删或最后一条历史永久滞留。
- 重试计数和 DLQ 状态保存在 Subject+Group 低基数元数据中。到达上限时使用 Lua 原子执行 DLQ `XADD`、原消息 `XACK` 和重试元数据收敛。
- 不删除离线 consumer group，不自动执行 `XGROUP DESTROY`、`DELCONSUMER` 或游标重置。
- 回收脚本必须校验 owner token、policy fingerprint 和 generation。

## NATS JetStream 机制

NATS 使用 JetStream 原生 stream、durable consumer 和管理 API：

- stream 使用 file storage 和无会删除未完成消息的 `LimitsPolicy`。Core 不用 `MaxAge`、`MaxMsgs` 或 `MaxBytes` 代替安全回收。
- 启动时预建所有必需 durable consumer。`DeliverAllPolicy` 和明确起始 sequence 实现两种入组模式，且不设置会自动删除离线 durable 的 inactive threshold。
- 可靠订阅接入 `ReliableMQProvider`：成功 `Ack`，失败按 `BackOff` 执行 `NakWithDelay`，处理中必要时 `InProgress`。`MaxAckPending` 用于有界背压。
- 消费组安全前沿使用 consumer info 的连续 `AckFloor.Stream` 与入组 sequence，不使用单条最后投递序号。
- 达到重试上限后，先使用稳定 `Nats-Msg-Id` 发布 DLQ 并等待 publish ACK，再 `Term`。重复 DLQ 是允许的，丢失不允许。
- 全部必需 durable 的最小安全 sequence 与 `MinAge` 边界同时满足后，使用 JetStream `Purge` sequence 或 `DeleteMsg` 执行回收。Purge 上界取“安全 sequence”和“当前首 sequence + `BatchSize`”的较小值，使单轮回收同时受数量和时间预算限制。不经过 Core 安全前沿计算直接使用时间或容量限制是禁止的。
- 发现已有 stream/consumer 的 retention、subject、durable 或游标与声明冲突时，不覆盖用户资源，而是 fail closed。

## 能力矩阵

| 能力 | Redis Streams | NATS JetStream | 无内置实现的 Provider |
| --- | --- | --- | --- |
| 发布确认 | `XADD` 成功仅为 broker-accepted | JetStream publish ACK 为 broker-persisted | 必须自行声明并验证级别 |
| 多必需消费组 | consumer groups | durable consumers | 默认不支持 |
| Handler 成功后 ACK | 已支持 | 本次接入统一契约 | 默认不支持 |
| 失败重投 | pending reclaim | NAK/BackOff | 默认不支持 |
| 最大投递次数 | Core 元数据 | consumer delivery metadata | 默认不支持 |
| DLQ | Redis Lua 原子转移 | publish ACK 后 Term | 默认不支持 |
| 离线组保护 | 保留 group | 保留 durable | 默认不支持 |
| 历史重放 | 保留范围内 | 保留范围内 | 按 capability |
| 多组安全回收 | 全组排他前沿 + Lua 有界 `XRANGE`/`XDEL` | 全 durable ACK floor + JetStream 管理 API | 默认不支持 |
| 有界回收 | 支持 | 支持 | 按 capability |
| ordered-reliable | 现有契约继续支持 | 未声明则继续 fail closed | 按独立 capability |

Kafka、RabbitMQ、RocketMQ 在当前 Core 中只是配置名称和自定义 factory 入口，不出现在内置 Provider 验收结论中。

## 新增 Provider 的统一扩展标准

实现后保留 `docs/codex/MQ_MESSAGE_LIFECYCLE_GUIDE.md` 作为长期设计与扩展标准，不把本次契约只留在历史 spec 或代码注释中。任何新增 MQ Provider 都必须按以下顺序接入：

1. 先列出 Broker 原生的发布确认、消费组、ACK/重投、DLQ、保留、物理回收和观测能力，不从 Redis 或 NATS 的物理命令反推统一接口。
2. 实现 `LifecycleMQProvider` 并逐项声明 `LifecycleCapabilities`。不支持的要求返回标准 unsupported 错误，不接受后忽略。
3. 证明它能识别全部必需组的连续完成前沿，且不会回收未投递、pending、重试中或离线必需组尚未完成的消息。
4. 使用 Broker 原生机制实现确认和回收。只有原生信息不足以表达统一契约时，才增加最小的 Core 元数据。
5. 通过共享 lifecycle conformance suite、Provider 定向故障测试、`-race` 和真实 Broker 验收，才能把 capability 标记为支持。
6. 在能力矩阵、配置矩阵、公开 API 兼容表面、skill 权威分片和 changelog 中同步登记。

扩展标准要求 capability 是可机器校验的结构，conformance 是行为证据；文档自称“支持”不能替代两者。

## 容量、背压与故障

Core 分开统计：

- 已被所有必需组完成、仍在 `MinAge` 内的历史保留。
- 至少一个必需组尚未完成的未消费积压。
- pending/重试中的处理失败积压。
- DLQ 中需要人工或业务补偿的消息。

`SoftMessages`/`SoftBytes` 超过时告警但继续发布。`HardMessages`/`HardBytes` 超过时，发布返回稳定的背压错误。开启硬限后，若容量快照过期或 Provider 无法可靠采集，必须 fail closed，不可继续盲发。

资源预算使用：

```text
稳态保留字节 ≈ 平均消息字节 × 生产速率 × 最小保留时间 × 存储开销系数
故障积压字节 ≈ 平均消息字节 × max(0, 生产速率-恢复消费速率) × 故障窗口
```

实际规划必须同时给出数量、平均与 P99 大小、生产速率、各组消费速率、保留时间和最大故障恢复窗口。消费者长期离线或消费能力不足时，系统通过告警、背压和发布失败保护未完成数据。

## 可观测性

统一快照提供值与 `ok|partial|stale|unavailable|not_collected` 状态。指标包括：

- retained messages/bytes 及其中已完成历史量和未完成 backlog。
- 每个必需组的 pending、lag、最老未完成消息年龄和最后前沿推进时间。
- publish accepted/rejected、handler in-flight、retry/redelivery、DLQ published/failure。
- reclaim attempted/reclaimed/failure、本轮耗时、预算耗尽和上次成功时间。
- Broker 资源指标：Redis stream 可得字节数或 NATS stream state bytes；无法采集时标记 `not_collected`。

标签只能使用 Provider、低基数 Subject family、逻辑消费组和结果类别。禁止消息 ID、用户 ID、订单 ID、TraceID、原始 subject 中的动态值以及 payload。日志不输出 payload，也不把原始消息身份做成指标标签。

## 兼容与迁移

- 旧配置不含 lifecycle policy 时保持当前行为：无自动物理回收、无新增硬限、不静默删除历史消息。
- 新策略是加性公开 API。只有应用显式声明并通过启动校验后才开始回收。
- 首次启用先进入 observe-only：建立必需组、读取前沿、输出预计可回收量，但不删除。显式切换到 `enforce` 后才执行物理回收。
- 已有 Redis group/NATS durable 保留，Core 不更名、不删除、不重置游标。发现不一致时停止回收并要求运维处理。
- 启用前必须先建立指标和容量基线，核对每个 Subject 的必需组，并保留可覆盖最大故障窗口的 Broker 资源。

## 测试与验收

使用 RED→GREEN，生产代码之前先运行并保留预期失败证据。

Provider-neutral conformance 覆盖：

- 单组、多组、`StartFromAllRetained`、`StartFromNew`。
- 慢消费者、离线组、漏 ACK、Handler error/panic/超时。
- 重复投递、最大投递次数、DLQ 先确认后终止。
- 进程重启、Broker 重启、组意外丢失、前沿倒退、策略冲突。
- 并发发布/ACK/回收，证明新消息和 pending 不丢失。
- 最小保留到期、有界回收、预算耗尽后下轮继续。
- 软限告警、硬限背压、快照过期时 fail closed。
- 缺失指标返回 `not_collected`，不返回假零，不产生高基数标签或 payload 泄漏。

Redis 和 NATS 都必须运行真实 Broker 集成测试，包括正常持续消费时保留量收敛到策略范围，故障时未完成消息不丢，恢复后继续消费和回收。定向包必须运行 `-race`。未实际运行的 Broker 验收在交付中逐项标记 `NOT RUN`，不用 mock 通过替代。

## Bitzoom 接入示例原则

Core 文档只展示声明方法，不写死成交事件的具体订阅服务。Bitzoom 接入时必须：

1. 从当前代码中搜索该成交 Subject 的全部 `SubscribeEvent` 注册点。
2. 只收录 `Reliable:true` 且业务必须完成的逻辑服务组，并将名称放入 Bitzoom 共享 `contract` manifest。
3. 由发布方和消费方加载同一 manifest；启动时由 Core 对比实际注册组和策略组。
4. 根据成交事件的实际产生速率、平均/P99 大小、允许重放窗口和最大故障恢复时间设定 `MinAge` 与容量阈值，不复制示例数字。
5. 先使用 observe-only 对比预计可回收前沿、pending 和积压，再由 Bitzoom 单独发布流程切换 enforce。

## 交付边界

实现完成后交付：中文契约文档、能力矩阵、Core 实现、RED→GREEN 证据、定向 race、真实 Redis/NATS 验证、旧配置迁移说明和 Bitzoom 声明示例。同时必须：

- 新增并保留 `docs/codex/MQ_MESSAGE_LIFECYCLE_GUIDE.md`，用于后续扩展其他 MQ Provider。
- 更新 `docs/ai/core-skill/multiservice-and-observability.md` 与 `docs/ai/core-skill/SKILL.md` 的必要索引/不可违反契约。
- 更新 `docs/codex/API_COMPATIBILITY_SURFACE.md`、`docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`、NATS 接入指南和 `CHANGELOG.md`。
- 保持 `.codex/skills/`、`.claude/skills/`、`.cursor/skills/`、`.github/copilot/skills/` 只是指针，不复制 skill 正文。

任何未运行或受环境阻断的测试都单独列出，不将设计、mock、编译或容器健康冒充真实 Broker 验收。
