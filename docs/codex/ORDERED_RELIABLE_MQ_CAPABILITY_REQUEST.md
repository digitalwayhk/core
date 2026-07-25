# Ordered-Reliable MQ 通用能力请求

> 日期：2026-07-24
> 类型：provider-neutral 能力请求
> 状态：请求评审；本文不包含 Core 实现
> 来源：Bitzoom `Trades -> Positions` 的 `TradeFill` 顺序与可靠投递需求

## 1. 结论

Core 需要在现有 `MQProvider`、`ReliableMQProvider`、`ServiceEventBridge` 和
Outbox 之上，增加一项可声明、可验证的可选能力：

> 同一个业务 `OrderingKey` 的控制事件按序、可靠、失败阻断；不同 key
> 可以并行处理。

该能力必须与具体 broker 解耦。Kafka、NATS JetStream、RabbitMQ、
RocketMQ、Redis Streams 或通过 `RegisterProviderFactory` 注册的外部
provider，可以用各自机制满足同一行为契约。

本请求不要求业务服务导入具体 broker client，也不要求已有普通
`MQProvider` 实现立即升级。未声明 ordered-reliable 需求的服务保持现有行为；
显式声明该需求的服务如果拿不到符合契约的 provider，则必须 fail closed。

## 2. 当前能力与缺口

当前 `main` 已有：

- `mq.MQProvider` 的统一连接、发布、订阅与健康检查；
- 可选 `mq.ReliableMQProvider`，handler 成功后 ACK，失败时消息保持 pending；
- `mq.RegisterProviderFactory` 自定义 provider 扩展点；
- `event.Envelope.ShardKey` / `IdempotencyKey`；
- `event.ExternalPublisher`、`ExternalSubscriber`、
  `ReliableExternalSubscriber`；
- `ServiceEventBridge` 进程内按 `ShardKey` 固定分片串行处理控制事件；
- Outbox 的 `IdempotencyKey`、`ShardKey`、`LoadPending` 和
  `MarkPublished`；
- Redis Streams 基础 provider，并实现了 `ReliableMQProvider`；
- NATS JetStream 基础 provider（仅普通 Publish/Subscribe）。

当前缺口（已对照源码）：

- `ReliableMQProvider` 只声明可靠 ACK，没有声明同 key 的有序语义；
- `PublishOptions` 只有 `Subject` 与 `IdempotencyKey`，没有显式
  `OrderingKey` / `PartitionKey`；
- `MQBridge.Publish` 序列化 Envelope 后以 `opts=nil` 调用
  `MQManager.Publish`，既不透传 `Envelope.IdempotencyKey`，也不把
  `Envelope.ShardKey` 作为 provider 发布元数据透传；
- Outbox 一条记录发布失败后，`drain` 会 `continue` 尝试同批后续记录，没有
  “同 key 最早失败记录阻断后续记录”的通用契约；
- 服务启动时无法声明并验证“必须支持 ordered-reliable by key”；
- 现有 provider 没有共用的 ordered-reliable conformance suite；
- 内置 NATS JetStream provider **未实现** `ReliableMQProvider`；
- 内置 Redis Streams 的 `SubscribeReliable`：
  - 默认按批 `Count` 拉取，handler 失败后仍会继续处理同批后续消息；
  - 同 consumer group 多实例会分片消费，不保证全局按 key 顺序；
  - 因此“可靠 ACK”不能推导为“同 OrderingKey 有序与失败阻断”；
- Kafka、RabbitMQ、RocketMQ 需自定义 factory，且没有统一有序验收套件；
- 当前 NATS 指南已经明确：基础 EventBridge/事件流可用，但完整生产级可靠
  写通道仍需补齐确认、重投、背压、DLQ 和真实集成门禁。

因此，“provider 支持可靠订阅”不能自动推导为“provider 支持业务 key
有序、失败阻断和故障接管”。

## 3. 行为契约

### 3.1 同一个 OrderingKey

必须满足：

