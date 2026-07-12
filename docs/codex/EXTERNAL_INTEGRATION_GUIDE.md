# 外部依赖集成测试指南

默认 Compose 服务为 etcd、Consul、Redis 和 NATS；MySQL、MongoDB、ClickHouse 位于 `persistence` profile，Kafka 位于 `kafka` profile。所有未认证端口只绑定 `127.0.0.1`。

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

## Kafka 状态

Kafka 仅提供显式基础设施 profile：

```bash
docker compose -f docker-compose.integration.yml --profile kafka up -d kafka
```

Core 没有内建 Kafka `MQProvider`。`BuildManager` 对未注册的 kafka provider 返回配置错误；应用只有在注册自定义 `ProviderFactory` 后才能使用。不得设置或报告 `CORE_TEST_KAFKA`，直到仓库拥有 Kafka provider 契约测试。
