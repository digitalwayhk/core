# Kafka 与 RabbitMQ 内建 Provider 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 Core 增加可由 `MQ.Provider=kafka|rabbitmq` 直接启用、实现 `MQProvider` 与 `ReliableMQProvider`、但不声明 `OrderedReliableMQProvider` 的两个内建 Provider。

**Architecture:** Kafka 使用 `segmentio/kafka-go` 的同步 writer 与显式 `FetchMessage/CommitMessages`；RabbitMQ 使用 `rabbitmq/amqp091-go` 的 durable topic exchange、publisher confirm、manual ACK 与订阅重连 supervisor。公共配置校验、TLS 装配、资源命名和生命周期全部留在 `pkg/server/config` 与 `pkg/server/mq`，业务层接口不变化。

**Tech Stack:** Go 1.26、`segmentio/kafka-go`、`rabbitmq/amqp091-go`、现有 `MQManager/EventBridge`、Docker Compose 外部集成测试。

---

### Task 1: 扩展并校验 MQ 配置

**Files:**
- Modify: `pkg/server/config/mqconfig.go`
- Modify: `pkg/server/config/mqconfig_test.go`

- [x] **Step 1: 写配置默认值与非法配置 RED 测试**

```go
func TestMQConfigApplyDefaults_KafkaAndRabbitMQ(t *testing.T) {
	var cfg MQConfig
	cfg.ApplyDefaults()
	assert.Equal(t, "digitalway-core", cfg.Kafka.Prefix)
	assert.Equal(t, "digitalway-core", cfg.Kafka.ClientID)
	assert.Equal(t, 10*time.Second, cfg.Kafka.ConnectTimeout)
	assert.Equal(t, "digitalway.core.events", cfg.RabbitMQ.Exchange)
	assert.Equal(t, "digitalway-core", cfg.RabbitMQ.QueuePrefix)
	assert.Equal(t, 1, cfg.RabbitMQ.Prefetch)
	assert.Equal(t, 10*time.Second, cfg.RabbitMQ.ConnectTimeout)
}
```

再以 table test 覆盖 Kafka 空 Broker、未知 SASL、SASL 缺用户名/密码、TLS cert/key 不成对，以及 RabbitMQ 空 URL/Exchange、非法 scheme、`Prefetch<=0`。

- [x] **Step 2: 运行测试并确认按缺少字段/校验失败**

Run: `rtk go test ./pkg/server/config -run 'TestMQConfigApplyDefaults_KafkaAndRabbitMQ|TestMQConfigValidate_(Kafka|RabbitMQ|TLS)' -count=1`

Expected: FAIL，原因是扩展字段尚不存在或非法配置尚未拒绝。

- [x] **Step 3: 增加加性配置类型、默认值和 fail-closed 校验**

```go
type MQTLSConfig struct {
	Enable bool `json:",optional"`
	CAFile string `json:",optional"`
	CertFile string `json:",optional"`
	KeyFile string `json:",optional"`
	ServerName string `json:",optional"`
}

type KafkaSASLConfig struct {
	Mechanism string `json:",optional"`
	Username string `json:",optional"`
	Password string `json:",optional"`
}
```

扩展 `KafkaMQConfig` 与 `RabbitMQConfig`，在 `ApplyDefaults` 填充设计默认值，在 `Validate` 对 `auto|on` 的所选 Provider 执行必填、scheme、SASL、TLS 成对和 Prefetch 校验；`Mode=off` 保持旧配置兼容。

- [x] **Step 4: 运行配置测试确认通过**

Run: `rtk go test ./pkg/server/config -count=1`

Expected: PASS。

### Task 2: 增加安全 TLS/SASL 与资源命名基础工具

