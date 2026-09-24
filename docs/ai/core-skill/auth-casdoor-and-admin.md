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
- local 模式使用 Badger，适合单实例。shared 模式使用 Redis 权威且必须启用内置 MQ `event-stream`，身份通知由专用原生广播桥装配。HTTP 始终查询权威，不因通知单独断线改用快照或拒绝权威仍健康的请求；权威故障 fail closed，Public REST 保持可用。
- shared Casdoor WebSocket 从登录开始（包含无订阅会话）每 1 秒查权威，单次 3 秒预算、每 Manager 至多 64 个检查在途，5 秒 watchdog 防停滞。通知断线/恢复代次变化、漏撤销或无法确认时关闭旧会话；不恢复旧身份，不影响其他认证域。完整条件、容量与维护窗口迁移见 [内部通知标准](../../codex/CORE_INTERNAL_NOTIFICATION_LIFECYCLE_GUIDE.md)。不得将原生通知接受等同于每个副本已完成撤销。
- WebSocket 登录与每次认证订阅都重新验证 Access Token 和撤销权威；更高世代、blocked 事件或共享权威不可用会关闭旧 Casdoor 连接。

## Manage RoleCode RBAC

标准 `run.WebServer` 会自动启用 Core Manage RBAC。Core 在 Casdoor Manage callback 与 refresh 签发 Access Token 前，用可信 `AuthIdentity` 查询共享 `ManageStore` 中的管理员主体与角色关系；Provider 只返回标准 `[]ManageRoleRef`，不返回权限明细：

```go
type IManageRoleProvider interface {
	ResolveManagePrincipal(context.Context, ManagePrincipalRequest) (ManagePrincipal, error)
}
```

Core 用稳定 `RoleCode` 绑定管理员主体，不保存角色表数据库 ID。Token 的保留 claim `manage_roles` 只保存规范化 RoleCode JSON 数组；不保存菜单、path、command、权限列表、权限哈希或数据库 ID。角色关系改变后，用户需 refresh 或重新登录取得新 token；角色权限明细由 Core 在请求时读取，改变后下一请求生效。

Core 内置并保护两个动态角色：

| RoleCode | 行为 |
| --- | --- |
| `core.system_admin` | 允许全部 Manage command |
| `core.viewer` | 只允许精确 `view`、`search` |

自定义角色固定使用 `explicit` 策略，权限由 `ManageRolePermissionModel` 的 `RoleCode + Service + Path + Command` 行保存。第一版不支持自定义默认角色；`core.viewer` 是唯一默认角色。角色 Code 创建后不可变，用户关系和权限关系都不得改用数据库 ID 或逗号分隔字符串。

Manage REST 的固定顺序是：

```text
Access Token 验签与认证域隔离
→ Casdoor 撤销权威
→ Core Manage RBAC
→ 业务 IAuthRequestHookProvider
→ Router Parse / Validation / Do
```

授权目标来自已注册 `RouterInfo` 的稳定 `service + path + command`。标准与自定义 command 都在 Router 前覆盖；不允许在 `ManageService.ValidationBefore`、`DoBefore` 或前端按钮隐藏中另造这层授权。没有权限返回 HTTP 403、公开码 `40300`、安全消息 `permission denied`，Router 和业务 Hook 都不执行。权限查询错误返回安全 500，不泄露数据库原文。

标准 WebServer 中 claim 缺失/非法、主体 Provider 或权限存储异常都 fail closed。升级前签发、尚未携带 `manage_roles` 的旧 Manage Access/Refresh Token 必须重新登录，不能继续按历史“认证即放行”行为使用。多服务 HTMLServer 使用选定的 Manage 认证权威服务签发身份，但仍按目标服务 RouterInfo 鉴权。

TestToken 的 Manage 身份由 Core 直接赋予 `core.system_admin`，不会创建管理员主体，也不会占用真实 Casdoor 首用户。Core 只在 TestToken 的 Refresh Token 中写入已签名内部标记，以便刷新时继续识别测试身份。第一个真实 Casdoor Manage callback 由 Core 在同一事务内创建主体并绑定 `core.system_admin`，后续新主体绑定 `core.viewer`；跨进程竞争由 `ManagePrincipalModel.BootstrapSlot` 的 nullable unique 约束仲裁，进程锁不是最终保障。首位管理员主体不能停用、删除，其引导产生的 `core.system_admin` 关系也不能解绑，避免控制面永久锁死。refresh 不创建未知主体，Casdoor 角色也不继承或同步到 Core。

