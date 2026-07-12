# Core 项目审查行动计划

> **面向智能体开发者：** 必须使用 `superpowers:executing-plans` 子技能，按任务逐项实施本计划。步骤使用复选框（`- [ ]`）语法跟踪。

**目标：** 将项目审查建议转化为可执行、可验证的工作：优先复用成熟的 go-zero 能力，删除无用或重复的框架代码，统一生产日志和异常归属，保护请求与配置边界，隔离依赖变更，提供可重复的集成依赖环境，建立兼容性与 CI 门禁，并对齐测试和文档。

**架构：** 将本仓库定位为 go-zero 及其他已选成熟库之上的轻量应用组装层。仅保留 Digitalway 路由/模型约定、MachineID 隔离、Provider 切换和跨节点通知等领域特定契约；通过成熟基础设施上的轻量适配器进行组装，不重复实现客户端、缓存、发现循环、重试、并发原语或日志框架。保持请求状态隔离，使配置声明与运行时行为一致，明确子系统生命周期归属，使用带跟踪上下文的 go-zero 结构化日志，仅在责任边界记录一次错误，并使快速单元测试不依赖 Docker。

**技术栈：** Go 1.26 module、go-zero v1.10.2、`go test`、`go vet`、race detector、Docker Compose、etcd、Consul、Redis Streams、NATS JetStream、Kafka 兼容 Broker 占位、MySQL、MongoDB、ClickHouse，以及现有的 `tests/integration` build tag。

---

## 当前基线

| 领域 | 当前状态 | 证据 / 命令 |
| --- | --- | --- |
| Server vet | 通过 | `go vet ./pkg/server/...` |
| 全项目 vet | 失败 | `go vet ./...` 报告 `pkg/persistence/database/nosql/mongo.go` 中存在未使用字段名的 `bson.E` 值 |
| Server 测试 | 允许本地端口绑定时通过 | `go test ./pkg/server/... -count=1` |
| 快速工具/manage 测试 | 通过 | `go test ./service/manage ./pkg/utils ./pkg/persistence/types -count=1` |
| 持久化测试 | 失败，与环境耦合，且结束缓慢 | `go test ./pkg/persistence/... -count=1` 的默认延迟/同步正确性测试失败，会隐式连接本地 MySQL，并可能在失败后留下重试或 worker |
| 竞态检查 | 未通过 | 定向 `-race` 运行暴露了异步 WebSocket 订阅回调的同步契约，该契约既未记录，也未被安全断言 |
| 集成测试 | 已正确受 build tag 控制 | 未提供 `-tags=integration` 时，`go test ./tests/integration` 报告构建约束排除了文件 |
| 外部依赖测试 | 部分实现 | 已有 etcd、Consul、Redis Streams、NATS JetStream；Kafka/RabbitMQ/RocketMQ 仅完成配置校验，未实现 Provider |
| go-zero 复用 | 部分且不一致 | 现有代码使用 `logx`、`httpx`、`conf` 和 `rest`；go-zero v1.10.2 还提供未使用的 `discov`、`stores/redis`、`stores/cache`、`mr`、`fx`、`threading`、`syncx` 和 `zrpc` 等候选能力 |
| 无用/未完成代码 | 已确认 | `CacheAdapter.getCacheDB()` 返回 `(nil, nil)`，Mongo 包含 `panic("implement me")`，两个 SQLite 注册表重复持有责任，运行时包中存在调试 `fmt.Print*` 调用 |
| 日志 | 不一致且存在安全风险 | 运行时代码混用 `fmt`、标准 `log` 和 `logx`；级别与语言不统一；部分日志包含完整 payload/response/SQL；TraceID 已存在但很少绑定到日志上下文 |
| 安全默认值 | 需加固 | 配置迁移使用过宽的文件权限，CORS 默认过宽，认证配置使用包全局变量，且在没有代理策略时信任转发的客户端 IP 请求头 |
| 配置契约 | 不完整 | 多个已接受的 MQ 和集群字段尚无已确认的运行时消费方或行为测试 |
| CI/发布治理 | 缺失 | 没有仓库 workflow、必需质量门禁、导出 API 兼容性检查、changelog 或发布策略 |
| 依赖升级状态 | 不干净 | `go.mod` 和 `go.sum` 在本地已修改，应作为独立的依赖升级任务处理 |

## 工程决策规则

添加或替换框架代码前，应用以下规则：

1. **go-zero 优先：** 创建基础设施辅助程序前，先检查已锁定的 go-zero 版本及其本地源码。
2. **仅使用轻量适配器：** Digitalway 公共接口能提供领域价值时予以保留，但连接管理、重试、缓存行为、发现、日志和生命周期处理应委托给成熟库。
3. **不混淆抽象：** go-zero `core/queue` 是进程内生产者/消费者队列，不是 Kafka/NATS/Redis Streams Broker 实现。Broker Provider 需要经过验证的客户端，或单独版本化的 go-zero 队列生态。
4. **保护领域语义：** 在契约测试证明 MachineID 隔离、心跳/Watch 行为、故障转移、确认、健康检查和 Provider 切换均保持不变前，不替换集群或 MQ 抽象。
5. **有证据才删除：** 将候选项分类为 `remove`、`replace`、`keep-domain` 或 `unsupported`；不得仅根据文本搜索删除已导出或运行时可达的代码。
6. **每个提交只做一类迁移：** 分离缓存、发现、并发、传输和无用代码变更，使每个提交都可审查、可回滚。
7. **记录事件，而非叙述性文案：** 通过 go-zero `logx` 使用稳定的 ASCII `snake_case` 事件名和结构化字段；不添加第二层日志门面。
8. **每个错误只记录一次：** 下层封装并返回错误；由负责停止、重试、降级或响应的边界记录事件。除非函数拥有回退或终止决策，否则不得同时记录并返回同一错误。
9. **不记录敏感遥测数据：** 不得记录 token、凭据、TOTP 密钥/验证码、完整请求/响应体、完整 payload、DSN 或带值的原始 SQL。
10. **默认安全：** 密钥使用最小权限存储，网络信任必须显式，缺少安全配置时必须 fail closed，不得扩大访问权限。
11. **配置必须真实：** 每个已接受字段都必须有运行时消费方和行为测试；不支持的值应校验失败，或通过有文档的废弃流程移除。
12. **仅使用请求本地状态：** 请求、身份、跟踪和可变操作数据不得存储在共享服务单例上，也不得通过可变全局注册表暴露。
13. **兼容性是发布契约：** 公共错误、路由、导出的 Go API、配置和消费方行为都需要兼容性证据或有文档的迁移路径。

## 计划拆分

本文件是总索引、依赖顺序和完成台账。实施任务 11-17 前，必须在 `docs/codex/plans/` 下创建对应的聚焦计划，并在其中保存代码级步骤、测试用例、发布说明和已接受的权衡：

| 任务 | 必需的实施计划 |
| --- | --- |
| 11 | `docs/codex/plans/11-security-auth-isolation.md` |
| 12 | `docs/codex/plans/12-request-lifecycle-concurrency.md` |
| 13 | `docs/codex/plans/13-persistence-correctness.md` |
| 14 | `docs/codex/plans/14-config-runtime-contract.md` |
| 15 | `docs/codex/plans/15-api-release-governance.md` |
| 16 | `docs/codex/plans/16-ci-quality-gates.md` |
| 17 | `docs/codex/plans/17-performance-slo-baseline.md` |

任务 6-9 也必须在已接受的迁移修改代码前创建聚焦子计划。总计划只记录结果与证据，不应膨胀为第二份实施规格。

## 完成跟踪

每个任务完成后更新此表。只有当“完成证据”中的命令通过且已记录提交哈希时，任务才算完成。