1. 按 broker 接受的发布顺序交付；
2. 同一时刻最多一个业务 handler in-flight；
3. handler N 失败时，N+1 不得越过（含同批拉取内的后续消息）；
4. handler 成功返回后才能 ACK 或 commit offset；
5. consumer 故障转移后保持同 key 顺序；
6. 允许 at-least-once 重复，不允许丢失或越序；
7. 重投保持原事件身份（`Envelope.ID` / `OutboxMessage.EventID`）、
   `IdempotencyKey`、payload 和 OrderingKey；
8. 消费者以 EventID/Inbox 完成最终业务幂等。

### 3.2 不同 OrderingKey

允许：

- 映射到不同 partition、shard、queue 或 message group；
- 并行消费；
- 单 key 的失败不阻断无关 key。

Core 规定行为，不规定 provider 内部必须使用 partition、subject、queue、
consumer group 还是 owner lease。

### 3.3 顺序边界：broker 序、业务序与多写者

本能力默认保证的是 **同一 OrderingKey 在 broker 侧的接受序**，不是任意业务序号：

1. 同 key 的跨进程有序，以 broker 接受并持久化后的顺序为准；
2. 业务若需要强于 broker 的业务序号（如 Bitzoom `matchSequence`），由业务
   Inbox / cursor / 业务字段负责最终收敛，不能假设 broker 序等于业务序；
3. 多写者并发向同一 OrderingKey 发布，不在本能力默认保证内。业务侧应约定
   单 active publisher（例如每 market 单 actor），或与 provider 共同约定
   序号合成 / 单写者租约；否则只保证“每个写者各自成功 publish 后的
   broker 接受序”，不保证跨写者的业务全序。

## 4. 建议的加性 API

以下名称用于表达需求，最终命名以 Core review 为准；行为契约不应弱化。

### 4.1 发布元数据

```go
type PublishOptions struct {
    Subject        string
    IdempotencyKey string
    OrderingKey    string // 新增；零值保持现有行为
}
```

`MQBridge.Publish` 应显式完成（默认 ExternalPublisher 路径）：

```text
Outbox / ServiceEventBridge
  -> ExternalPublisher (= MQBridge)
  -> Envelope.IdempotencyKey -> PublishOptions.IdempotencyKey
  -> Envelope.ShardKey       -> PublishOptions.OrderingKey
  -> MQManager.Publish
```

provider 不应通过解析业务 JSON 获取分区键或幂等键。透传在 `MQBridge`
内部从 Envelope 提取即可，不必为此修改 `ExternalPublisher` 接口签名。

空 key fail-closed：

- 未声明 ordered-reliable 时，`ShardKey` / `OrderingKey` 零值保持现有行为
  （含 Outbox 回落到 `eventType:id` 的兼容路径）；
- 已声明 `RequireOrderedReliableByShardKey`（或等价声明）时，空
  `ShardKey` / `OrderingKey` 必须在发布路径直接失败，禁止静默回落到
  每条事件唯一的伪 key，否则“看起来声明了有序，实际无跨事件顺序”。

### 4.2 可选能力接口

```go
type OrderedReliableCapability struct {
    Delivery       string // AT_LEAST_ONCE
    OrderingScope  string // ORDERING_KEY
    AckPolicy      string // AFTER_HANDLER_SUCCESS
    FailurePolicy  string // BLOCK_SAME_KEY
    FailoverPolicy string // KEEP_KEY_ORDER
}

// OrderedReliableMQProvider 是可选扩展。
// 仅声明能力不够；必须通过 conformance suite 证明实际行为。
type OrderedReliableMQProvider interface {
    ReliableMQProvider
    OrderedReliableInfo() OrderedReliableCapability
}
```

该接口必须是可选扩展，避免破坏现有只实现普通 Pub/Sub 或可靠 ACK 的 provider。
不能通过 provider 名称白名单推测能力。

启动检查 = 类型断言 + `OrderedReliableInfo` 字段合法 +（可选）最小 smoke；
**行为以 conformance suite 为准**，禁止只靠 Info 自报放行生产。有序语义同时
依赖发布侧 `OrderingKey` 透传与消费侧 `SubscribeReliable` 的实际失败阻断，
不能只实现其中一侧。

