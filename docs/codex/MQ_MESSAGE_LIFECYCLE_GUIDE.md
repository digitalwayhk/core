# MQ 消息生命周期与 Provider 扩展标准

本文是 `github.com/digitalwayhk/core/pkg/server/mq` 的长期设计、接入和验收标准。它适用于 Redis Streams、NATS JetStream 以及未来新增的 MQ Provider。应用声明业务要求，Core 校验能力、调度观测与回收，Provider 按 Broker 原生语义提供证据和机制；应用不得自行在 Handler 中编写 Stream 删除、TTL、Trim 或消费组清理逻辑。

## 消息生命周期状态机

**框架内部通知边界：** Redis 发现唤醒、shared 缓存失效和 Casdoor 身份变更有独立可读权威及补偿，遵循 [Core 内部通知生命周期标准](CORE_INTERNAL_NOTIFICATION_LIFECYCLE_GUIDE.md)。它们不要求业务项目创建可靠消费组；其瞬时广播不能替代本文的业务消息策略、ACK、pending、多组完成证明或纯保留主题。

```text
Publish requested
  -> Broker accepted
  -> Broker persisted（仅声明该能力的 Provider）
  -> Delivered to logical consumer group
  -> Handler success / failure / panic / timeout
  -> ACK or retry
  -> Dead letter（达到有界重试上限时先确认 DLQ）
  -> Retained after every required group completed
  -> Physically reclaimed after retention and safety checks
```

这些状态不可合并：

- 发布确认不等于消费成功。Redis `XADD` 成功只在 Core 契约中表示 Broker accepted；Redis 是否落盘取决于部署的 AOF/RDB 策略，Core 不把它伪装成 persisted。JetStream 同步 publish ACK 表示 Broker 已持久化接收。
- Handler 返回 `nil` 后 Provider 才能 ACK。error、panic、超时和进程退出均不能产生成功 ACK。
- ACK 只属于一个逻辑消费组。单组 ACK 不代表其他必需组已完成；同一组的多个实例是竞争消费，不是扇出组。
- 达到重试上限时，必须先确认 DLQ 写入，再停止原消息重投。DLQ 允许 at-least-once 重复，不允许丢失。
- 所有必需组完成后仍要满足 `Retention.MinAge`，才能物理回收。

## 应用声明与 Core 边界

应用在组合根调用 `ServiceContext.RequireMessageLifecycle`，并把同一 Subject 的策略放在无基础设施依赖的 `contract` 包中。`RequiredGroups` 必须从应用实际的 `Reliable` 订阅注册点生成，不能凭部署猜测。

```go
policy := mq.LifecyclePolicy{
	Subject: "trade.filled",
	Mode:    mq.LifecycleModeObserve,
	RequiredPublishAck: mq.PublishAckBrokerPersisted,
	RequiredGroups: []mq.ConsumerGroupRequirement{
		{Name: contract.RegisteredTradeFilledConsumerGroup, Start: mq.StartFromAllRetained},
	},
	Retention: mq.RetentionPolicy{MinAge: 24 * time.Hour},
	Retry: mq.RetryPolicy{
		MaxDeliveries:     10,
		Backoff:           []time.Duration{time.Second, 5 * time.Second, 30 * time.Second},
		DeadLetterSubject: "trade.filled.dlq",
		HandlerTimeout:    30 * time.Second,
		MaxAckPending:     1_000,
	},
	Capacity: mq.CapacityPolicy{
		SoftMessages: 500_000,
		HardMessages: 1_000_000,
	},
	Reclaim: mq.ReclaimBudget{
		Interval: 30 * time.Second, BatchSize: 1_000, TimeBudget: 500 * time.Millisecond,
	},
}
if err := sc.RequireMessageLifecycle(ctx, policy); err != nil {
	return err
}
```

Core 承担以下职责：