| 任务 | 状态 | 提交 | 完成证据 |
| --- | --- | --- | --- |
| 1. 依赖升级隔离 | 已完成 | `f72447f` | `go mod verify`、`go mod tidy -diff`、server vet 和定向兼容性测试通过；依赖文件已单独提交 |
| 2. Docker Compose 集成环境 | 未开始 |  | 默认 profile 中 etcd/consul/redis/nats 健康；显式请求 `--profile kafka` 时 Kafka 健康 |
| 3. 测试命令脚本 | 已完成 | `0d29df1` | `bash -n`、`quick` 和 `server` 通过；未知模式输出用法并代码 2 退出，脚本不依赖 `rtk` |
| 4. 通过 Docker 运行外部集成测试 | 未开始 |  | `./scripts/test.sh integration-external` 通过 etcd/consul/redis/nats 测试套件 |
| 5. Kafka Provider 缺口决策 | 未开始 |  | 实现 Provider 测试，或在文档中明确将 Kafka 标记为仅配置 |
| 6. go-zero 能力与复用审计 | 未开始 |  | `docs/codex/GO_ZERO_REUSE_AUDIT.md` 记录每个已审查子系统的证据和 keep/replace/remove 决策 |
| 7. 无用与未完成代码清理 | 未开始 |  | 已启用的运行时路径不包含已知占位实现；每个删除/替换都有定向测试 |
| 8. 全局日志与异常审计 | 未开始 |  | 运行时日志使用 `logx` 结构化事件，在请求/跨服务边界携带跟踪上下文，通过敏感数据扫描，且不包含未批准的控制台/fatal 输出 |
| 9. 架构加固待办 | 未开始 |  | 问题均已修复，或已转换为包含文件路径和测试命令的跟踪文档 |
| 10. README/文档与场景使用指南 | 未开始 |  | README、skill 参考和场景指南对路由、模型、成熟度、日志和复用策略的描述一致 |
| 11. 安全基线与认证隔离 | 已完成（包含审查后 A-E） | `804a2de`, `937d381`, `daa2c57`, `5e4bcd8`, `503a01d`, `0bc1a14`, `3f4f506`, `e320017`, `6dd5f89`, `219da16`, `307f44e` | 代理/本地访问伪造防护、Logto 身份与 JWKS 生命周期、nil Request 处理、显式 CORS 示例、TrustedProxies 指南和可执行 security 测试模式均通过 |
| 12. 请求隔离、全局状态与生命周期 | 已完成 | `60b6e3a`, `fc42ae7`, `52ac181`, `87cc800`, `b816515`, `ffe27c8`, `f016173`, `8aeed28`, `2f70294`, `f0f70ae` | 请求/注册表隔离、幂等可等待关闭、Provider 持续对账、WebSocket worker 归属和 concurrency 门禁均已通过 |
| 13. 持久化正确性与外部测试分离 | 已完成 | `b144f9a`, `aa6c2ad`, `e8330c0`, `adbd803`，以及本次 13.4 提交 | 默认/外部套件分层，GORM result 错误传播、SharedBadger CAS/pending/fatal-break 语义和 Docker 持久化 driver 契约均已通过；容器、测试进程与锁具有有界清理 |
| 14. 配置到运行时能力契约 | 已完成并通过外部复审 | `f91c79b`, `c52e32e` | `config-contract`、config/router/cluster/transport/mq/event 全包与 race 门禁通过；外部复审结论为 APPROVED，无 P0/P1/P2 返工项 |
| 15. 公共 API 兼容性与发布治理 | 未开始 |  | 类型化错误、路由/API 快照、废弃策略、changelog 和消费方兼容性检查通过 |
| 16. CI 质量门禁与消费方兼容性矩阵 | 未开始 |  | 必需 CI 层级在干净检出上通过，并发布可操作的失败产物 |
| 17. 性能、容量与运维 SLO 基线 | 未开始 |  | 基准、预算、RED/USE 指标、跟踪和 SLO 检查均有已记录基线与责任人 |

## 任务 1：依赖升级隔离

**文件：**
- 审查：`go.mod`
- 审查：`go.sum`
- 按需修改：本任务不修改代码文件

- [x] **步骤 1：检查当前依赖偏移**

运行：

```bash
git diff --stat -- go.mod go.sum
git diff -- go.mod | sed -n '1,220p'
```

预期：仅出现依赖版本和直接/间接分类变更。

- [x] **步骤 2：确定提交边界**

如果依赖升级是有意的，仅提交 `go.mod` 和 `go.sum`：

```bash
git add go.mod go.sum
git commit -m "chore: update core dependency versions"
```

如果依赖升级不是有意的，回退前必须先询问，因为这些文件已包含用户/本地变更。

- [x] **步骤 3：验证依赖升级兼容性**

运行：

```bash
go vet ./pkg/server/...
go test ./pkg/server/... ./pkg/utils ./service/manage ./pkg/persistence/types -count=1
```

预期：两条命令均代码 0 退出。

## 任务 2：Docker Compose 集成环境

**文件：**
- 创建：`docker-compose.integration.yml`
- 创建：`.env.integration.example`
- 修改：如果需要忽略本地 Docker volume 或环境文件，修改 `.gitignore`

- [ ] **步骤 1：添加 Compose 服务**

使用以下内容创建 `docker-compose.integration.yml`。其中使用已审查的固定版本，而非 `latest`；实施任务时应验证官方镜像 manifest，并记录不可变 digest。所有未认证的集成端口只绑定主机回环地址。

```yaml
name: digitalway-core-integration

services:
  etcd:
    image: gcr.io/etcd-development/etcd:v3.6.11
    command:
      - /usr/local/bin/etcd
      - --name=core-etcd
      - --data-dir=/etcd-data
      - --listen-client-urls=http://0.0.0.0:2379
      - --advertise-client-urls=http://etcd:2379
      - --listen-peer-urls=http://0.0.0.0:2380
      - --initial-advertise-peer-urls=http://etcd:2380
      - --initial-cluster=core-etcd=http://etcd:2380
      - --initial-cluster-state=new
      - --initial-cluster-token=core-integration
    ports:
      - "127.0.0.1:2379:2379"
    healthcheck:
      test: ["CMD", "etcdctl", "--endpoints=http://127.0.0.1:2379", "endpoint", "health"]
      interval: 5s
      timeout: 3s
      retries: 20

  consul:
    image: hashicorp/consul:1.21.3
    command: ["agent", "-dev", "-client=0.0.0.0", "-log-level=warn"]
    ports:
      - "127.0.0.1:8500:8500"
    healthcheck:
      test: ["CMD", "consul", "members"]
      interval: 5s
      timeout: 3s
      retries: 20

  redis:
    image: redis:7.2-alpine
    command: ["redis-server", "--appendonly", "no"]
    ports:
      - "127.0.0.1:6379:6379"
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 20

  nats:
    image: nats:2.12.8-alpine
    command: ["-js", "-sd", "/data"]
    ports:
      - "127.0.0.1:4222:4222"
      - "127.0.0.1:8222:8222"
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://127.0.0.1:8222/healthz"]
      interval: 5s
      timeout: 3s
      retries: 20

  kafka:
    profiles: ["kafka"]
    image: apache/kafka:4.3.1
    environment:
      KAFKA_NODE_ID: "1"
      KAFKA_PROCESS_ROLES: "broker,controller"
      KAFKA_CONTROLLER_QUORUM_VOTERS: "1@kafka:9093"
      KAFKA_LISTENERS: "PLAINTEXT://:9092,CONTROLLER://:9093"
      KAFKA_ADVERTISED_LISTENERS: "PLAINTEXT://127.0.0.1:9092"
      KAFKA_CONTROLLER_LISTENER_NAMES: "CONTROLLER"
      KAFKA_INTER_BROKER_LISTENER_NAME: "PLAINTEXT"
      KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR: "1"
      KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR: "1"
      KAFKA_TRANSACTION_STATE_LOG_MIN_ISR: "1"
      KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS: "0"
    ports:
      - "127.0.0.1:9092:9092"
    healthcheck:
      test: ["CMD", "/opt/kafka/bin/kafka-broker-api-versions.sh", "--bootstrap-server", "127.0.0.1:9092"]
      interval: 10s
      timeout: 5s
      retries: 30
```

- [ ] **步骤 2：添加环境变量示例**

创建 `.env.integration.example`：

```bash
CORE_TEST_CLUSTER_LOCAL=1
CORE_TEST_ETCD=1
ETCD_ENDPOINTS=127.0.0.1:2379
CORE_TEST_CONSUL=1
CONSUL_HTTP_ADDR=127.0.0.1:8500
CORE_TEST_REDIS_STREAM=1
CORE_TEST_REDIS_ADDR=127.0.0.1:6379
CORE_TEST_NATS=1
CORE_TEST_NATS_URL=nats://127.0.0.1:4222
# 仅在任务 5 添加 Kafka Provider 和契约测试后启用：
# CORE_TEST_KAFKA=1
CORE_TEST_KAFKA_BROKERS=127.0.0.1:9092
```

- [ ] **步骤 3：验证环境启动**

运行：

```bash
docker compose -f docker-compose.integration.yml up -d
docker compose -f docker-compose.integration.yml --profile kafka up -d kafka
docker compose -f docker-compose.integration.yml --profile kafka ps
```

预期：默认 profile 中 etcd、consul、redis 和 nats 均处于健康状态；只有显式请求 Kafka profile 时，Kafka 才应处于健康状态。

## 任务 3：测试命令脚本

**文件：**
- 创建：`scripts/test.sh`
- 修改：仅当引入本地产物时修改 `.gitignore`

- [x] **步骤 1：创建脚本目录和脚本**

创建 `scripts/test.sh`：

```bash
#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

case "${1:-quick}" in
  quick)
    go vet ./pkg/server/...
    go test ./pkg/utils ./service/manage ./pkg/persistence/types -count=1
    ;;
  server)
    go vet ./pkg/server/...
    go test ./pkg/server/... -count=1
    ;;
  integration-local)
    CORE_TEST_CLUSTER_LOCAL=1 go test -tags=integration ./tests/integration -run TestClusterLocal -count=1
    ;;
  integration-external)
    CORE_TEST_ETCD=1 \
    ETCD_ENDPOINTS="${ETCD_ENDPOINTS:-127.0.0.1:2379}" \
    CORE_TEST_CONSUL=1 \
    CONSUL_HTTP_ADDR="${CONSUL_HTTP_ADDR:-127.0.0.1:8500}" \
    CORE_TEST_REDIS_STREAM=1 \
    CORE_TEST_REDIS_ADDR="${CORE_TEST_REDIS_ADDR:-127.0.0.1:6379}" \
    CORE_TEST_NATS=1 \
    CORE_TEST_NATS_URL="${CORE_TEST_NATS_URL:-nats://127.0.0.1:4222}" \
    go test -tags=integration ./tests/integration -run 'TestClusterEtcd|TestClusterConsul|TestMQ' -count=1
    ;;
  all)
    "$0" quick
    "$0" server
    "$0" integration-local
    "$0" integration-external
    ;;
  *)
    echo "usage: scripts/test.sh {quick|server|integration-local|integration-external|all}" >&2
    exit 2
    ;;
esac
```