### 4.3 服务启动声明

建议由 `ServiceContext` / `ServiceEventBridge` 提供等价于以下语义的声明：

```text
RequireOrderedReliableByShardKey
```

生产启动行为：

- provider 明确声明并满足能力：允许注册 ordered-reliable 控制订阅；
- provider 只支持普通 Pub/Sub：fail closed；
- provider 支持可靠 ACK，但不保证同 key 顺序：fail closed；
- 未配置外部 provider：fail closed；
- 测试可以显式注入满足契约的 fake provider。
- 发布时若 OrderingKey/ShardKey 为空：fail closed（见 §4.1）。

## 5. Outbox 同 key 失败屏障

Core Outbox publisher 需要定义：

- 同 OrderingKey 最早未发布记录失败后，本轮不再尝试该 key 后续记录；
- 其他 OrderingKey 可以继续；
- `MarkPublished` 失败允许同 EventID 重发；
- provider 支持 broker dedup 时使用 `IdempotencyKey`；
- provider 不支持 broker dedup 时依赖消费者 Inbox 幂等；
- 重启后从持久化最早 unpublished 记录恢复，不能只依赖进程内 blocked map。

实现可以位于 Outbox publisher、`OutboxStore` 的扩展能力，或二者组合，但行为必须
由 provider-neutral 测试固定。不得要求 `OutboxStore` 理解消费者或具体 broker。

## 6. Provider 映射原则

| Provider | OrderingKey 可能映射 |
| --- | --- |
| Kafka | record key 到 partition；每 partition 单 consumer |
| NATS JetStream | 固定 shard subject/consumer，或等价有序 consumer |
| RabbitMQ | consistent-hash routing 或固定 shard queue |
| RocketMQ | message group/sharding key 与 orderly consumer |
| Redis Streams | 固定 shard stream 与单 shard active owner |

provider adapter 自己负责 rebalance、pending、ACK、owner 接管和关闭语义。
Core 不应把 Redis consumer group 或任何单一 broker 的偶然行为当作通用契约。

内置 provider 落地时的特别说明：

- Redis：需从“单 stream + 多 consumer 分片”升级为“按 key 的 shard stream /
  单 active owner”，并在 handler 失败时阻断同 key 后续消息（含同批）；
- NATS：需先补齐 `ReliableMQProvider`，再叠加 ordered-by-key 语义；
- 自定义 factory：同样必须通过同一 conformance suite，不能只注册名字。

## 7. Conformance suite

Core 应提供所有 ordered-reliable provider 可复用的验收套件：

1. 同 key 100 条严格按序；
2. 不同 key 可并行；
3. 第 N 条失败时 N+1 不执行（含同批拉取场景）；
4. N 恢复后继续处理 N+1；
5. handler 成功前不得 ACK；
6. consumer 退出后另一实例接管；
7. 接管允许重复，但不丢失、不越序；
8. 重投保持 EventID、payload、IdempotencyKey 和 OrderingKey；
9. provider 不具备能力时启动检查明确失败；
10. capability 声明与实际行为不一致时拒绝；
11. 已声明 requirement 时，空 ShardKey/OrderingKey 发布失败；
12. race 门禁通过；
13. 每个生产 provider 的真实 broker integration gate 通过。

测试层次：

- provider-neutral fake：锁定接口和启动检查；
- 每个 provider 的真实 broker 测试：锁定发布、失败阻断、接管和关闭行为；
- broker 不可达测试：验证 fail closed；
- Outbox 持久恢复测试：验证最早 unpublished 屏障跨重启成立。

## 8. 向后兼容与文档门禁

- `MQProvider` 既有实现继续编译；
- 新能力通过可选接口和加性字段表达；
- 新字段零值保持现有行为；
- 未声明 ordered requirement 的服务保持现有语义；
- 显式声明 requirement 的服务不得静默降级；
- Event Envelope 的既有 JSON 字段保持兼容；
- 公共 API 变更同步
  `API_COMPATIBILITY_SURFACE.md`、apidiff 基线和迁移说明；
