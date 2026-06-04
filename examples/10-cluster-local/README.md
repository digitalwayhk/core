# 10 - Cluster Local

本示例展示默认自研 local provider：

- 按 `ServiceName + DataCenterID + MachineID` 注册实例。
- 当同一实例槽位已被占用时，可通过 claim 逻辑扩展 MachineID。
- 服务下线时应 Deregister，停止心跳后 provider 可按 TTL 清理。

## 运行

```bash
go run ./examples/10-cluster-local/main
```

该示例不依赖外部服务。
