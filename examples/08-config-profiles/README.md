# 08 - Config Profiles

本示例展示新增配置结构的默认值和兼容旧配置策略：

- 旧配置文件缺少 `Cluster`、`Transport`、`MQ` 字段时，应通过 `ApplyDefaults()` 补齐。
- `Cluster.Mode` 默认 `auto`，Provider 默认 `local`。
- `Transport.Internal` 默认 `grpc`，Fallback 默认 `grpc -> http -> socket`。
- `MQ.Mode` 默认 `auto`，Provider 默认 `redis-stream`，开发阶段优先 Redis Stream。

## 运行

```bash
go run ./examples/08-config-profiles/main
```

该示例只构造和打印配置，不启动外部服务。
