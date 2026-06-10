# Core Codex — 优化建议

> 基于代码深度审查,2026-06-10

## 优先级分类

| 级别 | 定义 | 数量 |
|------|------|------|
| 🔴 P0 | 影响生产稳定性/安全性，必须修复 | 3 |
| 🟡 P1 | 影响性能/可维护性，尽快修复 | 5 |
| 🟢 P2 | 改善开发体验/代码质量，可延后 | 4 |

---

## 🔴 P0 — 生产关键

### 1. gRPC 每次调用都创建新连接，无连接池

**位置**: `pkg/server/transport/grpc/client.go`

**现状**: `GRPCTransport.Send()` 每次调用都 `dial` + `defer Close()`。高频调用下浪费大量 TCP 握手和 gRPC 协商开销。

**影响**: 高频内部服务间调用场景（行情推送、订单状态同步），延迟增加 50-100ms/次。

**建议**:
```go
// 使用 sync.Map 按 target 缓存连接
type GRPCTransport struct {
    pool sync.Map  // map[string]*grpc.ClientConn
    maxRecvMsgSize int
    maxSendMsgSize int
}

func (g *GRPCTransport) Send(ctx context.Context, payload *types.PayLoad, target string) ([]byte, error) {
    conn, err := g.getOrCreateConn(target)
    // ...
    client := proto.NewCoreTransportClient(conn)
    resp, err := client.Call(ctx, req)
    // 不关闭连接，复用
}
```

### 2. Consul Heartbeat/Deregister 是 O(n) 线性扫描

**位置**: `pkg/server/cluster/provider_consul.go`

**现状**: `Heartbeat()` 和 `Deregister()` 调用 `c.client.Health().Service("", "")` 遍历全量 catalog 再线性匹配 nodeID。当集群服务数量增长时不可扩展。

**影响**: 100+ 服务实例时，每次心跳(默认 3s)都会产生 O(n) 的 Consul API 压力。

**建议**: 注册时存储 `serviceID`（已生成格式 `<serviceName>-<nodeID>`），心跳和注销直接用 `consulAPI.Health().Service(serviceName, serviceID, ...)` 精确查询。

### 3. ServiceContext 网络错误无重试机制

**位置**: `pkg/server/router/servicecontext.go` 第 771/788 行

**现状**: 代码中已有 TODO 注释标注网络错误应进入重试，但未实现。`CallService` 失败直接返回错误给调用方。

**影响**: 间歇性网络抖动（Docker 网络、K8s Pod 重启）会导致请求失败。

**建议**: 实现带 backoff 的重试策略，可配置重试次数和超时。

---

## 🟡 P1 — 性能与可维护性

### 4. EtcdProvider 硬编码 TTL 忽略配置值

**位置**: `pkg/server/cluster/provider_etcd.go`

**现状**: `etcdTTL = 10` 是常量，但 `EtcdProviderConfig.TTL` 已在 config 结构中定义并默认 10s。配置值被忽略。

**建议**: `NewEtcdProvider` 使用 `cfg.TTL` 替代硬编码常量:
```go
func NewEtcdProvider(cfg *EtcdProviderConfig, endpoints []string) (*EtcdProvider, error) {
    if cfg.TTL > 0 {
        ttl = cfg.TTL  // 使用配置值
    }
    // ...
}
```

### 5. CrossNodeNoticeBroker 只用 HTTP 转发，不用已配置的传输层

**位置**: `pkg/server/cluster/event.go`

**现状**: `sendNoticeToPeer` 和 `sendSummaryToPeer` 始终用 `http.Client.Post`。即使 ServiceContext 已配置 gRPC/Socket 作为内部传输，WebSocket 跨节点通知仍走 HTTP。

**影响**: 与传输统一策略不一致，跨节点 WebSocket 通知不能受益于 gRPC 的性能优化。

**建议**: `CrossNodeNoticeBroker` 持有 `TransportSelector` 引用，通过 `SendWithFallback` 发送通知。

### 6. ProviderSwitcher warm migration 只在 Begin 时刻复制节点

**位置**: `pkg/server/cluster/switcher.go`

**现状**: `Begin()` 调用 `current.List() + to.Register()` 做一次性节点复制。Begin 到 Complete 之间如果有节点上下线，新 provider 的节点列表会过时。

**建议**: 引入 `Begin -> DualWrite -> Complete` 或 reconciliation goroutine，在 migration 期间持续同步节点变更。

