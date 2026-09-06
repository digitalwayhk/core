# NATS JetStream 可靠写路径接入指南

本文说明业务数据如何通过 Core 的 NATS JetStream Provider 可靠进入下游数据库。消息生命周期、安全回收和新增 Provider 的统一标准见 [MQ 消息生命周期与 Provider 扩展标准](MQ_MESSAGE_LIFECYCLE_GUIDE.md)。

## 结论

JetStream 适合不可合并的业务事件和跨进程可靠投递；`PrefixedBadgerDB`/`ReliableWriteStore` 适合可按 key 合并的最新状态。资金流水、成交、审计事实等不可合并事件应使用事务 Outbox 或稳定事件 ID，经 JetStream durable consumer 写入远端权威库。

全链路仍是 at-least-once。JetStream `Nats-Msg-Id` 与 DLQ 去重不能代替业务幂等；消费者必须以 `event_id` 唯一约束、幂等表或等价事务约束收敛重复投递。

## 当前 Core 能力

`pkg/server/mq` 当前提供：

- 同步 JetStream publish ACK；`PublishOptions.IdempotencyKey` 映射 `Nats-Msg-Id`，`OrderingKey` 进入稳定 Header。
- `ReliableMQProvider` durable consumer；Handler 成功后 `DoubleAck`，error/panic/timeout 不成功 ACK。
- 按 `LifecyclePolicy.Retry.Backoff` 执行 `NakWithDelay`；`MaxAckPending` 使用 Broker 原生背压。
- 达到 `MaxDeliveries` 后，以稳定 DLQ 消息 ID同步发布并等待 ACK，之后才 `Term` 原消息；DLQ 发布失败时原消息继续重投。
- `RequireMessageLifecycle` 预建全部必需 durable，持久化 enrollment/completed frontier，保护离线组和 pending，并在满足最小保留期后有界 purge。
- 检测自动 `MaxAge`、`DiscardOld` 限额、非 `LimitsPolicy`、不兼容 durable、组缺失和前沿回退并 fail closed。
- retained、bytes、backlog、pending、oldest age、redelivery、DLQ、回收和发布拒绝低基数指标。

当前 NATS Provider 没有声明 `OrderedReliableMQProvider` 或 `KeyedReliableMQProvider`。需要同 OrderingKey 严格失败屏障或跨 key 并行时，Manager 会 fail closed；不能因可靠 ACK 已实现就推导有序能力。

## 事件信封

```json
{
  "event_id": "01J...",
  "aggregate_id": "order-123",
  "event_type": "order.snapshot.requested",
  "schema_version": 1,
  "occurred_at": "2026-07-13T08:00:00Z",
  "trace_id": "...",
  "payload": {}
}
```

- `event_id` 全局唯一，同一次业务重试复用，并传给 `PublishOptions.IdempotencyKey`。
- `aggregate_id` 用于业务幂等与未来明确声明的分区顺序，不把实体 ID 塞进 Subject。
- `event_type` 与 `schema_version` 用于兼容演进；未知版本返回错误并进入重试/DLQ。
- payload 只含处理所需数据，不写日志或指标标签。

Subject 使用稳定低基数命名，例如 `persistence.order.snapshot.v1`。

## 在线服务与事务 Outbox

普通在线链路：

```text
API -> 校验并生成 event_id -> JetStream Publish -> publish ACK -> 返回已受理
durable consumer -> 数据库幂等事务 -> Handler nil -> DoubleAck
```

publish ACK 表示 Broker 已持久化接收，不表示数据库已提交。需要同步读取本次写入时使用状态查询、完成事件或有界等待，不能把“已发布”包装成“已落业务库”。

核心交易路径优先使用事务 Outbox：

```text
业务事务 -> 更新业务表 + 插入 outbox
relay/CDC -> JetStream publish ACK -> 标记 outbox 已发布
consumer -> 下游幂等事务 -> DoubleAck
```

业务表与 Outbox 必须在同一数据库事务中提交。relay 可以重试，事件 ID 在 Broker 和消费者两端保持稳定。