**Files:**
- Create: `pkg/server/mq/provider_security.go`
- Create: `pkg/server/mq/provider_security_test.go`
- Create: `pkg/server/mq/provider_resource_name.go`
- Create: `pkg/server/mq/provider_resource_name_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [x] **Step 1: 写资源名稳定、字符集、截断防碰撞与 TLS 错误脱敏 RED 测试**

```go
func TestMQResourceName_LongSubjectsRemainDistinct(t *testing.T) {
	a := mqResourceName("digitalway-core", strings.Repeat("a", 300)+"x", 249)
	b := mqResourceName("digitalway-core", strings.Repeat("a", 300)+"y", 249)
	require.NotEqual(t, a, b)
	require.LessOrEqual(t, len(a), 249)
}
```

同时验证仅保留字母、数字、点、下划线、连字符，TLS cert/key 缺一项立即失败，错误不包含文件内容。

- [x] **Step 2: 运行测试确认 helper 尚不存在**

Run: `rtk go test ./pkg/server/mq -run 'TestMQResourceName|TestBuildMQTLSConfig' -count=1`

Expected: FAIL，helper 未定义。

- [x] **Step 3: 实现共享 helper 并引入 Broker 客户端**

```go
func mqResourceName(prefix, value string, max int) string
func buildMQTLSConfig(cfg config.MQTLSConfig) (*tls.Config, error)
func redactedAMQPURL(raw string) string
```

资源名对非法字符折叠为 `_`；超长时保留前缀并附 SHA-256 短哈希。TLS 使用系统 CA，可选追加 CA 文件，可选 client cert/key，不设置 `InsecureSkipVerify`。加入 `github.com/segmentio/kafka-go` 与 `github.com/rabbitmq/amqp091-go`。

- [x] **Step 4: 运行 helper 与配置测试**

Run: `rtk go test ./pkg/server/mq ./pkg/server/config -count=1`

Expected: PASS。

### Task 3: 实现 Kafka Provider 核心契约

**Files:**
- Create: `pkg/server/mq/provider_kafka.go`
- Create: `pkg/server/mq/provider_kafka_test.go`

- [x] **Step 1: 写编译期能力、元数据映射、ACK/失败重试、取消与关闭 RED 测试**

```go
var _ MQProvider = (*KafkaProvider)(nil)
var _ ReliableMQProvider = (*KafkaProvider)(nil)

func TestKafkaProviderDoesNotDeclareOrderedReliable(t *testing.T) {
	var provider MQProvider = NewKafkaProvider(config.KafkaMQConfig{Brokers: []string{"127.0.0.1:9092"}})
	_, ok := provider.(OrderedReliableMQProvider)
	require.False(t, ok)
}
```

通过包内 `kafkaReader`/`kafkaWriter`/metadata adapter fake 验证：同步写包含 Key 与 `core-idempotency-key`；Handler 成功后 commit；Handler 失败不 commit 并重试同一消息；commit 失败退出 session；panic 转失败；cancel/Close 幂等且关闭后返回 `ErrNotConnected`。

- [x] **Step 2: 运行测试确认 Provider 尚不存在**

Run: `rtk go test ./pkg/server/mq -run 'TestKafkaProvider' -count=1`

Expected: FAIL，`KafkaProvider`/构造函数未定义。

- [x] **Step 3: 实现 Kafka Provider**

```go
func NewKafkaProvider(cfg config.KafkaMQConfig) *KafkaProvider
func (p *KafkaProvider) Connect(ctx context.Context) error
func (p *KafkaProvider) Publish(ctx context.Context, subject string, data []byte, opts *PublishOptions) error
func (p *KafkaProvider) Subscribe(ctx context.Context, subject string, handler func(*Message)) (func(), error)
func (p *KafkaProvider) SubscribeReliable(ctx context.Context, subject string, options ReliableSubscribeOptions, handler func(*Message) error) (func(), error)
func (p *KafkaProvider) Health(ctx context.Context) error
func (p *KafkaProvider) Close() error
```

`Connect` 通过 metadata 请求验证 Broker；writer 使用 `RequiredAcks=RequireAll` 和同步写；reader 以逻辑 Group 消费；可靠循环只在 Handler 成功后 commit，失败固定有界退避重试当前消息。所有订阅都登记 cancel/wait，Close 先切换 closed 状态再取消并等待。

- [x] **Step 4: 运行 Kafka 单元与 race 测试**

Run: `rtk go test -race ./pkg/server/mq -run 'TestKafkaProvider' -count=1`

Expected: PASS。

### Task 4: 实现 RabbitMQ Provider 核心契约

**Files:**
- Create: `pkg/server/mq/provider_rabbitmq.go`
- Create: `pkg/server/mq/provider_rabbitmq_test.go`

- [x] **Step 1: 写编译期能力、publisher confirm、ACK/NACK、重连、取消与关闭 RED 测试**

```go
var _ MQProvider = (*RabbitMQProvider)(nil)
var _ ReliableMQProvider = (*RabbitMQProvider)(nil)