- [x] **步骤 2：设置可执行权限**

运行：

```bash
chmod +x scripts/test.sh
```

- [x] **步骤 3：验证快速模式**

运行：

```bash
scripts/test.sh quick
scripts/test.sh server
```

预期：两条命令均代码 0 退出。

## 任务 4：通过 Docker 运行外部集成测试

**文件：**
- 修改：`docs/codex/AUTOMATED_VERIFICATION_PLAN.md`
- 读取：`tests/integration/etcd_provider_test.go`
- 读取：`tests/integration/consul_provider_test.go`
- 读取：`tests/integration/mq_provider_test.go`

- [ ] **步骤 1：启动依赖**

运行：

```bash
docker compose -f docker-compose.integration.yml up -d
```

预期：Compose 创建或复用所有服务。

- [ ] **步骤 2：运行现有外部集成测试**

运行：

```bash
./scripts/test.sh integration-external
```

预期：名为 `TestClusterEtcd*`、`TestClusterConsul*`、`TestMQRedisStream`、`TestMQNATSJetStream`、`TestMQEventStreamRedis` 和 `TestMQEventStreamNATS` 的测试通过。

- [ ] **步骤 3：记录精确的本地命令**

将以下章节追加到 `docs/codex/AUTOMATED_VERIFICATION_PLAN.md`：

```markdown
## Docker 支撑的外部依赖

启动本地依赖：

```bash
docker compose -f docker-compose.integration.yml up -d
```

运行外部集成测试：

```bash
scripts/test.sh integration-external
```

停止本地依赖：

```bash
docker compose -f docker-compose.integration.yml down
```
```

## 任务 5：Kafka Provider 缺口决策

**文件：**
- 审查：`pkg/server/config/mqconfig.go`
- 审查：`pkg/server/mq/factory.go`
- 如实施则创建：`pkg/server/mq/provider_kafka.go`
- 如实施则创建：`tests/integration/kafka_provider_test.go`
- 如推迟则修改：`docs/codex/AUTOMATED_VERIFICATION_PLAN.md`

- [ ] **步骤 1：确认当前 Kafka 行为**

运行：

```bash
rg -n 'case "kafka"|provider=kafka|CORE_TEST_KAFKA|NewKafka' pkg/server tests
```

当前预期：配置可通过 `kafka` 校验，factory 返回 Provider 未实现错误，且没有 Kafka 集成测试。

- [ ] **步骤 2A：如实现 Kafka Provider，先编写失败的集成测试**

创建 `tests/integration/kafka_provider_test.go`：

```go
//go:build integration

package integration_test

import (
	"os"
	"strings"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/mq"
)

func TestMQKafka(t *testing.T) {
	if os.Getenv("CORE_TEST_KAFKA") == "" {
		t.Skip("未设置 CORE_TEST_KAFKA")
	}
	brokers := os.Getenv("CORE_TEST_KAFKA_BROKERS")
	if brokers == "" {
		brokers = "127.0.0.1:9092"
	}
	p := mq.NewKafkaProvider(strings.Split(brokers, ","), "core-integration")
	runMQContract(t, p)
}
```

运行：

```bash
CORE_TEST_KAFKA=1 CORE_TEST_KAFKA_BROKERS=127.0.0.1:9092 go test -tags=integration ./tests/integration -run TestMQKafka -count=1
```

实现前预期：因 `mq.NewKafkaProvider` 未定义而编译失败。

- [ ] **步骤 2B：如推迟 Kafka Provider，将其记录为仅配置**

将以下说明添加到 `docs/codex/AUTOMATED_VERIFICATION_PLAN.md`：

```markdown
### Kafka 状态

`MQConfig` 校验和 Docker 集成环境中已包含 Kafka，但 `pkg/server/mq` 尚未实现 Kafka `MQProvider`。在添加 `provider_kafka.go` 和 `tests/integration/kafka_provider_test.go` 前，不得启用 `CORE_TEST_KAFKA`。
```

## 任务 6：go-zero 能力与复用审计

**文件：**
- 创建：`docs/codex/GO_ZERO_REUSE_AUDIT.md`
- 审查：`go.mod`
- 审查：`pkg/server/config/serverconfig.go`
- 审查：`pkg/server/trans/rest/server.go`
- 审查：`pkg/server/mq`
- 审查：`pkg/server/cluster`
- 审查：`pkg/persistence/adapter/cache.go`
- 审查：`pkg/persistence/database/nosql/redis.go`
- 审查：`pkg/utils/concurrency.go`
- 只读：`${GOMODCACHE}/github.com/zeromicro/go-zero@v1.10.2`

- [ ] **步骤 1：记录本仓库实际使用的 go-zero 能力面**

运行：

```bash
rg -l 'github.com/zeromicro/go-zero' pkg service examples tests --glob '*.go'
rg -n 'conf\.(MustLoad|Load)|rest\.(NewServer|MustNewServer)|zrpc\.|stores/redis|stores/cache|core/discov|core/mr|core/fx|core/threading' pkg service examples tests --glob '*.go'
go list -deps ./pkg/server/...
```

预期：审计可以区分“实际使用”和“go-zero v1.10.2 仅提供但未使用”的包。当前证据表明实际使用了 `logx`、`httpx`、`conf` 和 `rest`；尚未使用 `stores/cache`、`stores/redis`、`discov`、`mr`、`fx`、`threading` 或 `zrpc`。

- [ ] **步骤 2：创建复用决策矩阵**

使用以下初始矩阵创建 `docs/codex/GO_ZERO_REUSE_AUDIT.md`，随后为每个变更的决策添加源码链接和测试证据：

```markdown
# go-zero 复用审计

锁定依赖：`github.com/zeromicro/go-zero v1.10.2`。

| 领域 | 当前实现 | 成熟候选 | 初始决策 | 必需证据 |
| --- | --- | --- | --- | --- |
| 配置 | `serverconfig.go` 已使用 go-zero `conf` | `core/conf`、`core/configcenter` | 保留并标准化；仅在配置兼容性测试通过后删除重复解析 | 现有 JSON 迁移/默认值测试保持通过 |
| 日志与恢复 | 混用 `logx`、`fmt.Print*` 和本地 panic 恢复 | `core/logx`、`core/rescue`、`core/threading` | 统一使用 `logx` 记录运行时日志；按调用点评估恢复辅助程序 | panic/错误行为和日志字段有测试 |
| HTTP 运行时 | Digitalway REST 代码已封装 go-zero `rest.Server`；其他位置仍有 Fiber | `rest`、`zrpc` | 保留当前公共封装；在任何 Server 迁移前审计 Fiber 用途 | 路由/认证/WebSocket/OpenAPI 兼容性套件 |
| 通用 Redis KV | 本地 `nosql.Redis` 每次操作都创建并 ping 新客户端 | `core/stores/redis` | 使用共享 go-zero Redis 适配器替换客户端生命周期 | ICache 契约加 Docker Redis 测试 |
| Cache-aside | `CacheAdapter.getCacheDB()` 返回 `(nil, nil)` | cache-aside 使用 `core/stores/cache`；纯 KV 使用 `core/stores/redis` | 根据真实调用方替换或删除；不保留无功能的适配器 | 调用方清单及缓存命中/未命中/TTL 测试 |
| SQL 持久化 | Digitalway 模型契约使用 GORM | go-zero `sqlx/sqlc` | 保留 GORM；在没有可量化需求时，不同时运行两套 ORM/数据访问栈 | 现有 ModelList/manage 契约保持通过 |
| etcd 发现 | 自定义 Provider 增加 MachineID、心跳、Watch 和服务语义 | `core/discov` publisher/subscriber | 创建兼容性验证；仅当领域契约保持完整时复用 go-zero etcd 生命周期 | 集群 Provider 契约和 Docker etcd 测试 |
| Consul 发现 | 自定义 Provider | 锁定的 go-zero core 中无等价能力 | 保留在通用 Provider 接口之后 | Consul 契约测试 |
| MQ 抽象 | `MQProvider`、切换、EventBridge、Redis Streams、NATS JetStream | 成熟 Broker 客户端；经批准时使用独立的 go-zero 队列生态 | 保留领域抽象；仅简化 Provider 内部实现 | 发布/订阅/确认/健康/切换/回滚测试 |
| 进程内队列 | 适用位置的本地框架代码 | go-zero `core/queue` | 仅用于进程本地生产者/消费者流水线，绝不作为 Broker 替代 | 关闭/背压测试 |
| 并发辅助程序 | `ConcurrencyTasks` 保留有序结果并聚合错误 | `core/mr`、`core/fx`、`core/threading`、`core/syncx` | 对比契约；仅在有序结果、限制、取消和 panic 行为匹配时替换 | 现有工具测试加取消/竞态测试 |
| 重试/超时/生命周期 | Provider 和持久化中的临时循环 | `core/fx`、`core/service`、`core/proc`、breaker 辅助程序 | 新代码优先使用 go-zero 原语；每次只迁移一个子系统 | 确定性重试/关闭测试 |
```

- [ ] **步骤 3：将每个 `replace` 决策转换为独立迁移计划**

每个已接受的替换都创建一个实施分支。首个建议切片是 Redis KV/cache，因为当前适配器尚未完成，且本地 Redis 封装每次操作都会重新连接。不得将其与集群、MQ 或 HTTP 迁移合并。

