# 外部依赖集成测试指南

默认 Compose 服务为 etcd、Consul、Redis 和 NATS；MySQL、MongoDB、ClickHouse 位于 `persistence` profile，Kafka 与 RabbitMQ 分别位于 `kafka`、`rabbitmq` profile。所有测试端口只绑定 `127.0.0.1`。

## 发现与消息集成

```bash
./scripts/test.sh integration-external-docker
```

该命令负责唯一 project name、并发锁、健康等待、测试、失败诊断和 `down -v --remove-orphans`。设置 `KEEP_CONTAINERS=1` 仅用于本地诊断，完成后必须手工执行：

```bash
docker compose -f docker-compose.integration.yml down -v --remove-orphans
```

## 持久化集成

```bash
./scripts/test.sh integration-persistence
```

## Docker credential helper 排障

若公开镜像拉取长期无 layer 进度，且中断后出现 `error getting credentials`，先检查本机 Docker credential helper。不要修改仓库脚本或把镜像改为浮动 tag。可使用不含认证信息的临时 `DOCKER_CONFIG` 验证公开镜像；Docker Desktop 用户还需让临时 config 能发现 `docker-compose` CLI plugin。该临时配置只用于公开镜像，不得用于需要私有 registry 凭据的任务。

## Kafka 与 RabbitMQ Provider

显式外部 MQ 门禁：

```bash
./scripts/test-external-mq.sh
```

该脚本使用唯一 Compose project，启动固定版本 Kafka/RabbitMQ，设置 `CORE_TEST_KAFKA=1` 与 `CORE_TEST_RABBITMQ=1`，运行普通发布订阅、可靠失败重投、消费组隔离和 EventBridge Envelope round-trip；退出时只对该 project 执行 `down -v --remove-orphans`。失败时设置 `CI_ARTIFACT_DIR` 可保留 `ps` 和 Broker 日志。

两种内建 Provider 都是 at-least-once，不是 exactly-once；可靠 Handler 成功后才确认，业务必须继续用 EventID/Inbox 幂等。两者都不满足 `RequireOrderedReliableByShardKey`，显式 `KeyConcurrency>1` 必须 fail closed。Kafka 新消费组默认从当前末尾消费；Connect 不保证 topic 已存在，首次 Publish 在 Broker 允许 auto-create 时建 topic，否则由运维预建。默认 PR quick 不启动 Broker；真实门禁是手工/定时外部验证，未运行时必须报告 `NOT RUN`。