- 规范化并冻结策略，校验 Provider capability；能力不足、状态未知或同 Subject 指纹冲突时 fail closed。
- 预建离线必需组，校验可靠订阅只能使用 manifest 中的组。
- 按周期、批量和时间预算检查及回收；未知指标不填假零。
- 达到声明的硬容量后拒绝发布；不通过删除未完成消息释放容量。

应用仍负责业务幂等、事件 schema、稳定 `IdempotencyKey`/`OrderingKey`、DLQ 补偿和告警阈值。Core 不从消费者代码推断业务必需组，也不把 DLQ 自动视为处理成功。

## 消费组、重放与变更边界

### 显式无必需消费组（纯保留主题）

`RequiredGroups:nil` 或空 slice 默认仍属于漏配，`observe` 也拒绝。应用必须明确声明：

```go
policy := mq.LifecyclePolicy{
	Subject: "retained.history",
	Mode: mq.LifecycleModeObserve,
	NoRequiredGroups: true,
	Retention: mq.RetentionPolicy{MinAge: 24 * time.Hour},
	Capacity: mq.CapacityPolicy{SoftMessages: 500_000, HardMessages: 1_000_000},
	Reclaim: mq.ReclaimBudget{
		Interval: 30 * time.Second, BatchSize: 1_000, TimeBudget: 500 * time.Millisecond,
	},
}
if err := sc.RequireMessageLifecycle(ctx, policy); err != nil {
	return err
}
```

这是“只保留历史、无消费完成承诺”，不是“允许删除其他消费者的 pending”。必需组必须为空，保留期必须大于零，不能配置重试、Handler 超时或 DLQ。`MinAge` 从消息入 Broker 的时间起算，不是从声明策略或最后一次读取起算；已存在历史只在显式切换 enforce 后才可能删除。没有声明仍维持旧行为。

当前两个内置 Provider 的普通 `Subscribe` 也创建 Broker 消费组，因此无组主题通过 `MQManager` 的普通与可靠订阅均返回 `ErrLifecycleRequiredGroupMismatch`；已有组或运行中出现任何组（包括离线、pending、非必需 durable/ephemeral consumer）时返回 `ErrLifecycleStateUncertain`，不自动删除、忽略或 ACK 该组。此版不支持“有可选 Broker 消费组但允许它们被保留期淘汰”的额外语义。没有组且 Broker 状态已确认时，pending/backlog 为真实的零，retained 仍记录实际历史量；状态读取失败不能冒充零。

无组回收使用保留时间边界，而不伪造消费 ACK。Redis 原子创建空 Stream（不插入伪消息），删除 Lua 同时要求组集合仍为空。NATS 用已观测 stream 末尾作为候选上界，逐条检查消息时间、有界 purge，并在删除前复核无 consumer。NATS 没有原子 check-and-purge：必须通过 Broker ACL 禁止其他账号创建/重建 consumer，所有应用实例必须在任何订阅前加载同一 manifest；未经协调的 Broker 管理写不在支持范围，不能声称该竞态已由 Core 消除。

同一 `MQManager` 的策略声明与订阅建组互斥。跨进程、旧版本实例及直接调用 Provider 不得绕过组合根声明；拓扑变更仍须维护窗口，不能靠进程内锁代替集群协调。由无组切换到有组会改变持久化 fingerprint，原地重启直接换策略会被拒绝；必须停回收、核对未过期历史、受控迁移 metadata，或使用新 Subject。已经回收的消息无法通过新建组恢复。Core 不提供自动删元数据或自动缩组入口。

正常纯保留主题资源量约为消息速率 × 消息大小 ×（保留期 + 回收延迟）；每轮回收能力须大于长期发布速率，否则即使没有消费积压，历史仍会增长至硬容量并拒绝发布。故障恢复后继续按原策略回收，不为维持容量提前删除保留期内数据。

### 有必需消费组

每个必需组必须选择起点：

- `StartFromAllRetained`：读取 Broker 当前仍保留的全部历史。
- `StartFromNew`：把组创建时的 Broker 末尾保存为 enrollment frontier，只读取之后的消息。