func TestRabbitMQProviderDoesNotDeclareOrderedReliable(t *testing.T) {
	var provider MQProvider = NewRabbitMQProvider(config.RabbitMQConfig{URL: "amqp://guest:guest@localhost:5672/", Exchange: "events", Prefetch: 1})
	_, ok := provider.(OrderedReliableMQProvider)
	require.False(t, ok)
}
```

通过包内 AMQP adapter fake 验证 persistent publish、`MessageId`、`x-core-ordering-key`、confirm ACK/NACK、Handler nil 后 Ack、error/panic 后 Nack requeue、channel 关闭后 supervisor 重建、并发 cancel/Close 幂等。

- [x] **Step 2: 运行测试确认 Provider 尚不存在**

Run: `rtk go test ./pkg/server/mq -run 'TestRabbitMQProvider' -count=1`

Expected: FAIL，`RabbitMQProvider`/构造函数未定义。

- [x] **Step 3: 实现 RabbitMQ Provider**

```go
func NewRabbitMQProvider(cfg config.RabbitMQConfig) *RabbitMQProvider
func (p *RabbitMQProvider) Connect(ctx context.Context) error
func (p *RabbitMQProvider) Publish(ctx context.Context, subject string, data []byte, opts *PublishOptions) error
func (p *RabbitMQProvider) Subscribe(ctx context.Context, subject string, handler func(*Message)) (func(), error)
func (p *RabbitMQProvider) SubscribeReliable(ctx context.Context, subject string, options ReliableSubscribeOptions, handler func(*Message) error) (func(), error)
func (p *RabbitMQProvider) Health(ctx context.Context) error
func (p *RabbitMQProvider) Close() error
```

连接时声明 durable topic exchange 并启用 confirm；发布 channel 由 mutex 串行保护且等待 confirmation；每个订阅 supervisor 持有自己的 channel、durable queue/binding 和 manual ACK consumer，断线按有界指数退避重连，Close 后禁止重连。

- [x] **Step 4: 运行 RabbitMQ 单元与 race 测试**

Run: `rtk go test -race ./pkg/server/mq -run 'TestRabbitMQProvider' -count=1`

Expected: PASS。

### Task 5: 接入内建 factory 并保持自定义覆盖优先

**Files:**
- Modify: `pkg/server/mq/factory.go`
- Modify: `pkg/server/mq/factory_test.go`

- [x] **Step 1: 把“not implemented”测试改成 Kafka/RabbitMQ 构造与配置错误 RED 测试**

使用不可达本地端口分别断言 `Mode=on` 返回 `ErrProviderUnavailable`，`Mode=auto` 仅对该错误降级；保留注册同名 custom factory 的测试并断言其优先于内建构造。RocketMQ 继续断言 `not implemented`。

- [x] **Step 2: 运行 factory 测试确认 Kafka/RabbitMQ 仍走旧拒绝分支**

Run: `rtk go test ./pkg/server/mq -run 'TestBuildManager_(Kafka|RabbitMQ|RocketMQ|RegisteredBuiltinOverride)' -count=1`

Expected: FAIL，Kafka/RabbitMQ 仍返回 `ErrProviderConfiguration`。

- [x] **Step 3: 在 `buildProvider` 构造并连接两个内建 Provider**

```go
case "kafka":
	provider := NewKafkaProvider(cfg.Kafka)
	if err := provider.Connect(ctx); err != nil { return nil, fmt.Errorf("%w: connect kafka: %v", ErrProviderUnavailable, err) }
	return provider, nil
case "rabbitmq":
	provider := NewRabbitMQProvider(cfg.RabbitMQ)
	if err := provider.Connect(ctx); err != nil { return nil, fmt.Errorf("%w: connect rabbitmq: %v", ErrProviderUnavailable, err) }
	return provider, nil