- [ ] **步骤 4：验证复用不会削弱 Digitalway 契约**

每次迁移后运行：

```bash
go test ./pkg/persistence/... ./pkg/server/... ./service/manage/... -count=1
go test -race ./pkg/utils ./pkg/server/cluster ./pkg/server/mq -count=1
```

预期：所有现有行为契约通过；如果替换改变了公共路由、模型行为、MachineID 隔离、Broker 确认或 Provider 切换，则拒绝或修订该替换。

## 任务 7：无用与未完成代码清理

**文件：**
- 创建：`docs/codex/DEAD_CODE_AUDIT.md`
- 审查：`pkg/persistence/adapter/cache.go`
- 审查：`pkg/persistence/adapter/nosql.go`
- 审查：`pkg/persistence/database/nosql/mongo.go`
- 审查：`pkg/persistence/entity/modellist.go`
- 审查：`pkg/persistence/adapter/default.go`
- 审查：`pkg/server/safe/twosteps/google.go`
- 审查：`pkg/server/trans/quic`
- 审查：`pkg/server` 和 `pkg/utils` 下的运行时 `fmt.Print*` 调用

- [ ] **步骤 1：创建分类清理台账**

使用以下已确认候选项创建 `docs/codex/DEAD_CODE_AUDIT.md`：

```markdown
# 无用与未完成代码审计

| 候选项 | 风险 | 初始分类 | 退出条件 |
| --- | --- | --- | --- |
| `pkg/persistence/adapter/cache.go` | `getCacheDB()` 返回 `(nil, nil)`，公共方法可能解引用 nil | 替换或删除 | 识别真实调用方；在 go-zero Redis/cache 上实现契约，或删除已导出适配器 |
| `pkg/persistence/adapter/nosql.go` | 大量已注释实现遮蔽了受支持的持久化路径 | 删除 | 没有活跃引用需要该注释代码；历史仍可从 Git 获取 |
| `pkg/persistence/database/nosql/mongo.go` | 已启用方法包含 `panic("implement me")`，另一方法会打印占位结果 | 实现或标记为不支持 | 已启用方法返回正确结果或明确错误；不再存在占位 panic |
| `entity/modellist.go` 和 `adapter/default.go` 中的 SQLite 注册表 | 两个全局 map 可能为同一数据库创建不同实例 | 合并 | 只有一个并发安全的 owner，且测试证明实例复用 |
| `pkg/server/safe/twosteps/google.go` 调试输出 | 可能打印 TOTP 密钥和验证码 | 立即删除 | 库运行时路径无密钥/验证码输出；行为测试通过 |
| 运行时 `fmt.Print*` 调用 | 绕过结构化日志且可能暴露数据 | 转入任务 8 日志审计 | 运行时包使用结构化 `logx`；仅保留明确的 CLI/示例输出 |
| QUIC stub/旧传输代码 | 可能不可达、未完成，或仅由 build tag 选中 | 删除前验证 | build-tag 矩阵和 factory 引用证明保留/删除决策 |
```

- [ ] **步骤 2：删除已导出代码前验证可达性**

运行：

```bash
rg -n 'CacheAdapter|NewRedis\(|Mongo|globalSqliteInstances|twosteps|TransportQUIC|quic' . --glob '*.go'
rg -n 'implement me|fmt\.(Print|Printf|Println)' pkg service --glob '*.go'
go list ./...
```

预期：每个候选项都已记录调用方、build tag 和公共暴露面。测试、示例、生成文件和运行时文件分开分类。

- [ ] **步骤 3：在清理台账中验证任务 11 的密钥输出修复**

任务 11 负责从 `pkg/server/safe/twosteps/google.go` 中删除 TOTP 密钥/验证码输出及其行为测试。任务 7 记录结果，并验证无用或占位路径不会重新引入该输出：

```bash
go test ./pkg/server/safe/... -count=1
rg -n 'fmt\.(Print|Printf|Println).*secret|fmt\.(Print|Printf|Println).*code' pkg/server/safe --glob '*.go'
```

预期：测试通过，第二条命令不返回任何运行时密钥/验证码打印。

- [ ] **步骤 4：测试先行替换或删除损坏的缓存路径**

添加 `ICache` 契约测试，覆盖 get、set、delete、TTL、scan/search 行为、Redis 不可用和客户端复用。使用 go-zero `core/stores/redis` 实现；仅对需要抑制未命中的 cache-aside 行为使用 `core/stores/cache`。如果全仓库调用方清单为空，删除 `CacheAdapter`。

运行：

```bash
go test ./pkg/persistence/adapter ./pkg/persistence/database/nosql -count=1
CORE_TEST_REDIS_STREAM=1 CORE_TEST_REDIS_ADDR=127.0.0.1:6379 go test -tags=integration ./tests/integration -count=1
```

预期：单元测试在无 Docker 时通过；Compose 运行时，显式启用的 Redis 契约测试通过。

- [ ] **步骤 5：独立合并 SQLite 所有权**

将实例创建移到一个由 ModelList 和 adapter 共用的并发安全注册表。添加并行测试，断言相同逻辑数据库名返回同一实例，并使用 `-race` 运行。

```bash
go test -race ./pkg/persistence/entity ./pkg/persistence/adapter -count=1
```

预期：测试通过，无竞态报告，且仅保留一个 `globalSqliteInstances` owner。

- [ ] **步骤 6：解决未完成的 Mongo 和旧 NoSQL 代码**

对每个可通过受支持配置达到的方法，在完整实现和集成测试存在前返回明确的类型化错误；不得留下运行时占位 panic。排除活跃引用后，删除已注释实现。

```bash
go test ./pkg/persistence/database/nosql ./pkg/persistence/adapter -count=1
rg -n 'panic\("implement me"\)|mongo implement|TODO implement me' pkg/persistence --glob '*.go'
```

预期：测试通过，扫描在已启用运行时路径上不返回占位实现。

## 任务 8：全局日志与异常审计

**文件：**
- 创建：`docs/codex/LOGGING_AUDIT_AND_STANDARD.md`
- 创建：`scripts/check-logging.sh`
- 修改：`pkg/server/router/request.go`
- 修改：`pkg/server/router/servicecontext.go`
- 修改：`pkg/server/trans/rest/server.go`
- 修改：`pkg/server/safe/logto/authmiddleware.go`
- 修改：`pkg/server/safe/twosteps/google.go`
- 分批审查并修改：`pkg/server/cluster`、`pkg/server/mq`、`pkg/server/event`、`pkg/server/transport`、`pkg/server/trans`、`pkg/persistence`、`pkg/utils`、`service/manage`
- 测试：在每个已修改包旁添加定向测试

- [ ] **步骤 1：生成完整的运行时日志清单**

运行以下扫描，并将每个活跃运行时发现复制到 `docs/codex/LOGGING_AUDIT_AND_STANDARD.md`。将示例、测试、注释和生成文件分开分类；仅 `pkg` 和 `service` 属于生产库范围。

```bash
rg -n 'fmt\.(Print|Printf|Println)|log\.(Print|Printf|Println|Fatal|Fatalf|Panic|Panicf)' pkg service --glob '*.go'
rg -n 'logx\.(Debug|Debugf|Info|Infof|Error|Errorf|Severe|Severef|Slow|Slowf|Infow|Errorw|Debugw|Sloww)' pkg service --glob '*.go'
rg -n 'logx\..*(payload|request|response|body|token|password|passwd|secret|authorization|cookie|sql)|fmt\..*(token|password|passwd|secret|authorization|cookie)' pkg service --glob '*.go' -i
rg -n 'logx\.(Error|Errorf).*(retry|fallback|degrad|skip|降级|跳过|重试)' pkg service --glob '*.go' -i
rg -n 'request.?id|trace.?id|span.?id|x-request-id|TraceID|RequestID' pkg service --glob '*.go' -i
```

预期：台账记录文件、行号、当前级别、事件用途、敏感数据风险、重复错误风险、目标操作和验证命令。已确认的初始发现包括标准控制台输出、库构造器中的 `log.Fatalf`、完整 payload/response/SQL 日志、将可恢复回退记为错误、装饰性横幅/图标，以及 TraceID 已传播但未一致绑定日志。

- [ ] **步骤 2：建立基于 go-zero `logx` 的唯一日志契约**

在 `docs/codex/LOGGING_AUDIT_AND_STANDARD.md` 中创建以下规范性章节：