离线组仍是必需组，它的完成前沿停止并阻断回收。新增组、删除组、重建 durable、重置游标、改变起点、缩减必需组集合或执行历史重放，都不是运行时热更新：

1. 把策略保持在 `observe`，停止物理回收。
2. 停止相关发布或确保恢复窗口内历史仍完整。
3. 通过受控运维变更组和游标，提升生命周期 generation，并核对 enrollment/completed frontier。
4. 在所有进程加载同一 manifest 后重启，观察 pending、lag 和 oldest age。
5. 验证真实 Broker 恢复消费，再在下一次重启切换到 `enforce`。

策略 fingerprint 包含组、起点、保留、重试、容量和回收安全字段。`observe`/`enforce` 不改变 Broker 策略指纹，但进程内策略冻结，因此模式切换仍通过重启完成。任何元数据缺失、前沿回退或必需组消失都保守保留。

## 安全回收前沿

统一算法是：

```text
group frontier = first message that must not be reclaimed for this group
completion frontier = min(group frontier for every required group)
retention frontier = first message younger than Retention.MinAge
safe frontier = min(completion frontier, retention frontier)
```

回收只能删除 `safe frontier` 之前的消息，并再次校验策略 generation、指纹和 Broker 当前状态。每轮受 `BatchSize` 与 `TimeBudget` 限制。信息不完整、管理锁丢失、context 取消、组状态变化或 Broker 返回不确定结果时停止，不猜测完成状态。

Redis 使用 `XINFO GROUPS`、`XPENDING` 和持久化的 enrollment/completed frontier。Lua 在同一 Redis 执行边界重新检查 owner、指纹、generation、全部必需组、游标和 PEL，然后用有界 `XRANGE` + `XDEL` 删除安全前沿前的消息；不使用无条件 `MAXLEN`、TTL、单组 ACK 后删除或自动删组。

Redis 正保留期使用 Broker `TIME` 与 Stream ID 的同源时间计算边界；零保留期只受全部必需组完成前沿约束，不额外叠加客户端时间边界。部署 Redis ACL 时需要允许生命周期管理读取 `TIME`；权限不足仍停止回收，不改用不可信客户端时间兜底。

NATS 使用 durable consumer 的 `AckFloor.Stream`、`NumAckPending`、`NumPending` 和 KV 中的 enrollment/completed frontier。Core 获取有租约的回收 owner 后再次检查，再以 `Purge(sequence)` 删除安全序列前的有界前缀。NATS 管理 API 不提供把“检查全部 durable + purge”合并为单条 Broker 事务的能力，因此生产账号必须用 ACL 禁止应用外部并发删除/重建 lifecycle stream 与 durable；受控组变更必须走上节维护窗口。检测到缺失或回退后 Core 停止后续回收。

## Provider 能力矩阵

### 多实例 worker、预算与容量采集

内置 Redis/NATS 的后台轮次在完整 Inspect 之前取得 subject 所有权。owner 跨周期续持有界租约，standby 不做全组或 pending 扫描；Redis standby 用 GET 排除竞争，只有 owner 原子续约，不能把检查放大转移成所有实例执行 Lua。租约有效期为 `max(1 秒, 3×Interval + 3×TimeBudget)`，复用既有 owner key / KV 锁编码。进程退出后停止续约，关闭时所有租约释放共享 1 秒预算，未成功释放的由 TTL/到期接管。旧 owner 不得释放新 owner 的锁。

`Reclaim.TimeBudget` 仍是整轮总上限，不是每个阶段各得一份预算。所有权判定和 Inspect 最多使用 70%，剩余时间留给使用独立 context 的回收；检查超时则结束本轮，不把过期 context 交给 Reclaim。所有网络操作必须遵守 context，Redis client 显式启用 context deadline。启动延迟分散在一个 Interval 内；后续延迟为 Interval 的 75%～125%，从上轮完成后计时，不追赶积压 tick，也不允许同 subject 轮次重叠。