离线边缘节点可先在本地可靠存储待发事件，网络恢复后发布；只有收到 publish ACK 才能确认或删除本地记录。不可合并事件必须以唯一事件 ID 为 key，不能用业务实体 key 让后写覆盖前写。

## 生命周期声明

生命周期是应用 contract，不是基础设施配置猜测：

```go
policy := contract.OrderSnapshotLifecyclePolicy()
if err := sc.RequireMessageLifecycle(ctx, policy); err != nil {
	return err
}
```

manifest 必须列出当前实际可靠订阅的逻辑组，并明确 `StartFromAllRetained` 或 `StartFromNew`。先以 `LifecycleModeObserve` 上线，核对 durable、pending、lag、oldest age 和容量；确认后通过重启切换 `LifecycleModeEnforce`。

不调用 `RequireMessageLifecycle` 时保持旧行为：不自动安全回收，可靠 Handler 失败无限重试。`ServerConfig.MQ.Retry`/`DeadLetter` 仍是 rejected 的旧容器，不能表达 Subject 和必需组，不应启用。

## 基础配置

```json
{
  "MQ": {
    "Mode": "on",
    "Provider": "nats-jetstream",
    "Usage": ["event-stream"],
    "NATSJetStream": {
      "URL": "nats://127.0.0.1:4222",
      "StreamPrefix": "orders-prod",
      "DurablePrefix": "orders-writer"
    }
  }
}
```

必须依赖可靠接收的写 API 使用 `Mode=on`，连接或 lifecycle capability 不满足时阻止启动。`Mode=auto` 允许外部依赖不可用时降级，不适合承诺可靠接收的接口。

Broker 生产部署还要单独确定 file storage、副本数、磁盘、账号配额、TLS/凭据、备份恢复和监控。Core lifecycle 管理的 Stream 禁止外部设置会删除未完成消息的 `MaxAge`/`DiscardOld`/per-subject TTL。声明硬容量时 NATS 使用 `DiscardNew` 拒绝新发布，不删除旧消息。

NATS 管理 API 无法把“读取全部 durable 状态”和“purge”组合成一条事务。生产账号必须用 ACL 禁止其他应用并发删除或重建 lifecycle stream/durable；组变更按长期指南进入维护窗口。

## Handler、重试与 DLQ

- 数据库事务提交前不得返回 `nil`。返回 error、panic 或超时会重投。
- Handler 自身必须设置数据库/HTTP context 超时并保持线程安全；Go 不能强制终止忽略取消的 goroutine。
- `MaxAckPending` 应匹配数据库连接池和允许的并发事务，不能靠大值掩盖下游不足。
- Backoff 数组用完后重复最后一个值。达到上限后 DLQ 先同步 publish ACK，再 `Term`。
- DLQ 保留原 payload、源 Subject、逻辑组、源 stream sequence、OrderingKey 和固定失败分类；日志不输出这些高基数身份或 payload。
- DLQ 是待补偿事实，必须单独声明容量、保留、告警和人工/自动恢复流程。

## 容量与观测

估算至少包含平均消息大小、峰值发布速率、正常保留时间、最坏必需组离线窗口、复制倍数、Stream 索引、durable 状态、KV 元数据和 DLQ。消费者离线时应告警、扩容、背压或让发布明确失败，不能删除未完成消息。

重点指标：`retained_messages`、`retained_bytes`、`backlog_messages`、`pending_messages`、`oldest_age_sec`、`redelivered_total`、`dead_letter_total`、`dead_letter_fail_total`、`reclaimed_total`、`reclaim_fail_total`、`publish_rejected_total`。未采集值省略并标记 `not_collected`，不填假零。

## 真 Broker 验收

```bash
docker compose --project-name core-mq-lifecycle \
  -f docker-compose.integration.yml up -d --wait nats

CORE_TEST_NATS_URL=nats://127.0.0.1:4222 \
go test -race ./pkg/server/mq -run '^TestNATS|MessageLifecycleConformance' -count=1
```

发布前还要验证真实生产拓扑下的进程重启、Broker 重启、慢/离线消费者、漏 ACK、重复投递、DLQ 故障、并发发布/ACK/回收、容量拒绝和受控组变更。未运行的场景明确写 `NOT RUN`，不能用 mock 代替。
