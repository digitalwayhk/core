# Copilot Review Bugs Round 3

本文件记录 Codex 对提交 `364fa5a Fix P0/P1/P2 round-2 bugs from COPILOT_REVIEW_BUGS_ROUND2.md` 的复查结果。

第二轮的主要问题已经基本修复：

- `ClusterSwitcher` complete / rollback 后会同步 `ServiceContext.ClusterProvider`。
- switcher warm-up 已按 serviceName 迁移节点。
- `Cluster.Mode=on` / `MQ.Mode=on` 初始化失败会阻止启动。
- WebSocket 重复注销增加了实际存在判断和测试。
- `TransportSelector` 已按配置构建 HTTP / Socket / gRPC 顺序。

仍需继续修复下面的问题。

## P1: TransportConfig 允许 quic/mq，但 BuildSelector 会静默跳过

### 现象

`TransportConfig` 的合法值包含 `grpc | http | socket | quic | mq`，用户可以配置：

```json
{
  "Transport": {
    "Internal": "quic",
    "Fallback": ["mq", "grpc", "http"]
  }
}
```

但当前 `transport.BuildSelector` 只支持构建 `grpc`、`http`、`socket`。当 `Internal` 或 `Fallback` 中出现 `quic` / `mq` 时，只打印日志并跳过。

这会带来两个问题：

- 显式配置 `Internal=quic` 或 `Internal=mq` 时，实际不会使用用户选择的传输。
- 如果配置只有 `quic` / `mq`，`BuildSelector` 返回 nil，`ServiceContext.sendPayload` 会继续 fallback 到旧 HTTP `CallService`，导致配置错误被静默掩盖。

### 定位

- `pkg/server/config/transportconfig.go:65-67` 将 `quic`、`mq` 视为合法 transport。
- `pkg/server/transport/factory.go:16-22` builders 只包含 `grpc`、`http`、`socket`。
- `pkg/server/transport/factory.go:39-41` 未实现协议只 log 后跳过。
- `pkg/server/router/servicecontext.go:747-759` selector 为空或失败时会 fallback 到旧 HTTP 调用路径。

### 期望修复

优先方案：

- 增加 `transport/quic` adapter，封装已有 `pkg/server/trans/quic` 的 client 能力。
- 增加 `transport/mq` adapter，使用 `ServiceContext.MQManager` 或等价依赖，把 MQ 作为内部传输的一种选择。
- `transport.BuildSelector` 需要能接收 MQ 依赖，例如 `BuildSelector(cfg config.TransportConfig, mgr *mq.MQManager)`。

如果短期还不能实现 quic/mq adapter：

- 显式配置 `Internal=quic`、`Internal=mq` 或 fallback 中只剩未实现协议时，应返回明确错误，不能静默跳过并回落旧 HTTP。
- 可引入 `BuildSelectorResult` 或 `BuildSelector(...)(TransportSelector, error)`，旧调用点需要在显式配置不可用时阻止启动或至少返回可见错误。
- 测试要覆盖 `Internal=quic` / `Internal=mq`，确认不会被静默跳过。

### 必补测试

- `Transport.Internal=quic` 时：
  - 已实现 adapter：selector primary 名称为 `quic`。
  - 未实现 adapter：构建 selector 返回明确错误。
- `Transport.Internal=mq` 时：
  - 已实现 adapter：selector primary 名称为 `mq`，并使用配置中的 MQ provider。
  - 未实现 adapter：构建 selector 返回明确错误。
- `Fallback=["mq","grpc","http"]` 时，fallback 顺序必须按配置保留，不能无提示删掉 mq。
- `Transport.Internal=quic` 且无可用 fallback 时，服务启动不应静默回退到旧 HTTP。

## 当前验证结果

提升权限后，以下测试通过：

```bash
go test ./pkg/server/config ./pkg/server/cluster ./pkg/server/transport/... ./pkg/server/mq ./pkg/server/event ./pkg/server/types ./pkg/persistence/types
go test -tags=integration ./tests/integration/...
```

普通沙箱下 gRPC 测试仍会因为不能监听 `127.0.0.1:0` 报 `bind: operation not permitted`，提升权限后通过。