standby 只读取数量/字节和策略证据，用独立容量快照维持发布门禁；不会更新完整 pending/lag 快照的采集时间。完整快照超过 2×Interval 后省略旧指标并标记不完整。注册时的 Ensure/初始验证是一次性冻结校验，命令放大验收须与稳定期轮次分开统计。自定义 Provider 仍保持公共接口兼容，但没有实现内置私有协调机制，不宣称其扫描具备跨实例去重。

Redis 原子删除检查不变；NATS 复用本轮 owner 内的完整快照，公开直接调用 Reclaim 仍须先获取 owner 再重新 Inspect，并在 purge 前复核策略及 lease revision。NATS 外部管理面并发变更的 ACL/维护窗口边界不变。

两个 Provider 的公开 Reclaim 直接调用也强制使用声明预算。NATS 候选消息读取最多占用该阶段剩余时间的一半，为最终校验和 purge 留出时间；扫描子预算结束时只提交已验证的前缀，并报告 `BudgetExhausted`，不读取或删除未验证的后缀。没有成功验证任何候选且发生超时、整轮 deadline 到期、校验失败或 purge 失败仍返回错误，不能用预算耗尽吞掉真实故障。

从 v1.1.1 原地升级无需修改 manifest、fingerprint、generation 或重建 Stream。新旧 worker 锁仍互斥，但旧进程继续进行锁前扫描，全部实例升级后才能验收去重收益。停止 observe 控制器后才能切换 enforce；NATS 重启读取原 enrollment 和已推进 completed frontier，仍拒绝实际前沿回退，不重置已存 metadata。

| 能力 | Redis Streams | NATS JetStream | 未声明 capability 的自定义 Provider |
| --- | --- | --- | --- |
| 发布确认 | `broker-accepted` | `broker-persisted` | fail closed |
| 多必需消费组 | 原生 consumer group | 原生 durable consumer | fail closed |
| 显式 `NoRequiredGroups` | 空 Stream + Lua 原子空组检查；按保留期有界删除 | 空 consumer 集合 + 末尾候选/时间检查/purge；要求管理面 ACL | 独立 capability，零值 fail closed |
| 重试 | Core 持久 attempt + pending reclaim | `NakWithDelay` + delivery metadata | 按 capability |
| DLQ | Lua 原子 `XADD + XACK + retry cleanup` | DLQ 同步 publish ACK 后 `Term` | 按 capability |
| 离线组保护 | 支持 | 支持 | 未证明则不回收 |
| pending/lag | `XPENDING` / `XINFO GROUPS` | consumer info | 未采集标 `not_collected` |
| 安全物理回收 | Lua fenced 有界 `XDEL` | owner lease + 有界 sequence purge | 默认不支持 |
| 硬容量 | Core 发布门禁 | Core 门禁 + Broker `DiscardNew` | 按 capability |
| 最老消息、保留量/字节 | 支持 | 支持 | 未采集标 `not_collected` |

当前内建 Provider 只有 Redis Streams 与 NATS JetStream。Kafka、RabbitMQ、RocketMQ 当前没有内建实现；注册 factory 不等于具备生命周期能力。

## 重试、DLQ 与超时

`MaxDeliveries=0` 保持旧版无限重试兼容语义，不自动创建 DLQ。大于零时 `DeadLetterSubject` 必填且不能等于源 Subject。Backoff 数组耗尽后重复使用最后一个值。

Redis 在真实投递前持久增加 Subject+Group+Message attempt。Handler 成功后原子 ACK 并清理 attempt；达到上限时 Lua 先写 DLQ，再 ACK 原消息。Lua 在任何写动作前检查 DLQ、去重和重试 key 类型；DLQ 写失败时原消息继续 pending。

JetStream 使用 Broker delivery count；达到上限时以源 stream sequence、Subject 和 group 生成稳定 `Nats-Msg-Id`，同步发布 DLQ 并等待 ACK，之后才 `Term`。发布失败时 NAK 原消息。`MaxAckPending` 使用 Broker 原生背压。

