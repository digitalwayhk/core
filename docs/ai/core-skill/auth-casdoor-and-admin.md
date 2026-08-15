# Casdoor 认证与 Web Admin 边界

## Web Admin bootstrap 与 Casdoor 部署边界

`GET /api/web/bootstrap` 是 **HTMLServer 开发/测试视图**的前端启动契约，不是每个正式业务服务自动注册的系统 API：

- 只有 `WebServer.ViewPort > 0` 时 HTMLServer 才执行 `Prepare()` 并挂载该路由；`ViewPort=0` 时正式 REST/gRPC 服务不选择 `ManageAuthAuthorityService`，也没有 `/api/web/bootstrap`。
- 当前 Web Admin 启动时固定读取该接口。若正式部署不启动 HTMLServer，外部网关/静态站点必须另行提供兼容 bootstrap 契约；Core 当前的 `release.Routers()` 不会在业务服务端口自动注册它。不得把“HTMLServer 不启动”与“当前 Admin 仍可直接读取 bootstrap”同时写成已支持。
- bootstrap 只返回模式、公开端点和 UI 能力，不返回 Secret 或 Token，响应带 `Cache-Control: no-store`。

bootstrap 的 Manage 登录矩阵由 **HTMLServer 选中的权威服务**决定：

| 权威服务运行时条件 | `auth.mode` | `ui.show_login` | `ui.show_logout` | `ui.show_test_identity` |
| --- | --- | --- | --- | --- |
| `ManageAuth.CasDoor.Enable=true` | `casdoor` | `true` | `true` | `false` |
| Casdoor 未启用，且本地来源允许 TestToken | `test_token` | `false` | `false` | `true` |
| 无权威服务、非本地 TestToken 或认证不可用 | `unavailable` | `false` | `false` | `false` |

同一个服务支持同时设置 `Auth.CasDoor.Enable=true` 和 `ManageAuth.CasDoor.Enable=true`。`ServiceContext` 会通过 `NewClientSet` 同时创建互不共享状态的 Auth Client 与 Manage Client：

- `type=auth` 签发普通用户身份，只能进入 Private API 和用户 WebSocket；`type=manage` 签发管理身份，只能进入 Manage API，两类 Token 不能跨域使用。
- `/api/casdoor`、`/api/casdoor/callback`、`/api/refresh` 和 `/api/casdoor/webhook` 通过 `type=auth|manage` 选择认证域；Callback/Refresh 未给 `type` 时默认 Auth 域。
- 两个域分别配置 YAML、Client、Access/Refresh Secret 和 Webhook Secret；Secret 必须隔离，但 Endpoint 是否相同由部署决定。
- 两个域共用服务级 `AuthRevocationManager`，撤销身份仍以 `AuthType` 区分。
- bootstrap 是管理后台启动契约，**只检查 Manage 域**。即使 `Auth.CasDoor.Enable=true`，只要 `ManageAuth.CasDoor.Enable=false`，`ui.show_login` 仍不会因此变为 `true`。

因此返回 `show_login=false, show_logout=false, show_test_identity=true` 时，含义是当前权威服务的 **Manage 域**被判定为 `test_token`，不是前端漏显示登录，也不能据此判断 Auth 域是否启用。要测试 Manage Casdoor，必须在创建权威服务 `ServiceContext` **之前**配置 `ManageAuth.CasDoor.Enable=true`、有效的 `YamlFilePath`、独立的 `WebhookSecret` 和撤销配置，然后重启服务。`ModifyConfig` 虽能校验、保存并替换 `context.Config`，但不会重建已经创建的 Casdoor Client 或 `AuthRevocationManager`，不能用它热启停 Casdoor。

`ManageAuthAuthorityService` 的边界同样仅限 HTMLServer：