### 7. Transport 无超时/重试配置

**位置**: `pkg/server/config/transportconfig.go`, `pkg/server/transport/selector.go`

**现状**: `TransportConfig` 只有 Internal/Fallback/GPRC 端口，没有 Timeout/Retry/Backoff 字段。各 Transport 实现各自 hardcode 超时(HTTP 30s, Socket 10s dial + 30s read)。

**建议**: 在 `TransportConfig` 中加入统一超时和重试配置:
```go
type TransportConfig struct {
    Internal string
    Fallback []string
    Timeout  time.Duration  // 新增
    MaxRetries int          // 新增
    // ...
}
```

### 8. Socket Transport Health 只检测 TCP 可达性

**位置**: `pkg/server/transport/socket/socket_transport.go`

**现状**: `Health()` 仅 dial + close，不验证对端是否真正运行 socket 协议。可能导致 selector 选到错误端点。

**建议**: Health 发送一个 ping 帧并等待响应，或至少做应用层版本协商。

---

## 🟢 P2 — 代码质量

### 9. Config 验证接受 quic/mq，但 BuildSelector 运行时拒绝

**位置**: `pkg/server/config/transportconfig.go` vs `pkg/server/transport/factory.go`

**现状**: `TransportConfig.Validate()` 将 `quic`/`mq` 视为合法值（不报错），但 `BuildSelector` 对 Internal=quic/mq 返回 error（panic）。配置验证通过 ≠ 服务能启动。

**建议**: 要么在 Validate 时区分 supported/unsupported（给明确 warning），要么让 `BuildSelector` 在 `mode=auto` 时优雅降级。

### 10. MachineID 分配未在接口层面统一

**位置**: `pkg/server/cluster/`

**现状**: `LocalProvider.AllocateMachineID`, `EtcdProvider.AllocateMachineID`, `ConsulProvider.AllocateMachineID` 有不同方法签名。`claimMachineID` 只调用 `processLocalRegistry.AllocateMachineID`，不通过配置的 cluster provider 分配。

**建议**: 如果 MachineID 应在集群范围内去重，应通过外部的 etcd/consul provider 分配而非仅本地 registry。

### 11. 测试中的 persistence 层失败

**状态**: 11 个 SQLite + BadgerDB 测试失败（289 pass）。

**根因**: `TestNewSqlite` 和 `TestSqlite_*` 系列测试依赖特定的测试环境状态（已打开的 DB 连接、WAL 模式配置冲突）。`TestSyncConfig_DefaultBatchDelay` 期望 100ms 但实现用 0s。`TestIssue_SmallValueThreshold_VlogGrowsUnboundedly` 是已知 Badger 设计问题。

**建议**: 
- SQLite 测试：添加 `defer Close()` 或使用独立临时目录避免连接泄漏
- Badger sync config：要么更新测试期望值为 0s，要么修改实现默认值为 100ms

### 12. 缺少 QUIC/MQ transport adapter

**位置**: `pkg/server/transport/`

**现状**: `pkg/server/trans/quic/` 已有 QUIC client/server 代码，`pkg/server/mq/` 已有 MQ provider。但没有对应的 `Transport` adapter 接入 `BuildSelector`。

**建议**: 
- QUIC: 封装 `pkg/server/trans/quic` 为 `transport/quic/adapter.go` 实现 `Transport` 接口
- MQ: 创建 `transport/mq/adapter.go`，用 `MQManager.Publish/Subscribe` + request-reply pattern 实现 `Transport`

---

## 整体架构评价

### 优点
- ✅ Genric 设计 (`ModelList[T]`, `ManageService[T]`) 提供类型安全的 CRUD
- ✅ Pipeline 模式 (`Parse→Validation→Do`) 分离关注点
- ✅ Observer 模式 (`SubscribeRouters`) 实现松耦合的事件通知
- ✅ 配置驱动的 Provider/Switcher 支持动态切换
- ✅ Snowflake ID + MachineID 分配避免分布式 ID 冲突
- ✅ EventBridge 连接本地 Stream 和外部 MQ，统一事件总线

### 改进方向
- 🔄 连接池化：gRPC、HTTP 客户端需要连接复用
- 🔄 可观测性：需统一的 tracing/metrics（现有 OpenTelemetry trace 但 metrics 覆盖不全）
- 🔄 错误处理：重试、circuit breaker、graceful degradation 未体系化
- 🔄 分布式 MachineID：应通过外部分配而非仅本地 registry