`IManageRoleProvider` 不是请求期 Hook：它只在 Casdoor Manage callback/refresh 签发 Access Token 前执行，普通 Manage API 请求不会调用它。标准 WebServer 已绑定 Core 默认实现；消费方只有在角色权威位于外部 IAM、必须自定义身份到 RoleCode 的映射时才实现该接口覆盖默认 Provider。自定义 Provider 仍只能返回角色，不能返回或缓存 path/command 权限。签发顺序是业务 `IAuthHookProvider.OnAuth` 先执行，再解析 Manage RoleCode，最后签名 Token；不能等待可能晚到的 `ICasdoorEventHookProvider` signup webhook。

管理员主体、主体角色关系、角色和权限都属于 Core 控制面。消费方不得再建立 `AdminUserModel`、`AdminUserRoleModel`、管理员 repository/store、专用 `AdminService` 或 `ConfigureManageModels` 桥接。标准启动只有：

```go
server := run.NewWebServer() // 读取 server.json，创建控制面存储并初始化 Core 系统表
server.AddIService(NewBusinessService())
server.Start()
```

`NewWebServer()` 在监听前根据 `server.json.ManageStore` 同步初始化目录、菜单、按钮、角色、角色权限、管理员主体和主体角色关系七类表，并绑定默认 Provider/Authorizer。启动还会验证首管理员 `BootstrapSlot` 列及唯一索引确实存在；约束缺失或建索引失败必须终止启动，不能在无跨进程仲裁能力时继续服务。不要把 `IDataAction` 沿 `WebServer → SystemManage → 具体 Manage → DmpBase` 逐层注入，也不要增加 `New...ManageWithAction`、`NewAdminService(repository...)`、消费方管理员表或请求期建表逻辑。

新生成的 `server.json` 默认使用 SQLite `core_manage`。升级时若旧 `server.json` 完全没有 `ManageStore` 字段，Core 继续读取历史 `models` SQLite 库，避免目录和菜单看似丢失；需要切换到 `core_manage` 或 MySQL 时必须显式配置并自行迁移既有数据。一个进程只能绑定一个控制面数据库，后续尝试绑定不同数据库会在启动时失败，不能让 CRUD 与鉴权各自读取不同存储。

Core `SystemManage` 提供角色、角色权限、管理员主体和主体角色绑定页。“绑定菜单默认权限”只为自定义角色幂等创建该菜单的 `view`、`search` 两条权限。当前版本不裁剪菜单或按钮；用户可以点击未授权 command 并看到 403，后端结果才是授权权威。完整启动、RoleCode 绑定与 HTTP 验证见 `examples/09-admin-manage-rbac`。

## 可选 HMAC 请求认证

服务可选实现 `types.IHMACAuthProvider`，用于在没有 Bearer 的 **Auth 用户域**验证请求签名。未实现时 Private REST 与用户 WebSocket 继续只认框架 Access Token，Manage 与 ServerManage 无论是否携带 HMAC Header 都不进入该 Hook。

### 默认行为与按服务启用

**服务开启 `Auth` 后的默认凭证方式仍是 Core 原有 Access Token（TestToken/JWT/Casdoor 换取后的 Bearer），HMAC 不会因为 `Auth` 开启而自动启用。**

Core 故意不提供全局 `HMACAuth.Enable`。HMAC 的启用开关是“具体服务实例是否显式实现 `IHMACAuthProvider`”：

| 服务状态 | Private Auth 凭证行为 |
| --- | --- |
| 只配置 `Auth`，未实现 `IHMACAuthProvider` | 只认原有 Bearer Access Token |
| 配置了 `HMACAuth` Header/`MaxInFlight`，但未实现 Provider | 仍只认 Bearer；`HMACAuth` 配置本身不是启用开关 |
| 该服务显式实现 `IHMACAuthProvider` | 该服务的 Auth Private REST/用户 WebSocket 才增加 HMAC 备选 |
| 请求已带非空 Bearer | 只验证 Bearer；即使 Bearer 无效也不降级尝试 HMAC |
| Manage / ServerManage | 始终使用原有域凭证，从不调用 HMAC Provider |