- 新配置同步 `CONFIG_RUNTIME_CAPABILITY_MATRIX.md` 与
  `config-contract`；
- provider 行为同步 EventBridge、NATS/Redis 接入指南和
  `use-digitalway-core` skill；
- 实现 PR 至少运行 `quick`、`release-contract`、server/manage、
  `concurrency-race` 和对应真实 broker integration gate。

## 9. Bitzoom 验证样本

Bitzoom 当前为 `TradeFill` 实现了项目内 Redis Streams adapter，作为 Core
通用能力落地前的业务验证样本：

- Trades 为每个 market 分配 durable `matchSequence`；
- 双侧 TradeFill Outbox 与订单、sequence state 同事务；
- Outbox 在最早 unpublished Fill 前设置失败屏障；
- Positions 在每个 market DB 内维护 `(marketID, userID)` sequence cursor；
- cursor 与 Position、TradeApplication、Settlement、OpenClose、Inbox、
  Outbox 同 MySQL 事务；
- 真实 Redis 覆盖 100 条顺序、第 17 条失败阻断第 18 条、pending 接管；
- 真实 MySQL + Redis 覆盖开仓、加仓、减仓、平仓和 ACK 前退出恢复。

该项目实现只用于证明需求和提供验收样本，不应原样复制为 Core 的通用框架。
Core 现有 `redis-stream` provider 本身**不**等于上述 ordered-reliable 语义。

## 10. 实现 PR 的完成定义

后续 Core 实现 PR 至少交付：

- provider-neutral API 与启动能力检查；
- OrderingKey 与 IdempotencyKey 从 Event Envelope 到 provider 的完整透传；
- Outbox 同 key failure barrier；
- 至少一个生产级 provider adapter（含真实 broker integration）；
- provider-neutral conformance suite；
- 配置矩阵、公共 API、使用指南和能力矩阵更新；
- release-contract、race、vet、format 门禁；
- 正式版本发布说明。

### 10.1 实现状态（对照 main，截至 residual 修复）

已合入：API/透传/Require fail-closed、Outbox barrier（含可选 SkipBlocked）、
Redis 单 owner + 读 `>` 前 reclaim pending（MinIdle=0）+ lost-owner 不 ACK、
`VerifyOrderedReliableFailureBarrier`、能力矩阵与 API 兼容表面登记。

发版门禁（每个生产 provider 必须通过，含自定义 factory）：

```bash
# 行为套件（fake 或真实 provider）
go test ./pkg/server/mq/ -count=1 -run 'Conformance|OrderedReliable|FakeOrdered'

# 真 Redis integration（需环境变量）
CORE_TEST_REDIS_ADDR=127.0.0.1:6379 go test ./pkg/server/mq/ -count=1 -run RedisReliable
```

handler 与**单次 pending 排空总时长**均应落在 owner lease TTL 内（默认约
`max(2m, 3×MinIdle)`）。实现会在每条成功处理后 refresh lease；超时后旧 owner
不得 ACK，新 owner 先回收 pending（MinIdle=0）再读新消息。

## 11. Bitzoom 迁移条件

Bitzoom 只有在以下条件**同时**满足后，才评估替换项目内 adapter：

1. Core 实现 PR 已合并；
2. ordered-reliable conformance suite 真实通过；
3. 至少一个 Bitzoom 生产选用 provider 已实现该能力；
4. release-contract、race 与外部 integration gate 通过；
5. 新 Core 正式版本已发布；
6. Bitzoom `go.mod` 可直接引用该版本；
7. 不使用 `go.work`、`replace` 或未发布本地 Core 冒充迁移完成；
8. Bitzoom Trades→Positions 双实例 UAT 通过；
9. `eventID`、`tradeID`、`matchSequence` 与失败阻断语义保持不变。

PR merged 不是迁移完成的充分条件；必须验证实际发布版本和运行行为。