```markdown
# 日志审计与规范

## 运行时契约

- 使用 go-zero `logx`；不引入其他日志门面。
- 事件名使用稳定的 ASCII `snake_case`，例如 `service_started`、`transport_fallback` 和 `mq_publish_failed`。
- 通过 `logx.Infow`、`Errorw`、`Debugw`、`Sloww`、`Field` 和 `ContextWithFields` 使用结构化字段。
- 运行时事件文本使用简洁英文。面向用户的校验错误可保留本地化，因为它们是 API 内容，而非日志事件名。
- 可用时必需的上下文：`service`、`trace_id`、`route`、`method`、`operation`、`provider`、`node_id`、`attempt`、`duration_ms` 和 `error`。
- 不得记录完整 payload、body、response、token、凭据、cookie、TOTP 值、DSN 或包含值的原始 SQL。

## 级别

| API | 用途 |
| --- | --- |
| `Errorw` | 最终操作失败、不变式破坏、数据丢失风险、panic 恢复，或无成功回退的依赖失败 |
| `Infow` | 服务生命周期、Provider 切换、成功恢复，或运维人员应知的已处理降级/回退 |
| `Debugw` | 每次尝试的重试、路由注册细节、worker 生命周期、缓存细节和其他高频诊断 |
| `Sloww` | 已测量操作超过配置的延迟阈值 |
| `Severe` | 仅限进程启动边界；绝不从可复用库包中终止进程 |

## 错误归属

1. 下层使用 `%w` 添加操作上下文并返回错误。
2. 执行重试的层以 debug 级别记录每次尝试。
3. 如果回退成功，记录一条描述降级的 info 事件。
4. 如果所有恢复均失败，由边界记录一条 error 事件并返回或响应。
5. 不得在每一层都记录并返回同一个未变错误。

## 删除或降级

- 删除分隔符、图标、成功口号、对象 dump 和重复堆栈跟踪。
- 将每 worker、每路由、每记录和每次重试消息降为 debug，除非其表示最终损失。
- 当要回答的问题是聚合速率、延迟、队列深度、内存或连接数时，用指标代替重复状态日志。
```

- [ ] **步骤 3：添加静态日志守卫**

创建 `scripts/check-logging.sh`：

```bash
#!/usr/bin/env bash
set -euo pipefail

failed=0

check_forbidden() {
  local description="$1"
  local pattern="$2"
  if rg -n "$pattern" pkg service --glob '*.go' --glob '!**/*_test.go'; then
    echo "forbidden runtime logging: $description" >&2
    failed=1
  fi
}

check_forbidden "console or process-terminating logger" 'fmt\.(Print|Printf|Println)|log\.(Print|Printf|Println|Fatal|Fatalf|Panic|Panicf)'
check_forbidden "decorative log output" 'logx\..*[🚀✅⚠️❌🛑📊🆕🔗║╚]'
check_forbidden "sensitive value in log expression" '(logx\.|fmt\.|log\.)(.*)(token|password|passwd|secret|authorization|cookie|totp)'

exit "$failed"
```

将 `./scripts/check-logging.sh` 加入 `quick` 测试层级。迁移期间，在审计文档中记录范围严格的临时例外、owner 和删除任务；不得在全局放宽模式。

- [ ] **步骤 4：完成归属任务 11 的 P0 日志工作**

任务 11 负责认证状态、构造器签名、客户端响应、CORS/代理策略和密钥删除。任务 8 负责日志契约，并在同一 P0 安全分支中验证以下变更：

1. 从 `pkg/server/safe/twosteps/google.go` 中删除所有 TOTP 密钥、验证码、QR payload 和验证结果打印。
2. 将 Logto 中间件中的标准 `log.Printf` 替换为结构化 `logx` 事件，且绝不包含 token 或 claims body。
3. 将 `AuthHandler` 中的 `log.Fatalf` 替换为返回错误的构造器，并通过 REST Server 注册将错误传播到服务启动边界。
4. 向客户端返回通用认证响应，日志仅记录错误类别、issuer host、路由和 TraceID。

使用任务 11 子计划选定并经兼容性检查的构造器契约；预期方向为：

```go
func NewAuthHandler(
    next http.HandlerFunc,
    issuer string,
    expectedAudience string,
) (http.Handler, error)
```

运行：

```bash
go test ./pkg/server/safe/... ./pkg/server/trans/rest -count=1
rg -n 'fmt\.(Print|Printf|Println)|log\.(Print|Printf|Println|Fatal|Fatalf)' pkg/server/safe --glob '*.go'
```

预期：测试通过；`pkg/server/safe` 中不再存在包含密钥或终止进程的运行时日志。

- [ ] **步骤 5：在请求和跨服务边界绑定 TraceID 与稳定字段**

使用现有 `Request.traceID`、`PayLoad.TraceID`、OpenTelemetry context 和 go-zero 上下文字段。不创建自定义 logger 类型。

```go
ctx := logx.ContextWithFields(r.Context(),
    logx.Field("trace_id", req.GetTraceId()),
    logx.Field("service", req.ServiceName()),
    logx.Field("route", req.GetPath()),
)
logger := logx.WithContext(ctx)
logger.Errorw("request_failed",
    logx.Field("operation", "router_do"),
    logx.Field("error", err),
)
```

仅对失败或慢请求在 HTTP 边界添加一条请求完成事件；成功请求量和延迟应记入现有路由指标/统计，而非每个请求记录一条 info。在 HTTP、gRPC、EventBridge、MQ envelope 和跨节点调用中传播同一 TraceID。

测试必须证明传入的 `X-Trace-Id` 出现在捕获的结构化错误事件中，且出站传输保持同一值。

- [ ] **步骤 6：分四个可审查批次统一级别和异常归属**

应用以下映射：

| 批次 | 包 | 必需变更 |
| --- | --- | --- |
| A | `router`、`run`、`trans/rest`、`safe` | 删除横幅和每次认证成功噪声；只记录一次启动摘要；请求终止失败只记录一次；路由注册细节改为 debug |
| B | `cluster`、`mq`、`event`、`transport` | 重试尝试改为 debug；成功回退/切换改为 info；恢复耗尽和回滚失败改为 error；添加 provider/node/attempt 字段 |
| C | `persistence`、`utils` | 停止 dump 原始 SQL、DSN、对象和记录；删除记录后又返回的重复；生命周期恢复为 info，最终损坏/数据丢失风险为 error |
| D | WebSocket 和通知包 | Worker 启动/停止改为 debug；队列丢弃、panic、不健康跳过和关闭超时保持 error，并携带 route/shard/drop-count 字段 |

每个批次独立提交并验证。不得将持久化日志变更与传输或认证变更混合。

- [ ] **步骤 7：添加缺失的高价值事件并删除低价值事件**

必需事件：

| 边界 | 必需事件 |
| --- | --- |
| 服务启动/关闭 | `service_starting`、`service_started`、`service_start_failed`、`service_stopped`，携带 service、mode、port 和 duration |
| 请求边界 | `request_failed` 和 `request_slow`，携带 trace、路由模板、method、status class、duration 和 error class |
| 集群 | `cluster_provider_ready`、`cluster_degraded`、`cluster_switch_started/completed/rolled_back`、最终心跳/Watch 失败 |
| 传输 | `transport_retry`、`transport_fallback`、`transport_send_failed`；绝不记录完整 payload 或 response |
| MQ/EventBridge | Provider 连接/切换/关闭、订阅失败、发布终止失败、消费者 panic；绝不记录每条成功消息 |
| 持久化 | 连接/恢复/迁移结果和终止同步失败；绝不记录每次 CRUD 成功或 SQL 值字符串 |
| WebSocket | 队列丢弃、消费者 panic、分片初始化失败和关闭超时；worker 生命周期保持 debug |

删除无法回答运维问题的日志：分隔符、装饰性状态图案、每记录常规成功、完整对象 dump、重复堆栈跟踪，以及缺少 service/operation 上下文的消息。

- [ ] **步骤 8：验证实际日志输出并防止回归**

使用临时 go-zero writer 添加定向测试。解析发出的 JSON，并断言稳定事件名、级别、`trace_id`、service/route/provider 字段和密钥 fixture 缺失。触发一次最终失败和一次成功回退；验证失败只记录一次，回退记为 info 而非 error。

运行：

```bash
scripts/check-logging.sh
go vet ./pkg/server/... ./pkg/persistence/... ./service/...
go test ./pkg/server/... ./pkg/persistence/... ./pkg/utils ./service/manage/... -count=1
go test -race ./pkg/server/router ./pkg/server/cluster ./pkg/server/mq ./pkg/server/types -count=1
```

预期：所有命令均代码 0 退出；可通过 TraceID 找到人工触发的失败，且不暴露请求体、凭据、token、SQL 值或 TOTP 数据。

## 任务 9：架构加固待办

**文件：**
- 修改：`docs/codex/CORE_RELEASE_READINESS_PLAN.md`
- 审查：`pkg/server/router/servicecontext.go`
- 审查：`pkg/server/cluster/event.go`
- 审查：`pkg/server/config/serverconfig.go`
- 审查：`pkg/persistence/entity/modellist.go`
- 审查：`pkg/persistence/adapter/default.go`

- [ ] **步骤 1：添加加固检查清单**

追加到 `docs/codex/CORE_RELEASE_READINESS_PLAN.md`：

```markdown
## 架构加固待办

- [ ] 使用互斥锁保护 `pkg/server/router/servicecontext.go` 的全局 `scontext` map，或将其替换为注册表类型。
- [ ] 决定 `types.SetCrossNodeForwarder` 应为进程全局，还是按服务名键控。
- [ ] 在 `pkg/server/cluster/event.go` 中使用 `net.JoinHostPort`，并将非 2xx HTTP 响应视为转发错误。
- [ ] 在 `pkg/server/config/serverconfig.go` 中记录配置迁移写入失败，携带配置路径和字段上下文。
- [ ] 按同步队列、批量写入、自愈和查询/缓存责任拆分 `pkg/persistence/database/nosql/sharedbadger.go`。
```

- [ ] **步骤 2：将每个选中项转换为独立实施分支**

每个项目创建一个小分支并包含定向测试。不得将 sharedbadger 拆分与运行时集群变更合并。

## 任务 10：README 与 API 文档对齐

