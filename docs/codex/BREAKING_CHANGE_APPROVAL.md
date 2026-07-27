# Casdoor 认证生命周期破坏性变更批准

- 变更 ID：`casdoor-auth-lifecycle-v1`
- Owner：`server/api/public`、`server/authstate`
- 批准日期：2026-07-16
- 目标版本：下一个 `Unreleased` 候选版本

## 批准范围

1. 删除旧 HTTP `/api/callback`，前端必须先调用 `/api/casdoor?type=auth|manage&service=<name>`，并使用返回的 `background_callback_url`（路径为 `/api/casdoor/callback`，HTMLServer 多服务场景会附带 `service`）。
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
3. 删除 `GRPCTransportConfig.Enable`；Go 调用方改用 `Transport.Internal=grpc`，旧 JSON 键由配置迁移器删除。
4. `SetAttachService` 改为三参数；旧源码必须迁移到 `ClusterProvider + ServiceResolver`，不保留 Socket 兼容 shim。
5. 内部同步传输默认改为 gRPC；HTTP 仅作为显式发送前备用，EventBridge/WebSocket 不承担同步调用。
6. `Transport`/`TransportSelector.Select` 改为 context + 协议端点的 `Selection` 契约；删除 `SendWithFallback`，调用方改用 `SelectWithRetry`、`SendSelection` 或 `Send`。
7. `CrossNodeSender` 从地址字符串改为接收完整 `*NodeInfo`，由发送器选择 `GRPCPort`；`ServiceContext.GetServers` 返回 go-zero `service.Service` 以统一 HTTP、gRPC 和扩展服务生命周期。
8. `MembershipManager.Stop` 返回关闭错误，构造器接受 `MembershipOption`；该类型不再承诺可比较，以支持注销重试与有界关闭状态。

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

---

# OpenAPI 受众拆分破坏性变更批准

- 变更 ID：`openapi-audience-split-v1`
- Owner：`server/internal/openapidoc`、`server/run`、`server/api/public`
- 批准日期：2026-07-23
- 目标版本：下一个包含该 HTTP 删除的 MAJOR 候选版本

## 批准范围

1. 删除重复的 `public.OpenAPI` 生成器和旧 HTTP `/api/servermanage/openapi`。
2. 匿名 `GET /api/openapi` 只展示普通 Public 与 Private，不展示声明了 `WithInternalCallers` 的路由，也不输出 `x-internal-callers`。
3. 新增 `GET /api/internal/openapi`，使用 `ServerManageAuth`，展示完整 Public/Private 文档和内部调用方扩展；`service` query 可筛选单个服务。
4. 外部、内部入口和兼容性快照必须共用 `pkg/server/internal/openapidoc`，不得恢复第二套生成器。

## 安全、迁移与回滚

- 旧调用方把 `/api/servermanage/openapi` 改为 `/api/internal/openapi`，并使用 ServerManage 域 Token；无需内部信息的调用方改用匿名 `/api/openapi`。
- 内部响应设置 `Cache-Control: private, no-store`，代理不得缓存。
- 回滚会重新暴露旧重复端点和内部元数据，生产回滚前必须确认网络边界与调用方风险。

## 验证证据

- `pkg/server/run/openapi_test.go`
- `pkg/server/api/public/rate_limit_test.go`
- `pkg/server/api/release/routes_test.go`
- `pkg/server/trans/rest/server_security_test.go`
- `./scripts/test.sh api-compat`
- `./scripts/test.sh release-contract`

---

# Logto、旧服务依赖与顶层配置删除批准

- 变更 ID：`logto-legacy-service-config-removal-v1`
- Owner：`server/config`、`server/router`、`server/trans/rest`
- 批准日期：2026-07-25
- 目标版本：下一个包含该删除的 MAJOR 候选版本

## 批准范围

1. 删除 Logto 配置、中间件、身份常量和专用 `keyfunc`、`jwt/v5` 依赖；受保护 REST 路由统一验证框架 Access Token，Casdoor 继续提供外部身份生命周期与撤销事实。
2. 删除 `Service.AttachService`、`IService.SubscribeRouters`、`ObserveArgs`、`NotifyArgs`、Router Observe 生命周期，以及 Attach/Observe/Notify/动态设置地址的系统 API。
3. 删除顶层持久配置 `RunIp`、`ParentServerIP`、`AttachServices`、`Debug`、`CustomerDataList`；旧 JSON 读取前幂等移除这些键和三组 Auth 下的 `Logto`，未知字段保持不变。
4. 服务实例地址由 `ServiceContext.RuntimeAddress()` 提供，`Cluster.AdvertiseAddress` 显式配置优先；REST 监听端口仍由 `RestConf.Host/Port` 拥有。
5. 同步跨服务调用统一使用同进程 `ServiceContext` 注册表或 `ServiceResolver`，异步事件统一使用显式 `ServiceContext.SubscribeEvent` 和 EventBridge。

## 迁移要求

- Logto 消费方迁移到框架 JWT；需要外部身份生命周期时使用 Casdoor。
- 删除服务实现中的 `SubscribeRouters()`。领域事件在 `Start()` 中显式订阅，可靠跨进程事件使用 Inbox/Outbox。
- 不再持久化服务节点地址。显式直连只允许在单次 `PayLoad.TargetAddress/TargetPort` 中表达；常规调用只提供 `TargetService`。
- 删除旧 Attach/Observe/Notify HTTP 调用和管理界面的依赖地址编辑逻辑。

## 验证证据

- `internal/compat/removed_capabilities_test.go`
- `pkg/server/config/serverconfig_removed_features_test.go`
- `pkg/server/router/serviceresolver_test.go`
- `pkg/server/trans/rest/authrequest_test.go`
- `./scripts/test.sh config-contract`
- `./scripts/test.sh public-api`
- `./scripts/test.sh release-contract`
