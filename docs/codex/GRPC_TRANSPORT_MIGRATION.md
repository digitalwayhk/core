# 自定义 Socket 到 gRPC 迁移指南

本指南适用于从旧版 Core 自定义内部 Socket 传输升级到当前 gRPC 默认传输的服务。该变化删除公开 Go API、配置字段和命令行参数，只能进入经批准的 MAJOR 版本，不能作为 `v0.x` PATCH/MINOR 静默发布。

## 迁移表

| 旧表面 | 新表面 | 行为变化 |
| --- | --- | --- |
| `ServerConfig.SocketPort` | `ServerConfig.Transport.GRPC.Port` | `0` 按 HTTP 端口派生；显式值为 `1..65535`，并通过 `NodeInfo.GRPCPort` 进入发现链 |
| `Transport.Internal=socket` | `Transport.Internal=grpc` | 客户端复用 go-zero `zrpc.Client`，服务端提供标准 gRPC health |
| `Transport.Socket` | 删除 | 不再存在 Socket Enable；HTTP 备用必须写入 `Transport.Fallback` |
| `Transport.GRPC.Enable` | `Transport.Internal=grpc` | Go 字段删除；旧 JSON 键由迁移器幂等删除 |
| `-socket` | `-grpc` | 命令行参数是破坏性替换 |
| Socket ping/自定义健康包 | `grpc_health_v1.Check` | 使用标准 `SERVING/NOT_SERVING` 探针 |
| 明文私网 Socket | `mtls` 或 `mesh` | 外部 Redis/Consul 发现默认要求可验证身份层 |
| Socket payload 地址/端口字段 | 逻辑 `TargetService` + `ServiceResolver` | 调用方不保存节点地址，Resolver 是唯一端点权威 |
| `TransportSelector.Select(ctx,payload,target)` | `Select(ctx,payload,TransportEndpoints) -> Selection` | 协议与端点绑定，健康检查和发送不能选到不同目标 |
| `SendWithFallback` | `SelectWithRetry` + `SendSelection` 或 `Send` | 只在发送前切换协议，发送后不重试 |
| `CrossNodeSender(ctx,address,...)` | `CrossNodeSender(ctx,*NodeInfo,...)` | 发送器读取目标 `GRPCPort` 和服务身份 |
| `ServiceContext.GetServers() []IRunServer` | `[]service.Service` | HTTP、gRPC 与扩展服务统一进入 go-zero ServiceGroup 生命周期 |
| `MembershipManager.Stop(ctx)` | `Stop(ctx) error` | 注销/关闭失败可观测；构造器可用 `MembershipOption` 配置有界关闭 |

旧 JSON 中的顶层 `SocketPort` 和 `Transport.Socket` 会在读取前被迁移器删除，其他未知字段保持不变；迁移是幂等的。`Transport.Internal=socket` 或 `Fallback` 中的 `socket` 不会被猜测改写，启动会 fail closed，部署者必须按实际拓扑明确改为 `grpc` 或 `http`。

## 推荐配置

单进程或仅本机开发可显式使用 insecure：

```json
{
  "Transport": {
    "Internal": "grpc",
    "Fallback": ["http"],
    "GRPC": {
      "Port": 19090,
      "Security": {"Mode": "insecure"}
    }
  }
}
```

Redis、Consul 等跨主机发现使用 `mtls`，为每个服务签发包含稳定服务名的证书；`ServerName: "{service}"` 会按目标服务名动态校验：

```json
{
  "Transport": {
    "Internal": "grpc",
    "Fallback": ["http"],
    "GRPC": {
      "Port": 19090,
      "Security": {
        "Mode": "mtls",
        "CAFile": "/run/secrets/ca.pem",
        "CertFile": "/run/secrets/service.pem",
        "KeyFile": "/run/secrets/service.key",
        "ServerName": "{service}"
      }
    }
  }
}
```

`mesh` 仅在 sidecar/服务网格已经完成双向身份校验时使用，Core 不重复装载证书。生产环境不得使用 `insecure`。

## 调用与生命周期

- 同步内部调用默认走 `ClusterProvider -> ServiceResolver -> TransportSelector -> zrpc.Client`。
- Core Resolver 负责节点选择；不得再引入 zrpc 自带发现或读取 `AttachServices` 静态地址。
- HTTP 只作为显式发送前备用。gRPC 一旦开始发送，无论结果是否确定，都不得跨协议重试，避免重复业务写。
- 每个 ServiceContext 拥有独立 gRPC listener、标准 health 和有界关闭；关闭顺序为 `NOT_SERVING -> 注销发现 -> GracefulStop/Stop -> 关闭 zrpc Client pool`。
- EventBridge 处理内部异步事件；WebSocket 只面向最终外部用户，二者都不是同步 RPC 备用协议。

## 远程调用方身份

受限 Public 不能只信任 gRPC 载荷中的 `SourceService`。该字段是调用方声明，服务端必须从已完成双向验证的 TLS peer 中读取客户端证书身份，并要求逻辑服务名与客户端证书的 mTLS SAN 完全一致，之后才把身份交给 `WithInternalCallers` 白名单校验。

缺少客户端证书、证书未通过 CA 验证、SAN 不匹配、只修改 `SourceService`、普通 HTTP 直连，都会在目标 Router 的 `Parse` 之前失败。`insecure` 远程调用不能满足此信任条件。`mesh` 只有在应用接入了可验证且不可由请求伪造的工作负载身份时，才可为受限路由提供等价信任；仅声明由 sidecar 加密并不自动注入调用方身份。

同进程调用不经过证书校验，可信身份直接来自源 `ServiceContext`，但仍执行同一白名单。示例 06 同时用 all-in-one 和三进程 mTLS 测试证明两条路径。

## 升级与回滚

1. 跨主机部署先准备并验证 mTLS 证书或 mesh 身份层；默认 `mtls` 缺少 CA/证书/私钥时会 fail closed，远程拓扑不得临时改用 `insecure`。
2. 更新 Core、Transport 配置和启动参数，确认所有候选节点能够发布正确的 `GRPCPort`。
3. 运行 gRPC health、错误 CA、错误服务名和 HTTP fallback 负向测试。
4. 使用消费方精确提交或 tag 运行编译、真实配置加载和行为 smoke；旧源码引用 Socket Go API 时必须先改代码。
5. 所有节点验证完成后统一切换；禁止新旧节点混跑。回滚必须同时回滚 Core 版本、身份材料和服务配置。

源码迁移还必须处理公共 Transport 和 Membership 生命周期签名；完整批准范围见 `docs/codex/BREAKING_CHANGE_APPROVAL.md`，不能只删除配置中的 `SocketPort` 就认为升级完成。

实现与验证提交链为 `3020f99..12ca575`。最终发布还必须通过 `./scripts/test.sh release-contract` 和消费方矩阵门禁。