Handler panic 被转为失败。配置 `HandlerTimeout` 后，Core 在到期时把本次投递判为失败；Go 无法强制终止忽略取消的业务 goroutine，因此 Handler 必须自身有界、线程安全，并对其数据库/HTTP 调用设置 context 超时。超时后可能与迟迟不退出的旧调用短暂重叠，业务幂等仍是硬要求。

## 容量与故障窗口

容量必须分别计算“已完成但仍在保留期”的历史和“尚未完成”的 backlog：

```text
retained bytes ≈ average message bytes
               × publish rate per second
               × (normal retention seconds + worst required-group outage seconds)
               × broker replication/encoding overhead
```

还要计入 Stream 索引、consumer PEL/durable 状态、Redis lifecycle/DLQ 去重元数据、JetStream KV、重试副本和 DLQ。`SoftMessages/SoftBytes` 用于告警；`HardMessages/HardBytes` 用于明确拒绝新发布。阈值不能低于业务要求的最大故障恢复窗口。慢消费者或离线组导致 backlog 增长时，应扩容消费者、降速、背压或拒绝发布，不能删除 pending、未投递消息或离线组历史来维持内存。

Redis 内存现场还应采集逐 Stream `MEMORY USAGE`、`XLEN`、组 lag/PEL 和消息大小分布；仅凭 Redis 总内存不能断言全部来自已 ACK 历史。JetStream 同样要区分 stream retained、consumer pending 和 DLQ。

## 观测与日志

Core Runtime/Prometheus 公开以下低基数聚合：

- gauge：`retained_messages`、`retained_bytes`、`backlog_messages`、`pending_messages`、`oldest_age_sec`。
- counter：`reclaimed_total`、`reclaim_fail_total`、`redelivered_total`、`dead_letter_total`、`dead_letter_fail_total`、`publish_rejected_total`。

回收失败总数另按固定指标名提供 `reclaim_fail_deadline_total`、`reclaim_fail_owner_lost_total`、`reclaim_fail_policy_fence_total`、`reclaim_fail_provider_total`，四项之和等于 `reclaim_fail_total`。沿用现有固定指标名标签，不增加动态 subject 或原始错误标签。竞争未获 owner 是正常跳过，不计失败；真正的观测/回收/容量读取错误仍计入，不能通过吞掉 deadline 达成零增量。

指标不存在或 Provider 未采集时省略样本，并用 `not_collected`/`partial`/`unavailable` 表达，不输出假零。指标标签只允许稳定服务、组件和固定指标名；禁止消息 ID、用户 ID、订单 ID、TraceID、动态 Subject 和 payload。日志同样不得输出 payload、凭据或原始消息身份；错误在拥有重试、终止或降级决策的边界记录一次。

## 兼容与迁移

- 旧配置没有生命周期声明时，不启动 lifecycle worker、不预建必需组、不物理回收，可靠订阅保持既有无限重试行为。升级不会静默删除历史消息。
- v1.0.1 及更早的生命周期契约不支持显式无组。新增 `NoRequiredGroups` 的 false 值不进入 fingerprint JSON，已有非空必需组策略的指纹保持不变；true 值属于新的安全授权，必须所有相关实例升级后才能使用，不支持滚动期间向旧实例投递新 manifest。旧自定义 Provider 未声明同名 capability 时明确拒绝，不因已有 `SafeReclaim` 而自动放行。
- `ServerConfig.MQ.Retry` 与 `DeadLetter` 旧配置容器仍保持 rejected；生命周期是应用级 Go manifest，不把业务组和保留需求塞进通用基础设施配置。
- 首次接入先用 `LifecycleModeObserve`，核对现有 Stream、durable/group、pending、lag、容量和告警。Redis 既有 `new-only` 组没有 Core enrollment 元数据时 fail closed；NATS 既有自动 `MaxAge`、`DiscardOld` 限额、非 Limits retention 或不兼容 durable 时 fail closed。
- Redis 要求 `broker-persisted` 的应用不能选择 Redis Provider；应改用满足能力的 Provider，不能静默降低为 accepted。
- `observe` 验证完成后通过重启切换到 `enforce`。回滚到旧版本前先切回 observe 并等待生命周期 worker 停止。

