# Kafka 与 RabbitMQ 内建 Provider 设计

## 状态

本设计已确认采用“Core 内建 Provider + `ReliableMQProvider`”方案。Kafka 与 RabbitMQ 首版提供普通发布订阅和 Handler 成功后确认的可靠消费，但不声明 `OrderedReliableMQProvider`，不承诺严格的同键有序与失败阻断。

## 背景

Core 已通过 `mq.MQProvider`、可选 `ReliableMQProvider`、`MQManager` 和 `ServiceEventBridge` 隔离业务代码与 Broker。当前内建 Redis Streams 和 NATS JetStream，Kafka、RabbitMQ、RocketMQ 只能由消费方注册自定义 `ProviderFactory`。

go-zero 当前依赖版本为 v1.10.2。Kafka 的 `kq` 能力位于独立的 `zeromicro/go-queue` 生态，不在当前 go-zero 模块的 `core/queue` 下；RabbitMQ 也需要外部 AMQP 客户端。直接包装这些上层组件无法完整表达 Core 的动态 subject、逻辑服务消费组、手动 ACK、上下文取消和统一关闭契约。因此 Kafka 使用维护中的 `segmentio/kafka-go`，RabbitMQ 使用官方 `rabbitmq/amqp091-go`，两者只在 `pkg/server/mq` 内出现，业务继续只依赖 Core MQ 接口。

## 目标

1. `MQ.Provider=kafka` 和 `MQ.Provider=rabbitmq` 可由 `BuildManager` 直接构造，不再要求业务注册 factory。
2. 两种 Provider 均实现 `MQProvider` 和 `ReliableMQProvider`。
3. 发布成功表示 Broker 已确认接收，不把仅写入客户端缓冲区当成成功。
4. 可靠消费仅在 Handler 返回 nil 后 ACK；Handler 返回 error 时消息可重新投递。
5. 连接、订阅、取消、健康检查和关闭遵守 `MQManager` 生命周期，关闭后不再接受新调用。
6. 配置错误 fail closed；只有 `Mode=auto` 下的 Broker 不可达允许按现有规则降级。
7. 更新配置矩阵、兼容表面、Core Skill 和真实外部集成门禁，使消费方可以直接选择两种 Provider。

## 非目标

- 不实现或声明 `OrderedReliableMQProvider`。
- 不启用当前仍被拒绝的通用 `MQ.Retry`、`DeadLetter`、`RequestReply` 或动态切换配置。
- 不实现 Kafka/RabbitMQ 业务级去重；EventID/Inbox 幂等仍由业务负责。
- 不修改 Redis Streams、NATS JetStream、EventBridge、Outbox 或 Runtime Graph 的既有行为。
- 不在业务项目复制 Broker 客户端和事件桥接代码。
- RocketMQ 继续通过自定义 `ProviderFactory` 扩展。

## 公共契约

新增以下导出构造函数，均返回尚未连接的 Provider：

```go
func NewKafkaProvider(config config.KafkaMQConfig) *KafkaProvider
func NewRabbitMQProvider(config config.RabbitMQConfig) *RabbitMQProvider
```

两种类型实现：

```go
var _ MQProvider = (*KafkaProvider)(nil)
var _ ReliableMQProvider = (*KafkaProvider)(nil)
var _ MQProvider = (*RabbitMQProvider)(nil)
var _ ReliableMQProvider = (*RabbitMQProvider)(nil)
```

两者不得实现 `OrderedReliableMQProvider`。服务调用 `RequireOrderedReliableByShardKey()` 时必须继续返回 `ErrOrderedReliableUnsupported`。

`PublishOptions` 和 `ReliableSubscribeOptions` 不改变字段与 JSON/Go 契约：

- `ReliableSubscribeOptions.Group` 是逻辑服务消费组，可靠订阅时必填。
- `Consumer` 是实例消费者标识；Kafka 用作客户端标识，RabbitMQ 用作 consumer tag 的稳定输入。
- `OrderingKey` 在 Kafka 映射为 message key，在 RabbitMQ 写入 `x-core-ordering-key` Header；这只是元数据映射，不构成 ordered-reliable 声明。
- `IdempotencyKey` 写入 Kafka Header 和 RabbitMQ `MessageId`；两种 Broker 均不因此被描述为自动去重。

## 配置

保留已有字段并做加性扩展：

```go
type KafkaMQConfig struct {
    Brokers       []string
    Prefix        string
    ClientID      string
    ConnectTimeout time.Duration
    TLS           MQTLSConfig
    SASL          KafkaSASLConfig
}

type KafkaSASLConfig struct {
    Mechanism string // 空、plain、scram-sha-256、scram-sha-512
    Username  string
    Password  string
}

type RabbitMQConfig struct {
    URL            string
    Exchange       string
    QueuePrefix    string
    Prefetch       int
    ConnectTimeout time.Duration
    TLS            MQTLSConfig
}

type MQTLSConfig struct {
    Enable     bool
    CAFile     string
    CertFile   string
    KeyFile    string
    ServerName string
}
```