**文件：**
- 修改：`README.md`
- 修改：`docs/codex/AUTOMATED_VERIFICATION_PLAN.md`
- 创建：`docs/codex/FRAMEWORK_USAGE_GUIDE.md`
- 审查：`.codex/skills/use-digitalway-core/references/core-backend-api.md`
- 审查：`examples/01-hello-router`
- 审查：`examples/03-manage-crud`
- 审查：`examples/12-mq-event-stream`

- [ ] **步骤 1：替换过时 README 片段**

更新 README 示例，使其符合以下当前规则：

```text
普通 public/private 路径: /api/{service}/{structLower}
private 身份读取: req.GetUser()
manage CRUD 路径: /api/manage/{service}/{manageStructLower}/{operationLower}
ModelList 初始化：每个嵌入 entity.Model 或 entity.BaseModel 的模型都必须实现 NewModel()
```

- [ ] **步骤 2：将示例链接到验证命令**

添加以下 README 章节：

````markdown
## 本地验证

快速检查：

```bash
./scripts/test.sh quick
```

Server 检查：

```bash
./scripts/test.sh server
```

Docker 支撑的集成检查：

```bash
docker compose -f docker-compose.integration.yml up -d
./scripts/test.sh integration-external
```
````

- [ ] **步骤 3：记录框架复用边界**

向 `README.md` 和 core skill 参考添加简短架构章节，说明：

```markdown
## 框架复用策略

Digitalway Core 组装 go-zero 和其他成熟库。新增基础设施代码必须先检查已锁定 go-zero 版本的能力。Digitalway 自有抽象应保持轻量，仅当其能保护公共 API 兼容性，或路由/模型约定、MachineID 隔离、跨节点通知和 Provider 切换等领域行为时才有存在理由。

go-zero `core/queue` 是进程本地队列，不能替代 Redis Streams、NATS JetStream 或 Kafka Provider。Broker 集成必须在现有 Provider 契约之后使用持续维护的 Broker 客户端。
```

- [ ] **步骤 4：向框架消费方发布日志契约**

更新 `README.md` 和 `.codex/skills/use-digitalway-core/references/core-backend-api.md`，链接 `docs/codex/LOGGING_AUDIT_AND_STANDARD.md` 并说明：

```markdown
## 日志规则

- 使用具有稳定 `snake_case` 名称的 go-zero 结构化 `logx` 事件。
- 可用时附加 TraceID 和 service/route/provider 上下文。
- 仅在处理、降级或终止操作的边界记录一次错误。
- 绝不记录 token、凭据、TOTP 值、完整 payload/body/response、DSN 或 SQL 值。
- 重试和单项细节使用 debug，生命周期或成功回退使用 info，终止失败使用 error，测量延迟超阈值使用 slow 日志。
```

- [ ] **步骤 5：发布基于场景的框架使用指南**

创建 `docs/codex/FRAMEWORK_USAGE_GUIDE.md` 作为框架消费方的决策入口。覆盖 public/private API、Manage CRUD 与 hook、模型选择与分页、WebSocket 通知、跨节点通知、EventBridge/MQ、集群 Provider、传输选择、cache/Redis、配置、测试和扩展边界。

每项能力都应包含：

```text
场景 -> 推荐的框架 API -> 最近示例 -> 必需配置 -> 测试命令 -> 成熟度
```

仅使用以下成熟度标签：

- `Stable`：当前生产构造器和测试已确认该路径。
- `Conditional`：仅在显式配置或具备外部依赖时支持。
- `Experimental`：API 已存在，但启动、生命周期、兼容性或生产证据不完整。
- `Unsupported`：配置或旧代码可能提及，但运行时使用必须明确失败。

添加简短的反模式章节，覆盖共享请求状态、绕过 ModelList/service 封装、重复实现基础设施、静默接受配置、日志中的密钥，以及单元测试中对外部服务的假设。从 README 和 `use-digitalway-core` skill 参考链接此指南。

**验收：** 每个已记录场景都指向真实示例/测试，并与 `CONFIG_RUNTIME_CAPABILITY_MATRIX.md` 一致；不得仅因为存在配置字段或接口就将能力标记为 `Stable`。

## 任务 11：安全基线与认证隔离

**优先级：** P0

**文件：**
- 创建：`docs/codex/plans/11-security-auth-isolation.md`
- 修改：`pkg/server/config/serverconfig.go`
- 修改：`pkg/server/trans/rest/server.go`
- 修改：`pkg/server/safe/logto/authmiddleware.go`
- 修改：`pkg/server` 下的客户端 IP 和请求边界辅助程序

- [x] **步骤 1：记录信任边界威胁模型**

记录静态密钥、JWT issuer/audience 归属、浏览器 origin、受信反向代理、body 大小限制、公共错误暴露和滥用控制。包含当前证据：过宽的配置文件模式、宽泛 CORS 回退、包全局认证设置，以及无条件信任转发 IP。

- [x] **步骤 2：添加失败的安全回归测试**

覆盖以 `0600` 写入的配置文件、两个具有不同 issuer/audience 的并发认证 handler、拒绝未批准 origin、来自不受信 peer 的伪造转发头、请求大小限制、通用客户端错误和安全响应头。测试必须证明 manage 和 user 认证不会相互覆盖策略。

- [x] **步骤 3：使默认值显式且 fail closed**

将认证策略移入每个 handler 的不可变配置；开发环境之外必须显式配置 CORS 允许列表；仅信任来自已配置代理 CIDR 的转发头；支持环境变量或 secret-provider 覆盖，但不序列化已解析密钥；添加有界 body、适当的 HTTP 安全响应头和 auth/API 限速。

- [x] **步骤 4：验证密钥与响应卫生**

运行定向测试并扫描仓库，检查过宽的密钥文件模式、原始 token/claim 日志、返回客户端的内部错误文本，以及生产环境通配符 CORS。

**验收：** security 测试在 `-race` 下通过；不再存在可变包全局认证策略；已迁移配置密钥使用最小权限；公共响应不暴露内部原因；代理与 origin 信任由配置驱动。

## 任务 12：请求隔离、全局状态与生命周期

**优先级：** 请求隔离与竞态为 P0；更广泛的生命周期合并为 P1

**状态：** 已完成。已创建中文聚焦计划 `docs/codex/plans/12-request-lifecycle-concurrency.md`；任务 12.1-12.7 对应提交为 `60b6e3a`, `fc42ae7`, `52ac181`, `87cc800`, `b816515`, `ffe27c8`, `f016173`, `8aeed28`；任务 12.8 测试入口与状态隔离在 `2f70294` 完成，WebSocket worker 生命周期在 `f0f70ae` 完成。下一主任务为任务 13 持久化正确性与外部测试分离。

**文件：**
- 创建：`docs/codex/plans/12-request-lifecycle-concurrency.md`
- 修改：`service/manage/manageservice.go`
- 修改：`pkg/server/api/manage/menumanage.go`
- 修改：`pkg/server/router/servicecontext.go`
- 修改：`pkg/server/run/server.go`
- 修改：`pkg/server/types/routerinfo.go`
- 按需修改：Provider、Fiber、WebSocket、MQ、传输和数据库生命周期 owner

- [x] **步骤 1：盘点可变进程与请求状态**

将每个 global/map/goroutine 分类为不可变注册表、已同步注册表、请求本地值或生命周期 owner 持有的 worker。包含 `ManageService.Req`、service/全局类型 map、订阅者 map、etcd lease 状态、空 Fiber 关闭、WebSocket limiter 清理和子系统关闭路径。

- [x] **步骤 2：证明请求隔离和注册表安全**

添加并发测试，证明请求 ID 和身份不会跨服务调用泄漏。使用显式参数或请求作用域上下文替换共享请求存储。从注册表返回不可变快照，并以一致方式保护所有可变 map。

- [x] **步骤 3：建立唯一生命周期 owner**

为集群成员、心跳、CrossNodeNoticeBroker、MQ、传输、数据库连接、Fiber/HTTP Server、清理 worker 和后台回调定义有序、幂等的 `Start`/`Close` 行为。使用取消、deadline 和 wait group；传播终止性启动/关闭错误。

- [x] **步骤 4：关闭 Provider 切换的对账缺口**

在 `Begin -> Complete` 期间，持续镜像或对账迁移开始后注册的节点。测试并发注册、Watch 事件、回滚、完成、重复投递和 Provider 失败，确保不会静默丢失成员。

- [x] **步骤 5：运行竞态和泄漏门禁**

按包划分竞态测试，并在重复启动/停止周期周围添加有界 goroutine 泄漏检查。记录异步 WebSocket 回调契约，并使测试通过 channel 或 wait group 同步，而非不安全地捕获状态。

**验收：** 共享服务对象上不存在请求作用域的可变状态；所有注册表使用唯一同步策略；重复 start/close 幂等；Provider 迁移对账并发成员；定向竞态与泄漏测试通过。

## 任务 13：持久化正确性与外部测试分离

**优先级：** P0

**状态：** 已完成。中文聚焦计划见 `docs/codex/plans/13-persistence-correctness.md`。默认持久化套件不依赖 MySQL、MongoDB、ClickHouse 或 Docker；外部套件同时受 `integration` build tag 和对应 `CORE_TEST_*` 环境变量控制。GORM 本次操作错误、SharedBadger 同步确认语义以及 MySQL/MongoDB/ClickHouse 真实 driver 契约均已有回归覆盖。