- 单业务 Manage 服务自动成为开发视图权威；多个业务 Manage 服务在启用 ViewPort 时必须显式指定。
- HTMLServer 初始化时会把权威服务的 `ManageAuth` 传播给同进程业务服务，并让聚合后的 Manage/ServerManage 页面校验权威服务签发的 Manage Token；Private Auth 与 ServerManageAuth 仍是独立认证域。
- `ViewPort=0` 时该选择和传播完全跳过。正式拆分部署的各服务必须自行具备正确的认证配置，不能依赖 `ManageAuthAuthorityService` 修改其运行时。
- Casdoor Client 在 `ServiceContext` 创建时装配，早于 HTMLServer 权威传播。若某服务需要通过自己的正式端口处理 Casdoor，它必须在创建 `ServiceContext` 前就有完整配置；权威传播不能给已经初始化的服务补建 Client。

HTMLServer 为多服务同源开发视图在认证 URL 上追加 `service=<服务名>`，只用于其代理选择目标 `ServiceContext`：

- bootstrap 中的 `acquire_token`、`casdoor_config`、`refresh` 可能带 `service`；Casdoor 配置返回的 callback 也可能带该参数。
- `service` 不是 OAuth/Casdoor 标准参数，不是身份或授权凭据；`CasdoorCallback.Parse` 实际只读取 `code`、`state`、`type`。
- 正式直连某个业务服务端口时使用该服务自动注册的 `/api/casdoor?type=auth|manage`、`/api/casdoor/callback?type=auth|manage&code=...&state=...`、`/api/refresh` 和 `/api/casdoor/webhook?type=auth|manage`，不依赖 `service` 路由参数。

## Casdoor 认证生命周期

- 前端先调用 `/api/casdoor?type=auth|manage` 获取对应 Casdoor 域配置和 `background_callback_url`；回调固定为 `/api/casdoor/callback`，不要再调用已删除的 `/api/callback`。
- Auth 与 Manage 分别配置 Casdoor YAML、Client、Access/Refresh Secret 和 Webhook Secret，任何 Secret 都不得复用。框架通过 ServiceContext 持有 DomainClient，不使用 Casdoor 全局 SDK。
- `casdoor.NewAuthHandler` 仅为公共 API 兼容保留并固定 fail closed，不得自行挂载。`TokenParseWithClient` 仅能解析原始 Casdoor JWT，解析成功不等于授权成功；生产请求必须经过 ServiceContext 注册的 Access Token、认证域、撤销世代和业务 Hook 完整链路。
- Callback 在线读取 Casdoor 用户并验证 Owner、Subject、`IsForbidden`、`IsDeleted`，随后以撤销权威当前世代签发 Access/Refresh。被 logout 的用户只有再次通过在线 Callback 才能解除 blocked，旧 Token 仍因世代落后而失效。
- `/api/refresh` 不访问 OAuth，但必须验证 Refresh 用途、AuthType、Provider、Subject、Generation 和当前在线用户状态；Auth Token 不能访问 Manage，Manage Token 不能访问 Private。
- `/api/casdoor/webhook?type=auth|manage` 使用对应域独立 Bearer Secret。Webhook 是控制面，不记录 Header/Payload，成功仅表示撤销事实已持久化且控制事件被 EventBridge 接受。
- 服务可选实现 `IAuthHookProvider`（签名前）、`IAuthRequestHookProvider`（验签及撤销校验后、Router 前）和 `ICasdoorEventHookProvider`（撤销事实提交后的异步业务通知）。只有类型化 `PublicError` 可向前端公开安全业务消息，普通错误统一 500 脱敏。
- local 模式使用 Badger，适合单实例。shared 模式使用 Redis 权威且必须启用 MQ `event-stream`；Redis/EventBridge 故障时认证面 fail closed，Public REST 保持可用。
- WebSocket 登录与每次认证订阅都重新验证 Access Token 和撤销权威；更高世代、blocked 事件或共享权威不可用会关闭旧 Casdoor 连接。

验证命令：

```bash
./scripts/test.sh security
CORE_TEST_REDIS_ADDR=127.0.0.1:6379 ./scripts/test.sh integration-casdoor-auth
```

