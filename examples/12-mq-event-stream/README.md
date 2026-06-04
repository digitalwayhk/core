# 12 - MQ Event Stream

本示例展示 MQ 与 event-stream：

- 开发阶段默认推荐 Redis Stream。
- 用户量增长后可切换到 NATS JetStream，不需要改业务代码。
- `ServiceContext` 在 MQ usage 包含 `event-stream` 时，会创建 MQ-backed EventBridge。

示例代码使用内存 fake MQ provider，方便本地直接运行；README 同时提供 Redis/NATS Docker 命令。

## 运行本地 fake provider 示例

```bash
go run ./examples/12-mq-event-stream/main
```

## Redis Stream Docker

```bash
docker run --rm --name core-redis \
  -p 6379:6379 \
  redis:7
```

配置要点：

```json
{
  "MQ": {
    "Mode": "on",
    "Provider": "redis-stream",
    "Usage": ["event-stream"],
    "RedisStream": {
      "Addr": "127.0.0.1:6379",
      "Prefix": "digitalway-core"
    }
  }
}
```

## NATS JetStream Docker

```bash
docker run --rm --name core-nats \
  -p 4222:4222 -p 8222:8222 \
  nats:2.10 -js -m 8222
```

配置要点：

```json
{
  "MQ": {
    "Mode": "on",
    "Provider": "nats-jetstream",
    "Usage": ["event-stream"],
    "NATSJetStream": {
      "URL": "nats://127.0.0.1:4222",
      "StreamPrefix": "digitalway-core",
      "DurablePrefix": "core"
    }
  }
}
```
