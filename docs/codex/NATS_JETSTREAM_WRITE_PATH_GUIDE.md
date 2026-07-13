# NATS JetStream 可靠写路径接入指南

> 本文是架构与接入指南，不表示当前框架已经完成数据库可靠写入、重试或死信队列实现。

## 结论

`PrefixedBadgerDB` 适合保存可按 key 合并的最新状态；NATS JetStream 适合承载不可合并的业务事件和跨进程可靠投递。推荐把远端数据库写入放在 JetStream durable consumer 中，API 或离线节点只负责生成稳定事件并等待发布确认，不直接把 Badger 同步器扩展成通用消息代理。

所有方案都是 at-least-once。Broker 去重不能代替消费者幂等，远端数据库必须用 `event_id` 唯一约束、幂等记录表或等价事务约束防止重复写入。

## 当前框架能力边界

当前 `pkg/server/mq` 已提供：

- `MQManager.Publish` 与 `MQManager.Subscribe` 的统一入口。
- NATS 发布等待 JetStream publish ACK；`PublishOptions.IdempotencyKey` 映射为 `Nats-Msg-Id`。
- 按 subject 创建 stream 和 durable consumer，消费使用显式 ACK。
- `Message.Ack` 只在业务处理成功后调用。
- `ServiceContext` 可按 `MQConfig` 构造和关闭 `MQManager`。

当前实现还不能直接宣称是完整的生产级数据库写通道：

- `Message` 仅暴露 `Ack`，没有 `Nak`、`Term`、`InProgress`。
- 配置尚未暴露 `AckWait`、`MaxDeliver`、退避、`MaxAckPending` 和死信策略。
- stream 尚未暴露存储类型、副本数、保留策略、最大消息年龄/字节数。
- `MQConfig.Retry` 和 `DeadLetter` 当前被校验为未实现，不能配置后假定已生效。
- 当前 `Subscribe` 是 push consume 回调，不是可控制批量和背压的 durable pull consumer。

因此，应先把下述缺口作为独立 MQ Provider 增强任务完成并通过真实 NATS 集成测试，再把数据库写路径标记为生产就绪。本任务不修改这些接口。

## 事件信封

事件一经发布就不可修改，建议使用稳定 JSON 信封：

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

- `event_id`：全局唯一，并同时传给 `PublishOptions.IdempotencyKey`。
- `aggregate_id`：消费者按业务实体控制顺序和并发。
- `event_type` 与 `schema_version`：用于兼容演进；消费者拒绝未知版本并进入死信流程。
- `payload`：只包含完成远端写入所需数据；日志不得输出完整 payload。

建议 subject 使用稳定、低基数命名，例如 `persistence.order.snapshot.v1`。不要把用户 ID、订单 ID 放进 subject。

## 模式一：在线服务默认路径

适用于 API 服务与 NATS、远端数据库均可达的常规部署。

```text
API -> 校验并生成 event_id -> JetStream Publish -> 等待 publish ACK -> 返回已受理
                                                |
durable consumer -> 数据库幂等事务 -> 成功后 Ack
```

1. API 在请求边界生成稳定 `event_id`，同一次业务重试复用该 ID。
2. 调用 `MQManager.Publish(ctx, subject, body, &mq.PublishOptions{IdempotencyKey: eventID})`。
3. 只有 publish 返回 nil 才表示 JetStream 已确认接收；这不表示数据库已经写入。
4. 消费者在一个数据库事务中检查/插入 `event_id` 幂等记录并执行业务写入。
5. 事务提交成功后调用 `msg.Ack()`；失败时不 ACK，由未来的重试策略控制重投。

若调用方必须同步读取刚写入的数据，应使用状态查询、完成事件或有界等待，不要把“发布已确认”包装成“数据库已提交”。

## 模式二：离线节点路径

适用于边缘节点可能长期离线、但本地必须先接受可恢复写入的场景。

```text
本地请求 -> PrefixedBadgerDB 保存待发信封 -> 网络恢复发布 JetStream
                                            -> publish ACK 后标记本地已同步
```

- 本地 Badger 只保存“待发布事件”，不直接同步远端业务数据库。
- 不可合并事件必须以唯一 `event_id` 作为 Badger key；若复用业务实体 key，后写会覆盖前写。
- `EnableWriteBehind` 现有目标是 `ModelList` 远端写回，并不会自动发布 NATS。接入 JetStream 需要单独设计 publisher/outbox adapter，不能靠替换 `SetSyncDB` 偷换语义。
- 只有收到 JetStream publish ACK 后才能删除或确认本地待发记录。
- 本地关闭仍有待发记录时必须返回可观察错误，并保留 Badger 目录。

该模式适合断网缓冲，但多进程争抢、事件顺序、重放速率和本地磁盘上限都必须另设契约测试。

## 模式三：事务 Outbox

适用于业务数据库本身就是事实源，且“业务提交成功但事件未发布”不可接受的核心交易路径。

```text
业务事务 -> 更新业务表 + 插入 outbox
relay/CDC -> JetStream Publish -> publish ACK -> 标记 outbox 已发布
consumer -> 下游幂等处理 -> Ack
```

这是资金、审计和关键状态变更的首选方式。业务表与 outbox 必须在同一数据库事务提交；relay 可重试，JetStream 与消费者均按 `event_id` 幂等。Badger 不参与主数据库事务一致性。

## 基础配置

当前可用配置只覆盖 Provider 选择和命名前缀：

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

`Mode=on` 适合必须依赖 NATS 的写链路，连接失败应阻止服务启动。`Mode=auto` 允许依赖不可用时降级，因此不能用于承诺可靠接收的写 API。

不要启用 `Retry.Enable`、`DeadLetter.Enable`、request/reply 或动态切换；当前配置校验会明确拒绝这些未实现能力。

## 生产化前必须补齐

1. 扩展消息确认契约：`Ack/Nak/Term/InProgress`，并定义 handler panic 与超时行为。
2. 扩展 consumer 配置：`AckWait`、`MaxDeliver`、backoff、`MaxAckPending`、durable/filter subject。
3. 扩展 stream 配置：file storage、生产副本数、retention、`MaxAge`、`MaxBytes`。
4. 实现明确的 DLQ/parking-lot 流程；超过重试上限的事件保留原始 `event_id` 和失败分类。
5. 增加 pull consumer 或等价有界批处理，限制数据库并发和单批事务大小。
6. 增加真实 NATS 集成测试：发布确认、重复 ID、重投、进程重启、消费者故障、DLQ、关闭积压。
7. 增加指标：publish 延迟/失败、consumer backlog、oldest message age、redelivery、DLQ 数、数据库处理延迟。

## 推荐实施顺序

1. 先选择在线 JetStream、离线节点或 transactional outbox，不把三种语义塞进同一个开关。
2. 为事件信封、subject、schema 兼容和消费者幂等写契约测试。
3. 独立增强 NATS Provider 的确认、重试、DLQ、stream/consumer 配置和 pull 消费能力。
4. 使用 `docker-compose.integration.yml` 的 NATS 服务做显式集成测试，默认单元测试继续 skip 外部依赖。
5. 先影子发布并校验消费者幂等，再逐步切换写流量；保留 backlog 和 oldest-age 告警。

在上述门禁完成前，当前 NATS Provider 可用于基础 EventBridge/事件流，但不应作为资金或审计数据“绝不丢失”的唯一承诺。