## 新增 Provider

新增 Provider 时按以下顺序实施，不从某个 Broker 的删除命令反推统一语义：

1. 列出发布 accepted/persisted、消费组、ACK、重投、DLQ、保留、删除和管理面的一手能力。
2. 实现 `MQProvider` 与 `ReliableMQProvider`；Handler 成功后 ACK，失败不 ACK。
3. 只有能提供真实证据时才实现 `LifecycleMQProvider` 并声明对应 capability。无法满足明确 requirement 时返回 `ErrLifecycleUnsupported`。
4. 持久化 policy fingerprint、generation、组 enrollment 和单调 completed frontier；状态未知时返回 `ErrLifecycleStateUncertain`。
5. 回收前重新读取 Broker 状态，保护未投递、pending、重试、离线必需组和保留期；使用原生事务、锁或 fencing，并限制批量和时间。
6. 实现低基数指标；不支持的值保持 nil/`not_collected`。
7. 运行共享 `VerifyMessageLifecycleConformance`，再运行该 Broker 的单组、多组、慢/离线消费者、漏 ACK、失败、重复投递、进程与 Broker 重启、并发发布/ACK/回收、保留、DLQ 和组变更真 Broker 测试。未运行必须标 `NOT RUN`，mock 不能替代。

声明 `NoRequiredGroups` capability 的 Provider 必须额外通过共享 runner 的无组分支（空主题、observe 不删、容量拒绝、组策略冲突、保留到期、批量回收），并验证意外消费者、重启及拓扑并发边界。不得只取消空组校验或把 `min(empty)` 默认为“全部业务完成”；若无法保护实际存在的消费者，拒绝该策略或关闭该能力。

若 Broker 原生保留机制会在必需组完成前按 TTL、条数、字节或单组 ACK 删除消息，且无法切换成 fail-new 或受安全前沿控制，则该 Provider 必须把 `SafeReclaim` 声明为 false。

## Bitzoom 接入示例

Core 文档不猜测 Bitzoom 具体有哪些服务订阅成交事件。Bitzoom 应从当前真实 `SubscribeEvent(... Reliable:true ...)` 注册点生成唯一 contract manifest：

```go
policy := contract.TradeFilledLifecyclePolicy()
// policy.RequiredGroups 由 Bitzoom 当前可靠订阅注册点生成；不手写猜测服务名。
if err := sc.RequireMessageLifecycle(ctx, policy); err != nil {
	return err
}
```

接入时先盘点正在运行和计划恢复的逻辑消费组、历史重放窗口、平均/峰值消息大小与速率，再确定保留、重试、容量和告警。Bitzoom 工作区的具体 manifest 由其项目按实际注册点实现，Core 不复制业务列表。

只有应用实际注册与部署清单均确认某个主题用于纯历史保留、没有任何 Broker 消费组时，才可使用上述 `NoRequiredGroups:true` 示例。单个发布服务没有本地订阅、订阅服务离线或尚未启动，都不是把成交主题改成无组策略的理由；不能通过空列表兜底自动切换语义。

## 验收命令

```bash
# 纯单元与共享契约
go test -race ./pkg/server/mq ./pkg/server/observability -count=1

# 真 Redis / NATS；缺少环境时测试必须明确 SKIP/NOT RUN
CORE_TEST_REDIS_ADDR=127.0.0.1:6379 \
CORE_TEST_NATS_URL=nats://127.0.0.1:4222 \
go test -race ./pkg/server/mq -count=1

# 启动隔离 Broker，执行共享契约，并分别在 pending 状态重启 Redis 与 NATS 后验证恢复和回收
./scripts/test.sh integration-external-docker

./scripts/ci.sh required/ai-skill
./scripts/test.sh config-contract
./scripts/test.sh api-compat
./scripts/check-logging.sh
```