```

自定义 factory 检查保持在 switch 前；配置错误先由 `cfg.Validate()` 或 Provider 安全校验 fail closed，不得在 auto 模式降级。

- [x] **Step 4: 运行 factory、manager、event 回归**

Run: `rtk go test -race ./pkg/server/mq ./pkg/server/config ./pkg/server/event -count=1`

Expected: PASS。

### Task 6: 增加真实 Kafka/RabbitMQ/EventBridge 集成门禁

**Files:**
- Modify: `docker-compose.integration.yml`
- Modify: `tests/integration/mq_provider_test.go`
- Create: `scripts/test-external-mq.sh`
- Modify: `scripts/test-compose-contract.sh`

- [x] **Step 1: 写环境变量门控的真实 Broker 测试**

Kafka 与 RabbitMQ 各自覆盖 publish/subscribe、首次 Handler 失败后的同消息重投、成功后不再重投、不同 group 独立消费、OrderingKey 元数据，并让 EventBridge 做 Envelope round-trip。

- [x] **Step 2: 运行未启用外部环境的测试确认安全 SKIP**

Run: `rtk go test ./tests/integration -run 'Test(Kafka|RabbitMQ).*Provider|TestEventBridge.*(Kafka|RabbitMQ)' -count=1`

Expected: PASS with SKIP，不接触现有 Broker。

- [x] **Step 3: 增加独立 Compose profile 与脚本**

脚本用唯一 project name，分别启动 Kafka/RabbitMQ，等待健康后设置 `CORE_TEST_KAFKA=1`/`CORE_TEST_RABBITMQ=1`，失败保留日志工件，退出时只对本 project 执行 `down -v --remove-orphans`。

- [x] **Step 4: 运行 Compose 静态契约与可用的真实 Broker 测试**

状态：Compose 静态契约 PASS；真实 Broker 脚本已尝试，但 Docker Compose 在 10 分钟内未完成环境启动，Kafka、RabbitMQ 与 EventBridge 真实场景记为 `NOT RUN`。

Run: `rtk ./scripts/test-compose-contract.sh`

Run when Docker is available: `rtk ./scripts/test-external-mq.sh`

Expected: 静态契约 PASS；真实 Kafka、RabbitMQ 与 EventBridge 场景 PASS。若本机 Docker/镜像不可用，明确记录 `NOT RUN`，不得假绿。

### Task 7: 更新文档、Skill 与公开兼容契约

**Files:**
- Modify: `docs/ai/core-skill/multiservice-and-observability.md`
- Modify: `docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`
- Modify: `docs/codex/API_COMPATIBILITY_SURFACE.md`
- Modify: `docs/codex/EXTERNAL_INTEGRATION_GUIDE.md`
- Modify: `docs/codex/CI_QUALITY_GATE_MATRIX.md`
- Modify: `README.md`

- [x] **Step 1: 把旧的“无内建 Provider”断言改成 Conditional 能力**

文档明确两者实现 `ReliableMQProvider`、publish 等待 Broker confirm/ACK、at-least-once 不等于 exactly-once、不支持 `OrderedReliableMQProvider`、业务仍需 EventID/Inbox 幂等，RocketMQ 保持 factory 扩展。

- [x] **Step 2: 运行过期文案扫描**

Run: `rtk rg -n 'Kafka/RabbitMQ.*无内建|provider .*not implemented|CORE_TEST_KAFKA.*不得' README.md docs pkg/server tests --glob '!docs/superpowers/specs/**' --glob '!docs/superpowers/plans/**'`

Expected: 无与当前能力冲突的现行文案。

- [x] **Step 3: 运行文档与日志门禁**

Run: `rtk ./scripts/check-logging.sh`

Run: `rtk ./scripts/test.sh api-compat`

Run: `rtk ./scripts/test.sh release-contract`

Expected: PASS。

### Task 8: 最终验证与交付审计

**Files:**
- Verify only: all changed files

- [x] **Step 1: 格式化并检查差异范围**

Run: `rtk gofmt -w pkg/server/config/mqconfig.go pkg/server/config/mqconfig_test.go pkg/server/mq/provider_*.go pkg/server/mq/factory.go pkg/server/mq/factory_test.go tests/integration/mq_provider_test.go`

Run: `rtk git diff --check`

Expected: 无格式或空白错误。

- [x] **Step 2: 运行定向 race 与全量 CI gate**

Run: `rtk go test -race ./pkg/server/mq ./pkg/server/config ./pkg/server/event -count=1`

Run: `rtk ./scripts/ci.sh required/quick`

Run: `rtk ./scripts/ci.sh required/race`

Expected: PASS。

- [x] **Step 3: 汇总真实外部测试状态**

记录 Kafka、RabbitMQ、EventBridge 外部门禁是 PASS、FAIL 或 `NOT RUN`，并单独说明 Docker/镜像/网络阻塞；不能用单元测试替代真实 Broker 验收。