**文件：**
- 创建：`docs/codex/plans/13-persistence-correctness.md`
- 修改：`pkg/persistence/database/oltp/mysql.go`
- 修改：`pkg/persistence/database/oltp/sqlite.go`
- 修改：持久化同步/配置测试和外部数据库测试
- 修改：`docker-compose.integration.yml`

- [x] **步骤 1：分离单元与外部数据库契约**

识别隐式连接 `127.0.0.1:3306` 或其他服务的测试。单元测试使用 SQLite/fake，MySQL、MongoDB 和 ClickHouse 套件必须同时受 integration build tag 和显式环境变量控制。添加专用 Compose profile 和健康检查；未认证主机端口仅绑定 `127.0.0.1`。

- [x] **步骤 2：添加失败的结果传播测试**

验证 `Raw`、`Scan` 和 `Exec` 返回操作结果的 `.Error`，而非过时数据库 handle 错误。覆盖 MySQL 兼容与 SQLite 路径的查询失败、scan 失败、上下文取消和事务回滚。

- [x] **步骤 3：修正同步语义**

修复并测试默认批处理延迟、成功/失败计数、pending 状态、CAS/冲突处理、重试边界和 fatal-break 行为。当请求的完成记录数为零时，日志绝不得报告同步成功。

- [x] **步骤 4：验证两个层级**

默认持久化命令必须在没有 Docker 或隐藏本地服务时通过。为每个层级设置显式超时，并验证测试失败会及时取消重试和 worker。Docker 支撑的套件必须针对锁定的 MySQL、MongoDB 和 ClickHouse 镜像证明 driver 配置、迁移、CRUD、取消和清理。

**验收：** `go test ./pkg/persistence/... -count=1 -timeout=5m` 不依赖环境并通过；显式 Docker 持久化套件通过；过时 handle 错误传播和虚假成功报告已有回归覆盖；失败路径测试结束时无残留重试或 worker。

## 任务 14：配置到运行时能力契约

**优先级：** P1

**文件：**
- 创建：`docs/codex/plans/14-config-runtime-contract.md`
- 创建：`docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`
- 审查/修改：`pkg/server/config`
- 审查/修改：集群、传输、MQ、事件和 ServiceContext factory

- [x] **步骤 1：建立字段级能力矩阵**

为每个 server、cluster、transport、MQ、event、auth 和 persistence 字段记录已接受值、默认值、校验、运行时消费方、行为测试、生命周期 owner 和支持状态。从 MQ `Usage`、request/reply、retry、dead-letter 和 dynamic-switch 字段，以及集群 heartbeat、suspect、reuse-cooldown 和 shard 设置开始。

- [x] **步骤 2：通过真实启动路径测试配置**

使用生产构造器，而非手工填充的 `ServiceContext` 值。证明配置按必需顺序创建并启动预期集群 Provider、selector、MQ manager、event stream/bridge 和 CrossNodeNoticeBroker，且关闭时会关闭它们。

- [x] **步骤 3：删除静默能力声明**

在轻量适配器之后将受支持字段连接到成熟库行为。对不支持的组合，返回可操作的校验/启动错误，或通过迁移文档删除/废弃字段。不得接受会被静默跳过的 `quic`、`mq`、retry、dead-letter 或 usage 模式。

- [x] **步骤 4：为未来配置添加设置门禁**

每当配置 struct 或 tag 变更时，在审查模板和 CI 中强制更新矩阵与行为测试。

**验收：** 每个已接受字段都有经测试的运行时效果；不支持的值在提供流量前失败；矩阵与默认值、factory、启动和关闭行为一致。

**完成记录（2026-07-12）：** 14.1-14.4 已完成。结构化闭集门禁锁定项目自有配置字段、状态、owner 和运行时证据；Transport/MQ/Cluster 未实现能力为 rejected，自定义 MQ provider 可通过已注册 factory 使用；Etcd Prefix 已实际接入 provider；ServiceContext 对运行时资源执行终止型关闭；MQManager Close 会等待在途 Manager 操作。`./scripts/test.sh config-contract` 和 config/router/cluster/transport/mq/event 六包 race 均通过。提交 `f91c79b` 的外部审查问题已由 `c52e32e` 修复，复审结论为 APPROVED，无 P0/P1/P2 返工项。两项可选维护备注为默认 Etcd Prefix 双处常量和 Transport 布尔拒绝文案一致性，均不阻碍任务完成。

## 任务 15：公共 API 兼容性与发布治理

**优先级：** P1

**文件：**
- 创建：`docs/codex/plans/15-api-release-governance.md`
- 创建：`docs/codex/API_COMPATIBILITY_SURFACE.md`
- 修改：`pkg/server/trans/rest/error.go`
- 创建：路由、OpenAPI 与导出 Go API 兼容基线及检查脚本
- 创建或更新：`CHANGELOG.md`、发布策略、废弃登记和消费方兼容性矩阵

已删除的 `CORE_RELEASE_READINESS_PLAN.md`、`DEPENDENT_SERVICES_RISK_PLAN.md` 和 `PERSISTENCE_MANAGE_COMPAT_PLAN.md` 仅作为 Git 历史输入，不恢复为执行状态文档；仍有效的要求统一收敛到任务 15 中文聚焦计划及其兼容性产物。

- [ ] **步骤 1：定义公共兼容性表面**

列出下游服务使用的导出 Go API、路由、payload、状态码、错误码、配置键/默认值、数据库兼容性和可观测生命周期行为。

- [ ] **步骤 2：替换字符串匹配的 HTTP 错误映射**

定义类型化公共错误码、HTTP 状态、安全消息和已封装内部原因。添加表驱动测试，证明本地化或内部文本变更不能改变状态，且不暴露内部细节。

- [ ] **步骤 3：添加兼容性产物**

生成确定性 OpenAPI/路由快照和导出 Go API 基线。采用前评估并锁定受维护的兼容性检查器；有意的破坏性变更必须提供显式批准文件。

- [ ] **步骤 4：建立发布治理**

记录语义化版本、废弃期限、迁移说明、changelog 格式、tag/发布所有权、回滚，以及消费方仓库的精确 commit/tag 锁定。在本地可用时，添加 futures、omni-flow 和 ai-ops 兼容性冒烟检查。

**验收：** 公共错误已类型化且稳定；路由/导出 API 偏移已审查；有意破坏性变更具有迁移证据；发布 tag 和下游锁定可重现。

## 任务 16：CI 质量门禁与消费方兼容性矩阵

**优先级：** P1

**文件：**
- 创建：`docs/codex/plans/16-ci-quality-gates.md`
- 创建：`.github/workflows/ci.yml`；如果分离更清晰，创建聚焦的 workflow 文件
- 修改：`scripts/test.sh`
- 创建：经批准的锁定工具/版本配置

- [ ] **步骤 1：定义必需层级和时间预算**

为格式化与完整 `go vet`、快速单元测试、按包划分的竞态测试、Docker Broker/发现集成、Docker 持久化集成、配置-运行时契约和下游冒烟测试创建门禁。记录必需/可选状态和预期时长。

- [ ] **步骤 2：启用门禁前要求归属任务修复阻塞项**

任务 7 负责清理 Mongo 未键控 `bson.E`，任务 13 负责持久化单元测试失败，任务 12 负责异步回调竞态契约。任务 16 将它们通过的命令接入 CI；不得重复修复产品代码。没有已记录的 owner 和到期时间时，不得抑制警告或排除包。

- [ ] **步骤 3：实现可重现 CI**

锁定 Go、服务镜像和可选工具；缓存 module/构建输出；使用显式 build tag 和环境变量；失败时上传日志和测试产物；取消已被新运行取代的任务；设置每个 job 的超时。

- [ ] **步骤 4：谨慎添加安全与兼容性门禁**

评估 `govulncheck`、静态/安全分析、导出 API 对比、生成文件偏移和消费方冒烟测试。锁定已批准工具并定义分类/豁免所有权，不引入无 owner 警告。

**验收：** 干净检出在本地和 CI 中运行相同命令；所有必需检查通过；本地默认跳过外部服务，CI 中显式启用；失败能识别重现所需的包、服务和产物。

## 任务 17：性能、容量与运维 SLO 基线

**优先级：** 正确性与生命周期工作之后的 P2

**文件：**
- 创建：`docs/codex/plans/17-performance-slo-baseline.md`
- 创建：聚焦的 benchmark 和可观测性测试
- 审查：大型持久化、ServiceContext、router 和 WebSocket 模块

- [ ] **步骤 1：重构前先测量**

对具代表性的路由分发、持久化操作、Provider Watch/切换、event/MQ 流和 WebSocket 扇出进行 benchmark。在拆分大文件或改变并发前，捕获 CPU、分配、goroutine、队列深度和关闭延迟。

- [ ] **步骤 2：定义容量与资源预算**

为 goroutine、队列、数据库连接池、重试、缓存大小、消息/请求体和本地存储映射设置有 owner 的限制。审查接近 30 GB 的 SQLite `mmap_size`，并用测量支撑的有界、可配置值替换机器级默认值。

- [ ] **步骤 3：添加运维信号**

暴露 HTTP/Provider 操作的 RED 指标、pool/queue/worker 的 USE 风格信号、依赖健康、Provider 切换状态和关闭失败。在 HTTP、event、MQ 和跨节点边界保持 trace 连续性；避免高基数 label 和敏感字段。

- [ ] **步骤 4：建立 SLO 与回归门禁**

