# Casdoor 认证生命周期破坏性变更批准

- 变更 ID：`casdoor-auth-lifecycle-v1`
- Owner：`server/api/public`、`server/authstate`
- 批准日期：2026-07-16
- 目标版本：下一个 `Unreleased` 候选版本

## 批准范围

1. 删除旧 HTTP `/api/callback`，前端必须先调用 `/api/casdoor?type=auth|manage`，并使用返回的 `background_callback_url`（当前为 `/api/casdoor/callback`）。
2. `public.Callback` 和 `public.Casdoor` 只保留 Go 类型别名兼容，新代码使用 `CasdoorCallback` 和 `CasdoorConfig`。
3. Casdoor Access/Refresh Token 新增并强制校验 `auth_provider`、`provider_subject`、`auth_generation`；Auth 与 Manage 密钥和用途严格隔离。
4. 升级部署必须轮换 Auth/Manage 的 Access 与 Refresh 四个 Secret，旧 Token 全部失效，用户和管理员重新登录。

## 安全与回滚

- shared 撤销模式必须同时具备 Redis 权威和 MQ EventBridge 控制通道；任何一个不可用时认证面 fail closed。
- Webhook 使用独立 Secret，限制 64KiB，并禁止日志记录 Header、Payload、Claims 或 Token。
- 回滚到不识别新 Claims 的版本不受支持；需要回滚时先停止流量、恢复旧配置并再次轮换全部 Token Secret。

## 验证证据

- `examples/integration/casdoor-auth-lifecycle`
- `./scripts/test.sh security`
- `./scripts/test.sh integration-casdoor-auth`
- `./scripts/ci.sh required/contracts`

---

# 自定义 Socket 到 gRPC 破坏性变更批准

- 变更 ID：`socket-to-grpc-v1`
- Owner：`server/config`、`server/router`、`server/transport`、`server/run`
- 批准日期：2026-07-17
- 目标版本：下一个包含该变更的 MAJOR 候选版本

## 批准范围

1. 删除 `pkg/server/trans/socket`、`pkg/server/transport/socket` 和 `-socket` 参数。
2. 删除 `ServerConfig.SocketPort`、`Transport.Socket`、`NodeInfo.SocketPort` 及 payload/observe/attach 的 Socket 端口字段。
3. `SetAttachService` 改为三参数；旧源码必须迁移到 `ClusterProvider + ServiceResolver`，不保留 Socket 兼容 shim。
4. 内部同步传输默认改为 gRPC；HTTP 仅作为显式发送前备用，EventBridge/WebSocket 不承担同步调用。
5. `Transport`/`TransportSelector.Select` 改为 context + 协议端点的 `Selection` 契约；删除 `SendWithFallback`，调用方改用 `SelectWithRetry`、`SendSelection` 或 `Send`。
6. `CrossNodeSender` 从地址字符串改为接收完整 `*NodeInfo`，由发送器选择 `GRPCPort`；`ServiceContext.GetServers` 返回 go-zero `service.Service` 以统一 HTTP、gRPC 和扩展服务生命周期。
7. `MembershipManager.Stop` 返回关闭错误，构造器接受 `MembershipOption`；该类型不再承诺可比较，以支持注销重试与有界关闭状态。

## 安全、迁移与回滚

- 跨主机生产部署使用 `mtls` 或已有身份校验的 `mesh`，禁止 `insecure`。
- 旧 JSON 的 Socket 字段由幂等迁移器删除；`Internal/Fallback=socket` 必须人工改为 `grpc` 或 `http`。
- 迁移说明：`docs/codex/GRPC_TRANSPORT_MIGRATION.md`。
- 回滚必须同时回滚 Core、配置和启动参数；不支持新旧内部传输节点混跑。

## 验证证据

- 实现与审查修复：`3020f99..12ca575`
- `pkg/server/transport/grpc` 的 zrpc、health、TLS/mTLS、关闭和错误身份测试
- `examples/integration/06-shop-microservices-three-process` 的 gRPC 调用计数与 HTTP 零调用证明
- `./scripts/test.sh release-contract`
- `./scripts/ci.sh required/contracts`
- 锁定 apidiff 对旧/新基线的报告必须只包含本批准范围内的上述不兼容项。