因此多服务工程必须在“确实需要 API Key/HMAC”的服务类型上实现该接口；其他服务不实现，保持原凭证行为。同一进程中各 `ServiceContext` 独立发现 Provider，不会因某一个服务启用 HMAC 而全局开启。

### 消费方最小接入

Provider 由已注册的业务服务实现；Core 在创建 `ServiceContext` 时通过可选接口自动发现，不需要手工向 Router 或 middleware 注册 Hook。

```go
func (s *UserService) AuthenticateHMAC(
	ctx context.Context,
	args types.HMACAuthArgs,
) (*types.HMACAuthResult, error) {
	// 业务服务负责：查找凭证、验签、时间窗、nonce 原子占用、撤销和权限。
	credential, user, err := s.verifyAPIKey(ctx, args)
	if err != nil {
		return nil, err
	}
	result := &types.HMACAuthResult{
		Identity: types.AuthIdentity{
			UID:             user.ID,
			Username:        user.Name,
			AuthType:        types.AuthTypeUser,
			Provider:        "apikey",
			ProviderSubject: credential.ID, // 稳定凭证 ID，不是原始 AccessKey
		},
		Claims: map[string]string{"platform_uid": user.ID},
	}
	if err := safe.ValidateHMACAuthClaims(result.Claims); err != nil {
		return nil, err
	}
	return result, nil
}
```

Header 可保留 Core 中性默认值，也可在创建 `ServiceContext` **之前**按产品协议覆盖：

```yaml
HMACAuth:
  AccessKeyHeader: X-Access-Key
  TimestampHeader: X-Timestamp
  NonceHeader: X-Nonce
  SignatureHeader: X-Signature
  RecvWindowHeader: X-Recv-Window
  MaxInFlight: 64
```

`MaxInFlight` 限制单个 `ServiceContext` 内 REST 请求体预处理和 REST/WebSocket Provider 的总在途数，是构造期配置，运行中修改不会重建信号量。产品自定义 Header 和签名串只写在消费方契约，不要写成 Core 默认。

### `HMACAuthArgs` 输入映射

| 字段 | Private REST | WebSocket HMAC logon |
| --- | --- | --- |
| `AccessKey` | 配置 Header 值去首尾空白 | logon `data.apiKey` |
| `Timestamp` | 配置 Header 原始字符串去首尾空白 | `data.timestamp` 的 `int64` 十进制字符串 |
| `Nonce` / `Signature` / `RecvWindow` | 对应配置 Header 值去首尾空白 | 对应 logon JSON 字段；`recvWindow` 可选 |
| `Method` | 冻结后 `RouterInfo.Method` | 固定 `WS` |
| `Path` | 冻结后 `RouterInfo.Path` | 固定 `/ws` |
| `Query` | `r.URL.RawQuery` 原样传递，Core 不排序、不删字段 | 空字符串 |
| `BodyHashHex` | 原始请求体字节的 SHA-256 小写 hex；空 body 哈希空字节 | SHA-256 空字节的小写 hex |
| `ClientIP` | `TrustedProxies` 规则下的请求来源 IP | WebSocket upgrade 请求的同样解析结果 |
| `TraceID` | 复用或生成当前 HTTP 请求 TraceID | 复用 upgrade 请求 `X-Trace-Id`；缺失时生成 UUID |
| `PathType` | 冻结后 `RouterInfo.PathType`，该分支必须为 Auth 用户域 | 固定 `types.PrivateType` |

同一 Provider 同时支持 REST 和 WebSocket 时，应在产品协议中根据 `Method`/`PathType` 明确两种 canonical payload；不得假设 WebSocket 有 REST body/query，也不得把上表所有字段当成 Core 强制的签名顺序。