默认值：

- Kafka `Prefix=digitalway-core`、`ClientID=digitalway-core`、`ConnectTimeout=10s`。
- RabbitMQ `Exchange=digitalway.core.events`、`QueuePrefix=digitalway-core`、`Prefetch=1`、`ConnectTimeout=10s`。
- TLS 默认关闭；不提供跳过服务端证书验证的配置。

校验规则：

- `Mode=on` 或 `auto` 且选择 Kafka 时，`Brokers` 至少一个非空地址。
- 选择 RabbitMQ 时，`URL` 和 `Exchange` 必须非空，URL scheme 只能是 `amqp` 或 `amqps`。
- Kafka SASL mechanism 非空时必须同时提供 Username/Password；未知 mechanism 拒绝。
- TLS Client Cert 与 Key 必须同时提供；配置了文件但无法读取或解析时构造失败。
- `Prefetch` 必须大于零。
- 错误与日志不得回显 Kafka SASL Password、RabbitMQ URL 中的 userinfo、证书内容或完整连接配置。

配置字段属于加性公开表面。此前 `kafka`、`rabbitmq` 在 `BuildManager` 返回“not implemented”的行为升级为内建构造；自定义 `RegisterProviderFactory` 仍优先于内建 Provider，便于测试和消费方覆盖。

## 资源命名

Broker 资源由 `Prefix/QueuePrefix` 与 subject 确定。映射函数只允许字母、数字、点、下划线和连字符；其他字符折叠为下划线，超长名称截断后附加原始值的短哈希，避免不同 subject 碰撞。

- Kafka topic：`<prefix>.<subject>`。
- RabbitMQ routing key：规范化 subject。
- RabbitMQ durable queue：`<queue-prefix>.<group>.<subject>`。
- RabbitMQ exchange：配置的 durable topic exchange。

资源名不包含用户 ID、订单 ID、TraceID、凭证或原始 payload。

## Kafka Provider

### 连接与健康

`Connect` 构造共享 transport/dialer 和同步 writer，并通过 metadata 请求验证至少一个 Broker 可达。SASL/TLS 只装配在 transport。连接失败包装为 `ErrProviderUnavailable` 的原因，不记录敏感配置。

`Health` 使用有界 metadata 请求检查 Broker；未连接、正在关闭或请求失败返回错误。`Close` 先禁止新发布/订阅，再取消所有 reader、等待 goroutine 退出并关闭 writer/transport；重复关闭保持安全。

### 发布

发布使用同步 `WriteMessages` 和全部副本确认。topic 由 subject 映射，`OrderingKey` 作为 Kafka Key；`IdempotencyKey` 写入 `core-idempotency-key` Header。只有 Broker ACK 成功才返回 nil。

### 普通订阅

`Subscribe` 使用 Provider 默认 group，收到消息后调用现有无错误返回值 Handler，然后提交 offset。返回的 cancel 只终止当前订阅并等待其 reader 退出，不关闭共享 Provider。

### 可靠订阅

`SubscribeReliable` 使用调用方 `Group` 创建 reader，通过 `FetchMessage` 获取一条消息：

1. 构造包含 topic/partition/offset 稳定 ID 的 `Message`。
2. 调用 Handler。
3. Handler 成功后提交当前消息 offset；提交成功才读取下一条。
4. Handler 失败时不提交，也不读取该消费循环的下一条，按固定有界退避重试同一消息，直至成功、取消或 reader 因重平衡失效。
5. 重平衡或连接中断导致提交失败时退出当前 reader 会话，由消费组重新投递未提交消息。

该实现提供 at-least-once。单 reader 失败时可能比 Broker 必需范围阻断更多分区，因此不声明精细的 per-key ordered-reliable 能力。

## RabbitMQ Provider

### 连接与健康

`Connect` 建立 AMQP 连接、声明 durable topic exchange，并创建启用 publisher confirm 的发布 channel。`amqps` 使用系统 CA 或配置的 CA/Client Certificate。

Provider 持有连接状态和订阅描述，而不让业务持有 channel。连接关闭时订阅 supervisor 按有界指数退避重连；发布在断线期间 fail fast，不在内存中无界排队。`Health` 检查当前连接/channel 状态并用有界 channel 操作确认可用。

`Close` 标记关闭、取消全部 consumer、关闭 channel/connection、等待 supervisor 退出。关闭后的重连被禁止。

### 发布