定义可用性、延迟、错误率、事件投递和恢复目标，且它们具有 owner 和告警阈值。只有在控制方差后才添加稳定 benchmark 对比；使用 profile 和契约边界指导任何大文件拆分。

**验收：** 基线和预算与可重现命令一同记录；关键路径发出可操作指标/trace；SLO 具有 owner；性能重构证明了可测量收益且无正确性回归。

## 跨任务归属

当发现重叠时，由以下任务负责实施；其他任务仅验证或消费其证据：

| 关注点 | 实施 owner | 消费方 |
| --- | --- | --- |
| 认证状态、TOTP 输出、CORS/代理信任、安全认证响应 | 任务 11 | 任务 8、15、16 |
| 请求/全局并发、worker、关闭、Provider 对账 | 任务 12 | 任务 9、16、17 |
| 持久化错误、同步语义、单元/外部分离 | 任务 13 | 任务 7、8、16、17 |
| 配置校验与实际运行时行为 | 任务 14 | 任务 5、10、15、16 |
| 运行时日志词汇与归属 | 任务 8 | 任务 10、16、17 |
| 公共错误、API/配置兼容性、发布 | 任务 15 | 任务 10、16 |
| CI 编排与必需检查策略 | 任务 16 | 所有任务提供命令；任务 16 不负责其产品修复 |

## 开发入口门禁

记录以下条件后可开始开发：

1. 单独提交此总计划，使实施 diff 无法静默重写范围。
2. 通过单独提交当前 Go 1.26/go-zero v1.10.2 依赖升级，或显式恢复已批准依赖基线，解决任务 1。
3. 在编辑任务 11-13 的运行时代码前，创建包含失败测试、兼容性影响、回滚和精确完成命令的聚焦计划。
4. 尽早落地可移植的任务 3 测试工具；仓库脚本和 CI 必须使用标准 `go`、`rg` 和 `docker compose`，绝不使用本地 `rtk` 封装或仅 macOS 可用的缓存路径。
5. 前三个运行时切片按以下顺序：任务 11 安全/认证隔离，任务 12 请求隔离与关闭关键竞态，任务 13 持久化正确性/测试分离。每个切片保持可独立审查和回滚。

## 执行顺序

1. 在将后续失败归因于代码变更前，冻结或单独提交任务 1 的依赖偏移。
2. 创建任务 11-13 的聚焦计划，然后修复 P0 安全默认值、请求隔离/竞态、生命周期关键缺口和持久化正确性。任务 8 的密钥/进程控制日志修复在同一 P0 阶段执行。
3. 在声称支持集群、传输、MQ 或事件功能前，完成任务 14 的配置-运行时矩阵和真实启动测试。
4. 完成任务 6 的 go-zero 复用矩阵、任务 7 的清理台账和任务 8 的完整日志清单。使用这些产物决定保留、委托、删除或废弃内容。
5. 一并完成任务 2 和 3，通过显式 Broker/发现和持久化 profile 扩展 Compose。除非脚本/CI job 设置了已记录环境变量，否则外部依赖保持禁用。
6. 健康检查稳定后完成任务 4，然后明确决定任务 5：在 `MQProvider` 之后使用受维护的 Kafka 客户端，或拒绝 Kafka 并记录为不支持。
7. 逐步启用任务 16 CI 门禁：首先是完整 vet 和单元测试，其次是竞态分区，第三是 Docker 集成与配置/消费方契约。只有在当前阻塞项修复后，门禁才能成为必需。
8. 在独立 cache、SQLite 和 Mongo/NoSQL 分支中执行任务 7；按四个包批次执行任务 8 日志统一。将任务 12 生命周期/Provider 对账变更与清理隔离。
9. 在下一次公共框架发布前，完成任务 15 类型化错误、兼容性快照、废弃策略和发布治理。
10. 完成任务 9 剩余加固和任务 10 文档对齐，然后在结构性性能重构前建立任务 17 性能/SLO 基线。

## 验证矩阵

| 命令 | 层级 | 需要 Docker | 需要外部环境 |
| --- | --- | --- | --- |
| `go vet ./...` | 全项目编译/vet 基线 | 否 | 否 |
| `./scripts/test.sh quick` | 格式化/vet + 环境无关快速单元测试 | 否 | 否 |
| `./scripts/test.sh server` | Server 包单元/集成风格测试 | 否，但必须允许本地端口绑定 | 否 |
| 计划中：`./scripts/test.sh persistence-unit` | 使用 SQLite/fake 的持久化正确性 | 否 | 否 |
| `./scripts/test.sh concurrency` | 按包划分的请求、注册表、生命周期和回调竞态测试，含 20 次重复关闭门禁 | 否，但必须允许本地端口绑定 | 否 |
| 计划中：`./scripts/test.sh config-contract` | 通过真实启动/关闭验证配置 | 本地 Provider 不需要 | 否 |
| `./scripts/test.sh integration-local` | 本地 Provider 集成测试 | 否 | 脚本设置 `CORE_TEST_CLUSTER_LOCAL=1` |
| `./scripts/test.sh integration-external` | etcd/consul/redis/nats 测试 | 是 | 由脚本默认值设置 |
| 计划中：`./scripts/test.sh integration-persistence` | MySQL/MongoDB/ClickHouse driver 和生命周期测试 | 是 | 脚本显式设置 `CORE_TEST_*` 变量 |
| 计划中：`./scripts/check-logging.sh` | 运行时日志策略和敏感输出守卫 | 否 | 否 |
| `./scripts/test.sh security` | 认证隔离、CORS/代理、文件模式、body、响应头和安全错误测试 | 否 | 否 |
| 计划中：`./scripts/test.sh compatibility` | 路由/OpenAPI/导出 API 和已配置消费方冒烟检查 | 取决于消费方 | 显式配置消费方路径或修订版 |
| `CORE_TEST_KAFKA=1 ... TestMQKafka` | Kafka Provider 契约 | 是 | 仅在 Kafka Provider 存在后 |

## 完成定义

满足以下条件时，本计划完成：

- `git status --short` 中没有未审查的依赖偏移。
- 完整 `go vet ./...` 通过，不排除包，也没有无 owner 抑制。
- `./scripts/test.sh quick` 通过。
- `./scripts/test.sh server` 通过。
- 持久化单元测试在不依赖隐藏 MySQL、MongoDB、ClickHouse 或其他本地服务时通过。
- 聚焦的竞态与生命周期泄漏测试通过，包括并发请求/认证隔离、注册表、WebSocket 回调和重复 start/close。
- `docker compose -f docker-compose.integration.yml up -d` 启动健康依赖。
- `./scripts/test.sh integration-external` 通过 etcd、Consul、Redis Streams 和 NATS JetStream。
- MySQL、MongoDB 和 ClickHouse 的显式 Docker 持久化套件通过并清理其资源。
- Kafka 已实现且 Provider 契约测试通过，或已记录为仅配置。
- 包含密钥的配置文件使用最小权限模式；CORS、转发 IP 信任、body 限制、认证策略和公共错误通过安全契约。
- 认证 issuer/audience 策略对每个 handler 不可变，且对并发 manage/user 服务安全。
- 共享服务对象中不存在请求作用域数据；可变注册表返回快照并使用一致同步。
- 每个已启动 Provider、Broker、传输、数据库、Server、回调和清理 worker 都具有幂等、有界的关闭路径。
- Provider 切换对账 `Begin -> Complete` 期间注册的节点，并在回滚/失败中保留成员。
- 每个已接受配置字段都有运行时消费方和行为测试；不支持的字段或值在校验/启动时失败，或遵循已记录废弃流程。
- `docs/codex/GO_ZERO_REUSE_AUDIT.md` 使用源码和测试证据，将每个已审查子系统标识为 keep、replace、remove 或 keep-domain。
- 契约匹配时，通用 Redis/cache、发现、并发、重试和生命周期辅助程序委托给成熟 go-zero 能力；例外具有已记录领域原因。
- 已启用运行时路径不包含 `panic("implement me")`、返回 nil 的占位 adapter 或包含密钥的调试输出。
- 重复 SQLite 实例所有权已合并，且有竞态测试覆盖。
- `./scripts/check-logging.sh` 通过，生产库代码不包含未批准的 `fmt.Print*`、标准 `log.*`、`Fatal*`、装饰性或敏感值日志。
- 请求与跨服务终止失败发出一条携带 TraceID 和稳定上下文字段的结构化事件；成功回退为 info 事件，重试尝试为 debug 事件。
- 捕获日志中不包含完整 payload、response、body、凭据、TOTP 值、DSN 和 SQL 值。
- 公共错误使用类型化稳定码/状态/安全消息；发布前审查路由/OpenAPI/导出 API 偏移和依赖服务冒烟检查。
- 必需 CI 门禁从干净检出重现本地命令，并保留可操作失败产物。
- 发布、changelog、废弃、迁移、tag、回滚和下游锁定策略已记录，并由发布候选版演练。
- 在接受性能驱动的结构变更前，已存在性能基线、资源预算、RED/USE 指标、trace 连续性和具有 owner 的 SLO。
- README 和 `docs/codex/AUTOMATED_VERIFICATION_PLAN.md` 显示相同命令。
- README 和 `use-digitalway-core` 参考声明相同 go-zero 复用与日志边界。
- `docs/codex/FRAMEWORK_USAGE_GUIDE.md` 提供由真实构造器、示例、配置和测试支撑的场景决策与成熟度标签。