- Core 只从 `ServerConfig.HMACAuth` 配置的中性 REST Header 提取 AccessKey、Timestamp、Nonce、Signature 和可选 RecvWindow；REST 不从 query 提取、移除或规范化凭证，客户端不得用 query 提交 HMAC 凭证，WebSocket 只从 logon JSON 提取。`HMACAuthArgs.Query` 原样保留业务 `RawQuery`，签名串排序仍由消费方负责。
- Core 在确认 Provider 存在并预留 `MaxInFlight` 名额后，才有界、有超时地读取并恢复请求体，按原始字节计算 SHA-256；HMAC 分支即使挂到 `NewExternalRouterHandler` 也受 `ServerConfig.MaxBytes` 硬限制。Core 不持有 HMAC Secret、不实现算法、不存 nonce，也不读取业务权限。
- Provider 成功返回 `HMACAuthResult`：UID、AuthType、非 Casdoor Provider 和稳定 `ProviderSubject` 必须完整；`ProviderSubject` 应是凭证记录 ID，禁止放原始 AccessKey。业务 Claims 可先调用 `safe.ValidateHMACAuthClaims` 预检查，不得使用 `uid`、`uname`、`auth_type`、`token_use`、`iat`、`exp`、`auth_provider`、`provider_subject`、`auth_generation`、`auth_authority_service`、`args`、`secret_args` 保留键。
- Core 在 `safe.BuildHMACAccessIdentity` 统一构造可信身份，随后复用 JWT 相同的 verified context、请求授权链和 `OnAuthRequest`；非 Casdoor 的 HMAC 身份不进入 Casdoor 世代校验。Bearer 始终优先，只要存在非空 Bearer，就不降级尝试 HMAC。
- 当服务实现 `IHMACAuthProvider` 时，`/api/openapi` 为 Private 操作生成 Bearer OR HMAC 安全要求，并通过 `x-core-hmac-auth` 公开实际 Header、Core 会交给 Provider 的可用字段和 Bearer 优先规则；签名算法、字段选择与规范化均由 Provider 定义。仅 `ServerOption.IsWebSocket=true` 时，顶层 `x-core-websocket-hmac-logon` 才公开 `event=sub`、`channel=logon` 的 `data_schema`。未实现 Provider 时不生成这些契约。
- HMAC 凭证拒绝统一返回 `authentication failed`，不得向未认证调用方透出 Provider 的业务消息。日志只记录 `hmac_access_denied` 与凭证摘要，不记录 AccessKey、Signature、Nonce、RawQuery 或 body。
- HMAC Hook 使用 ServiceContext 级 `MaxInFlight` 名额、服务 Timeout、panic 隔离并 fail closed；REST 从读 body 到 Provider 返回全程占用同一名额。Provider 必须响应请求、WebSocket 会话和 ServiceContext 关闭的 `ctx` 取消；服务停止时 Core 先禁止新调用、取消存量 Hook，再在生命周期超时内等待。忽略取消的调用会继续占用 Hook 槽位直到真正返回，防止超时 goroutine 无界堆积；关闭等待超时会记入 `ShutdownError`。
- WebSocket HMAC 只在 logon 调用一次，订阅不重放签名或 nonce；每次认证订阅仍校验目标域必须为 Auth 用户域并继续执行 `OnAuthRequest`。会话到期时间不晚于 `Auth.AccessExpire`，Provider 返回更早的 `Identity.ExpiresAt` 时取更早值；到期先标记失效并关闭连接，再清理订阅。断连必须取消进行中的 HMAC Hook、停止到期计时器并释放缓存身份；重新登录一律清理旧的认证订阅，避免同 UID 降权后仍保留旧授权。
- HMAC logon 成功后立即清除 SessionRequest 中的 Signature、Nonce、Timestamp、RecvWindow，并只保留掩码后的 ApiKey、可信身份、Claims 和到期时间。

验证命令：

```bash
./scripts/test.sh security
CORE_TEST_REDIS_ADDR=127.0.0.1:6379 ./scripts/test.sh integration-casdoor-auth
```

消费方至少还要测试：Bearer 优先且不调 Provider、无 Provider 仍为 401、坏签名/重放/Provider 超时 fail closed、Manage 不调 Provider、日志无 AccessKey/Signature/body，以及 WebSocket HMAC logon 后的 Private 订阅不再重放 nonce。