发布 channel 由互斥边界串行保护，避免 AMQP channel 并发误用。消息设置 persistent delivery mode、`MessageId=IdempotencyKey` 和可选 `x-core-ordering-key` Header。`PublishWithContext` 成功后必须等待 publisher confirmation；Nack、channel 关闭或 context 超时均返回失败。

### 普通订阅

`Subscribe` 声明默认 durable queue、绑定 routing key，并使用 manual ACK。Handler 正常返回后 ACK；Provider 不向业务暴露 auto-ack 模式。

### 可靠订阅

`SubscribeReliable` 按 `Group` 声明 durable queue，设置 `Qos(prefetch, 0, false)` 并 manual ACK：

1. Handler 返回 nil 后 ACK。
2. Handler 返回 error 时 `Nack(requeue=true)`，经小幅退避避免紧密重投。
3. consumer/channel 断开时 supervisor 重新声明 exchange、queue、binding 和 consumer；未 ACK 消息由 Broker 重投。

RabbitMQ requeue、多 consumer 和故障恢复时不保证严格原位置顺序，因此不声明 `OrderedReliableMQProvider`。

## 并发与关闭纪律

- Provider 状态由互斥锁或原子状态保护，Broker I/O 不在持有全局状态锁时执行。
- 每个订阅拥有独立 context、cancel 和 wait 记录；取消与 Provider Close 可并发且幂等。
- Handler panic 不能杀死 Provider supervisor；转为失败并保持消息未确认，同时使用稳定事件名记录，不记录 payload。
- Provider 不在调用方 context 取消后继续创建新 goroutine或重试。
- 错误由 Provider 返回给 `MQManager/EventBridge` 决策，日志只在拥有最终重连、降级或终止决策的边界记录一次。

## 测试策略

严格按 RED → GREEN 实施。

### 单元与契约测试

1. 修改 factory 测试，先证明 Kafka/RabbitMQ 仍返回“not implemented”，再实现到可构造路径。
2. 配置测试覆盖默认值、必填字段、SASL/TLS 成对约束、RabbitMQ URL 脱敏和非法值。
3. 编译期断言两种 Provider 实现 `MQProvider`、`ReliableMQProvider`，且 `RequireOrderedReliable` 仍 fail closed。
4. 使用包内受控 adapter/fake 覆盖：发布确认、Handler 成功 ACK、Handler 失败不 ACK/重投、提交失败、重平衡、重连、取消、并发 Close 和 panic 隔离。
5. 运行 `go test -race ./pkg/server/mq ./pkg/server/config ./pkg/server/event`。

### 真实 Broker 集成测试

扩展 `tests/integration/mq_provider_test.go` 与外部 Compose：

- `CORE_TEST_KAFKA=1`：Kafka publish/subscribe、可靠 Handler 首次失败后重投、成功后不再重投、consumer group 隔离、OrderingKey 映射。
- `CORE_TEST_RABBITMQ=1`：RabbitMQ publisher confirm、durable queue、失败 Nack 重投、成功 ACK、连接恢复和 group 隔离。
- EventBridge 分别通过 Kafka/RabbitMQ完成 Envelope round-trip，验证 Subject/EventType/EventID/TraceID，不记录 payload。
- Docker 脚本使用唯一 project、健康检查、超时、日志工件和 `down -v --remove-orphans`，不污染开发者现有 Broker。

真实外部测试进入 scheduled/显式外部门禁，不把 Broker 启动成本加入最小 PR quick；配置、factory、race 和 API 兼容测试进入常规门禁。

## 文档与兼容性

实现时同步更新：

- `docs/ai/core-skill/multiservice-and-observability.md`
- `docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`
- `docs/codex/API_COMPATIBILITY_SURFACE.md`
- `docs/codex/EXTERNAL_INTEGRATION_GUIDE.md`
- `docs/codex/CI_QUALITY_GATE_MATRIX.md`
- `README.md` 中 MQ 能力与最小配置示例

文档必须明确：Kafka/RabbitMQ 是 Conditional 内建 Provider；可靠消费不等于 exactly-once；两者暂不满足 `RequireOrderedReliableByShardKey`；业务必须保留 EventID/Inbox 幂等。

## 验收标准

1. `MQ.Provider=kafka|rabbitmq` 可由 Core 配置直接启动，factory 自定义覆盖仍有效。
2. 两种 Provider 的 Publish 都等待 Broker 确认。
3. `SubscribeReliable` 的 Handler 失败不会 ACK，随后能收到同一消息重投；成功后消息被确认。
4. 两者调用 ordered-reliable requirement 均 fail closed。
5. 断线、取消和 Close 不泄漏 goroutine，不记录敏感连接信息或消息体。
6. 单元、race、配置契约、API/发布契约及真实 Kafka/RabbitMQ/EventBridge 集成测试通过。
7. Redis Streams、NATS JetStream 与自定义 `ProviderFactory` 现有测试无回归。
