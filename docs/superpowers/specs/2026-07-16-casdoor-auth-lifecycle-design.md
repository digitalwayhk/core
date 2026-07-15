# Casdoor 认证生命周期如何保持持续有效

> 状态：已完成方案确认，等待书面规格复核
> 日期：2026-07-16
> `meta.contentType`：`Conceptual`
> 范围：`pkg/server/api/public`、`pkg/server/config`、`pkg/server/router`、`pkg/server/safe`、`pkg/server/trans/rest`、`pkg/server/trans/websocket`、`pkg/server/types`

本文定义 Casdoor 登录、内部令牌签发、请求前授权、Webhook 撤销和多节点同步的完整生命周期。设计目标是让 Casdoor 用户登出、禁用、删除或权限变化后，框架及时拒绝旧 Access Token 和 Refresh Token，同时允许服务实现自己的账户、租户和风控规则。

## 1. 背景和问题

现有 Auth Hook 只覆盖内部 JSON Web Token（JWT）签发前的业务检查。Callback 首次登录和 Refresh 换发 Access Token 时会调用 `IAuthHookProvider.OnAuth`，但 Token 验证成功后、业务 API 执行前没有统一授权 Hook。

现状还存在以下缺口：

- Casdoor 用户在首次登录后被禁用，内部 Refresh Token 仍可换取新 Access Token
- 已签发 Access Token 在到期前无法响应 Casdoor 登出或禁用事件
- Private 和 Manage 可以配置不同 Casdoor，但当前全局 `casdoorsdk.InitConfig` 使后初始化配置覆盖前一配置
- 服务无法在每次认证请求前统一检查账户冻结、租户状态和风控限制
- WebSocket 建立后没有统一的身份撤销关闭链路
- `futures` 的 `UserInfoHook` 混合 Casdoor 协议、用户服务调用、缓存预热和推荐码业务，不适合直接下沉到框架

本设计保留现有签发 Hook，并新增请求前授权 Hook、Casdoor 事件 Hook、双 Casdoor Client 和认证世代存储。

## 2. 目标和非目标

### 2.1 目标

本次实现必须完成以下能力：

1. 分别使用 `Auth.CasDoor` 和 `ManageAuth.CasDoor` 验证用户与管理员
2. 在 Callback 和 Refresh 签发内部 Token 前在线确认 Casdoor 身份仍有效
3. 在 Token 验证成功后、Router 执行前调用服务请求授权 Hook
4. 通过 Casdoor Webhook 近实时撤销旧 Access Token 和 Refresh Token
5. 使用 Badger 保证单节点重启后仍保留撤销状态
6. 使用 Redis 保证水平扩展时的权威认证世代和原子递增
7. 使用每个服务默认存在的 EventBridge 分发可靠撤销控制事件
8. 在撤销后关闭该身份已有的 WebSocket 会话
9. 保留 `/api/casdoor` 配置入口，并由其向前端返回新的 `/api/casdoor/callback` 路径
10. 通过一次密钥轮换淘汰缺少新 Claims 的旧 Token

### 2.2 非目标

本次不实现以下能力：

- 不把 Casdoor 用户主数据复制成框架用户模型
- 不把业务资源所有权检查移入全局请求 Hook
- 不在每个 API 请求中远程访问 Casdoor
- 不复用 OAuth `ClientSecret` 作为 Webhook Secret
- 不实现通用身份提供方插件市场
- 不用进程级可变全局单例保存 Casdoor Client 或撤销状态
- 不把 `futures` 登录预热、推荐码和用户服务调用移入框架
- 不为水平扩展提供无 Redis 的弱一致性降级模式

## 3. 已选方案

### 3.1 方案比较

评估过三种方案：

1. **框架安全检查与服务业务授权分层（采用）**：框架验证 Casdoor 身份、Token 语义和认证世代；服务 Hook 检查业务账户、租户和风控状态
2. **服务 Hook 承担全部检查（拒绝）**：改动较少，但服务遗漏接口时会默认放过已禁用身份
3. **框架只维护 Casdoor 黑名单（拒绝）**：能处理禁用和登出，但无法统一表达账户冻结、租户停用和业务风控

分层方案保证框架安全能力默认生效，同时保留服务级业务扩展。

### 3.2 所有权关系

```text
ServiceContext
├── AuthHookProvider
├── AuthRequestHookProvider
├── CasdoorEventHookProvider
├── CasdoorClients
│   ├── auth
│   └── manage
├── AuthRevocationManager
│   ├── Badger local store
│   ├── Redis authority, optional in single-node mode
│   └── ServiceEventBridge
└── RouteWebSocketHub
```

`ServiceContext` 是上述组件的唯一所有者。组件随服务初始化和关闭，不得共享进程级可变实例。

## 4. Casdoor 公共 API

### 4.1 Go 类型和 URL

框架在 `pkg/server/api/public` 中提供三个 Casdoor API：

| Go 类型 | 方法和 URL | 职责 |
| --- | --- | --- |
| `CasdoorConfig` | `GET /api/casdoor` | 返回前端发起 OAuth 所需的公开配置 |
| `CasdoorCallback` | `GET /api/casdoor/callback` | 使用 OAuth Code 获取 Casdoor Token，再签发内部 Token |
| `CasdoorWebhook` | `POST /api/casdoor/webhook?type=auth|manage` | 接收并标准化 Casdoor 身份事件 |

现有 `Casdoor` 和 `Callback` Go 类型保留为废弃别名。旧 `/api/callback` URL 不再保留；前端不得硬编码 Callback 路径，必须读取 `/api/casdoor` 响应中的 `BackgroundCallbackURL`。该字段返回 `/api/casdoor/callback`，并继续由前端按当前登录域传递 `type=auth|manage`。

### 4.2 Callback 路径发现

`CasdoorConfig` 根据 `type` 返回对应 Casdoor 的公开配置。响应中的 `BackgroundCallbackURL` 是 Callback 路径的唯一现行来源。前端使用返回值发起后端 Callback 请求，因此路由迁移不要求兼容旧 `/api/callback`。

配置响应不得根据 Host、Forwarded Header 或客户端输入拼接不可信绝对 URL。默认返回服务内相对路径，由部署层和前端使用已知站点 Origin 组成完整地址。

### 4.3 Webhook 认证域选择

Webhook 共用一个 RouterInfo。查询参数 `type` 只能选择预先存在的认证域：

- `type=auth` 选择 `Config.Auth.CasDoor`
- `type=manage` 选择 `Config.ManageAuth.CasDoor`
- 缺失或其他值返回统一请求错误

框架确定认证域后，只验证该域的 Webhook Secret，不尝试另一套密钥。Payload 的组织和应用必须与所选配置一致。

## 5. 三类服务 Hook

### 5.1 Token 签发 Hook

现有接口保持兼容：

```go
type IAuthHookProvider interface {
    OnAuth(ctx context.Context, args *AuthHookArgs) error
}
```

Callback、Refresh 和 TestToken 在内部 Token 签名前调用该接口。服务可以拒绝签发或向 Access Token 注入业务 Claims。Refresh Token 不保存业务 Claims，刷新时重新执行 Hook。

### 5.2 请求前授权 Hook

新增接口在 Token 验证成功后、业务 Router 执行前调用：

```go
type IAuthRequestHookProvider interface {
    OnAuthRequest(ctx context.Context, args AuthRequestArgs) error
}
```

`AuthRequestArgs` 按值传递。它只包含可信身份和不可变请求元数据：

- `UID`、`Username`
- `AuthType`、`Provider`、`ProviderSubject`
- `IssuedAt`、`ExpiresAt`、`AuthGeneration`
- `ServiceName`、`Path`、`Method`、`PathType`
- `ClientIP`、`TraceID`
- 只读 Claims 副本

Hook 不接收请求 Body、可变 `IRequest`、响应对象或可变 `RouterInfo`。资源所有权和请求参数授权继续由具体 API 的 `Validation` 或 `Do` 处理。

Hook 返回错误时，框架停止请求。服务可以返回 `types.NewPublicError`，由现有 `ResolvePublicError` 契约将明确声明可公开的 HTTP 状态、业务错误码和安全消息返回前端。例如账户冻结可以返回 `403` 和“账户已冻结”，业务前置条件不满足可以返回 `422` 和对应安全提示。

普通 `error`、Hook panic、超时和未分类依赖错误仍 fail closed，并对外返回通用内部错误。框架只记录脱敏后的内部诊断信息，不把任意 `err.Error()`、调用栈或依赖错误直接返回前端。

```go
return types.NewPublicError(
    types.ErrorKindForbidden,
    40321,
    "账户已冻结",
    internalErr,
)
```

上述公开错误规则同时适用于现有 `IAuthHookProvider.OnAuth`。Callback、Refresh 和 TestToken 的 Hook 可以向前端返回服务明确声明的安全错误；未包装的错误继续使用通用脱敏响应。

### 5.3 Casdoor 事件 Hook

新增接口接收已经通过框架验证和持久化的标准事件：

```go
type ICasdoorEventHookProvider interface {
    OnCasdoorEvent(ctx context.Context, event CasdoorEvent) error
}
```

EventBridge 订阅回调异步调用该接口。服务可以更新业务用户状态、清理缓存或触发业务审计，但不能决定框架是否执行基础 Token 撤销。

### 5.4 ServiceContext 自动注册

`NewServiceContext` 和 `NewServiceContextWithConfig` 检查服务注册对象是否实现上述接口。未实现请求 Hook 或事件 Hook 不影响框架自己的身份验证和撤销能力。

## 6. 双 Casdoor Client 隔离

### 6.1 独立 Client

框架使用 `casdoorsdk.NewClient` 为 `auth` 和 `manage` 分别创建 Client。禁止认证运行时调用全局 `casdoorsdk.InitConfig`、`GetOAuthToken` 或 `ParseJwtToken`。

```text
AuthTypeUser   -> Config.Auth.CasDoor client
AuthTypeManage -> Config.ManageAuth.CasDoor client
```

每个 Client 使用自己的 Endpoint、Client ID、Client Secret、Certificate、Organization 和 Application。初始化任一已启用 Client 失败时，所属认证域 fail closed。

### 6.2 身份状态验证

框架内部验证器执行以下检查：

- Casdoor 用户存在
- `Enabled` 为允许状态
- `IsForbidden` 为 `false`
- `IsDeleted` 为 `false`
- 用户组织与当前 Client 配置匹配

验证器由框架根据配置创建并注入 `ServiceContext`。它不是服务必须实现的消费方接口。测试可以注入假验证器，避免访问真实 Casdoor。

## 7. 内部 Token 契约

### 7.1 新增 Claims

Access Token 和 Refresh Token 都必须包含以下受签名保护的 Claims：

```json
{
  "uid": "user_id_1234567890123",
  "uname": "user_name",
  "auth_type": "auth",
  "auth_provider": "casdoor",
  "provider_subject": "casdoor_user_name",
  "auth_generation": 7,
  "token_use": "access",
  "iat": 1784131200,
  "exp": 1784138400
}
```

`provider_subject` 使用 Casdoor 稳定用户名或经验证的稳定主题标识。业务 `uid` 可以继续使用 Casdoor 用户 ID。框架不得根据显示名查询用户状态。

### 7.2 认证世代

认证世代是按身份单调递增的无符号整数。Token 中的世代必须等于权威存储中的当前世代，否则 Token 失效。

撤销键格式为：

```text
{service}:{auth_type}:{auth_provider}:{provider_subject}
```

该格式确保用户和管理员的撤销状态隔离。不同服务也不会共享业务认证状态。

### 7.3 旧 Token 迁移

上线时轮换以下四个密钥：

- `Auth.AccessSecret`
- `Auth.RefreshSecret`
- `ManageAuth.AccessSecret`
- `ManageAuth.RefreshSecret`

旧 Access Token 和 Refresh Token 全部失效。新认证中间件拒绝缺少 Provider、ProviderSubject 或 AuthGeneration 的 Casdoor 内部 Token，不提供自然过期兼容窗口。

## 8. 撤销存储和多节点同步

### 8.1 单节点模式

单节点使用独立前缀的 Badger 数据库存储：

- 当前认证世代
- 身份阻断状态
- 已处理 Webhook 事件
- 事件处理阶段和失败信息摘要

Badger 写入成功后才允许 Webhook 返回成功。服务重启后必须恢复撤销状态，不能让旧 Token 重新有效。

### 8.2 水平扩展模式

水平扩展使用 Redis 作为权威存储：

- 使用原子递增更新认证世代
- 使用条件写入保证事件幂等
- 使用独立命名空间隔离服务和认证域
- Badger 只保存本节点最后确认的权威状态
- Badger 不向 Redis 异步回写较旧状态

启用水平扩展和 Casdoor 认证但未配置 Redis 时，服务启动失败。Redis 不可用时，Callback、Refresh、Private、Manage 和新 WebSocket 订阅全部 fail closed。

### 8.3 EventBridge 控制事件

认证撤销属于可靠控制事件。Webhook 更新权威存储后，将标准化事件提交给服务专属 EventBridge：

```text
auth.casdoor.identity.changed
```

事件包含服务名、认证域、Provider、ProviderSubject、UID、事件类型、目标世代和事件 ID，不包含 Token、Secret 或完整原始 Payload。

其他节点收到事件后执行以下操作：

1. 单调更新本地已确认世代
2. 更新身份阻断状态
3. 关闭旧世代 WebSocket 会话
4. 异步调用服务 `OnCasdoorEvent`

迟到事件不得回退本地世代。重复事件不得再次递增世代。

## 9. Webhook 处理契约

### 9.1 独立密钥

`Auth.CasDoor` 和 `ManageAuth.CasDoor` 分别增加 `WebhookSecret`。配置校验必须保证：

- Webhook Secret 非空
- 两套 Webhook Secret 不相同
- Webhook Secret 不等于 Client Secret
- Webhook Secret 不等于任一 Access Secret 或 Refresh Secret
- 生产 Casdoor Endpoint 使用 HTTPS
- 部署文档要求在 Casdoor 控制台登记 HTTPS Webhook URL；框架不声称能够读取或验证该外部配置

Casdoor 通过 `Authorization: Bearer <secret>` 发送固定认证 Header。框架使用常量时间比较验证密钥，不记录 Header。

### 9.2 请求边界

Webhook 在解析业务字段前执行以下边界检查：

1. 限制请求 Body 大小
2. 校验 Content-Type
3. 校验 `type` 白名单
4. 验证对应 Webhook Secret
5. 解析允许字段
6. 校验组织、应用、用户标识和事件类型

认证失败返回统一 `401`。格式错误返回统一 `400`。内部存储或 EventBridge 不可用返回统一 `503`。

### 9.3 幂等和乱序

幂等键包含服务、认证域和 Casdoor 事件 ID。Casdoor 未提供稳定事件 ID 时，框架使用经过规范化的认证域、事件类型、用户标识、事件时间和允许字段生成摘要。

幂等记录的保留时间不得短于当前最大 Refresh Token 有效期。重复事件返回成功，但不重复递增世代。事件带有可比较顺序时，旧事件不能覆盖新阻断状态。

### 9.4 撤销事件

以下事件递增认证世代：

- `logout`
- `sso-logout`
- `update-user`
- `delete-user`
- `unlink`
- 用户禁用
- 用户禁止

`update-user` 无论修改资料还是权限，都要求重新登录或刷新身份。这一规则保证 Access Token 中的业务 Claims 不会在权限变化后继续使用。

`login` 和 `signup` 不递增世代。在线身份验证通过后，新 Token 使用当前世代，因此登出后的重新登录可以恢复访问。

### 9.5 成功响应时机

Webhook 只有在以下步骤全部完成后才返回 `2xx`：

1. 幂等事件持久化成功
2. 认证世代和阻断状态更新成功
3. EventBridge 接受可靠控制事件

服务 `OnCasdoorEvent` 由 EventBridge 异步执行，不阻塞 Casdoor 请求。业务处理失败按可靠事件策略重试，不重复执行框架世代递增。

## 10. 请求认证和授权链路

### 10.1 REST 中间件顺序

REST 认证路由按以下顺序处理：

```text
Security headers
  -> External rate limit
  -> go-zero JWT signature and expiry validation
  -> Internal token semantic validation
  -> Revocation authority check
  -> Service OnAuthRequest
  -> Trusted proxy and IP allowlist checks
  -> Request parse and RouterInfo.Exec
  -> Public error response
```

框架继续复用 go-zero JWT 中间件完成签名和到期时间验证。新增语义层对内部 Token 严格检查 `token_use` 和 `auth_type`；对 Casdoor 换发的内部 Token 额外检查 Provider、ProviderSubject 和 AuthGeneration，并构造只读 `AuthRequestArgs`。Logto 等其他提供方使用各自经过验证的身份上下文，不执行 Casdoor 世代检查。

### 10.2 调用范围

请求 Hook 的调用范围如下：

| 请求类型 | 是否调用 `OnAuthRequest` |
| --- | --- |
| Private REST | 是 |
| Manage REST | 是 |
| 已认证 WebSocket 新订阅 | 是 |
| Public REST | 否 |
| Casdoor Callback | 否，使用在线验证和 `OnAuth` |
| Refresh | 否，使用在线验证和 `OnAuth` |
| TestToken | 否，只使用 `OnAuth` |

其他身份提供方通过框架认证后也可以执行请求 Hook，但 Casdoor 世代检查只处理带有 Casdoor Provider 的内部 Token。

### 10.3 失败语义

Token 无效、缺少 Claims、世代不一致或权威状态不可用时返回统一 `401`。Token 合法但服务业务状态拒绝时返回统一 `403`。

日志只记录稳定事件名、服务、认证域、路由和脱敏身份摘要。日志不得记录 Token、Secret、完整 Claims、Header、Payload 或 Hook 原始错误。

## 11. WebSocket 撤销

认证 WebSocket 会话必须记录以下不可变身份索引：

- ServiceName
- AuthType
- Provider
- ProviderSubject
- UID
- AuthGeneration

新订阅先执行与 REST 相同的语义验证、世代检查和 `OnAuthRequest`。EventBridge 收到更高世代或阻断事件后，`RouteWebSocketHub` 关闭该身份旧世代的全部会话，并清理订阅表。

WebSocket 只面向最终外部用户。内部服务通信继续使用 TransportSelector 和 EventBridge，不通过 WebSocket 传播 Casdoor 控制事件。

## 12. 生命周期和故障策略

### 12.1 初始化

`ServiceContext` 按以下顺序初始化认证组件：

1. 校验 Auth 和 ManageAuth 配置
2. 创建独立 Casdoor Client
3. 创建 Badger 撤销存储
4. 按水平扩展配置连接 Redis
5. 创建 AuthRevocationManager
6. 注册 EventBridge 控制事件处理器
7. 捕获服务 Hook Provider
8. 注册 REST 和 WebSocket 路由

任一必需组件初始化失败时，受影响认证域不得启动为可用状态。

### 12.2 运行时故障

共享模式下 Redis 权威状态不可用时：

- Callback 拒绝签发
- Refresh 拒绝换发
- Private 和 Manage 拒绝请求
- 新 WebSocket 订阅拒绝
- 已有认证 WebSocket 会话关闭
- Public API 继续工作

单节点模式只依赖本地 Badger，不因缺少 Redis 失败。Badger 不可用时，所有需要认证世代的路径 fail closed。

### 12.3 关闭

`ServiceContext` 关闭时按以下顺序处理：

1. 停止接受新的认证请求
2. 关闭认证 WebSocket 会话
3. 停止 EventBridge 认证订阅
4. 等待已接收控制事件完成持久化
5. 关闭 Redis 适配器和 Badger
6. 清理 Client 和 Hook 引用

关闭完成后，旧组件不得被同名新 `ServiceContext` 复用。

## 13. 配置契约

Casdoor 配置增加 Webhook 和撤销相关字段。字段名称在实施计划中以现有配置风格为准，但必须表达以下语义：

```yaml
auth:
  casDoor:
    enable: true
    yamlFilePath: etc/auth.yaml
    webhookSecret: auth_webhook_secret_example

manageAuth:
  casDoor:
    enable: true
    yamlFilePath: etc/manage-auth.yaml
    webhookSecret: manage_webhook_secret_example

authRevocation:
  mode: local
  badgerPath: data/auth-revocation
  redis:
    host: 127.0.0.1:6379
```

运行时配置文件和 Casdoor YAML 文件必须使用 `0600` 权限。`CasdoorConfig` API 只能返回 Endpoint、Client ID、Organization、Application、公开前端地址和 Callback 相对路径，不能返回 Client Secret、Webhook Secret 或证书私钥材料。

## 14. 兼容和发布策略

本次变更涉及公开 Go 类型、配置、JWT Claims、认证中间件、路由和运行时行为。实施必须遵守发布契约：

- 保留 `Casdoor` 和 `Callback` 类型的废弃别名
- 保留 `/api/casdoor` URL，并让其返回新的 `/api/casdoor/callback`
- 删除旧 `/api/callback` URL，在路由兼容登记中记录有意迁移
- 新增 `/api/casdoor/webhook`
- 为新增配置提供明确校验和迁移错误
- 在废弃登记中记录全局 Casdoor SDK 入口
- 更新 API 快照和配置契约基线
- 在发布说明中明确要求轮换四个 Token Secret
- 明确所有用户和管理员需要重新登录
- 不自动生成可预测的 Webhook Secret

旧 Token 和旧 Callback URL 不兼容属于本次有意安全变更。发布前必须完成迁移说明和批准记录。消费方必须从 `/api/casdoor` 动态读取 Callback 路径，不能继续硬编码 `/api/callback`。

## 15. 测试和验收

### 15.1 单元测试

必须覆盖：

- Auth 和 Manage Casdoor Client 使用各自配置且不串扰
- Casdoor 用户不存在、禁用、禁止和删除时拒绝签发
- Access 和 Refresh Token 包含相同身份域和世代
- auth Token 不能访问 manage，manage Token 不能访问 private
- 缺少 Provider、ProviderSubject 或 Generation 的新 Token 被拒绝
- `OnAuthRequest` 收到只读副本，无法修改框架状态
- `OnAuth` 和 `OnAuthRequest` 返回类型化公开错误时保留安全状态、错误码和消息
- Hook 返回普通错误、panic 和超时时 fail closed，响应不包含内部错误
- `/api/casdoor` 返回 `/api/casdoor/callback`，旧 `/api/callback` 不再注册
- Webhook Secret 使用常量时间比较
- Webhook `type`、组织和应用不匹配时拒绝
- 重复事件不重复递增世代
- 迟到事件不回退状态
- 登出后旧 Token 失效，新登录 Token 有效

### 15.2 持久化和并发测试

必须覆盖：

- Badger 重启后恢复认证世代和阻断状态
- Redis 并发事件只产生一次有效世代推进
- Redis 故障时认证路径 fail closed
- EventBridge 重复和乱序投递保持世代单调
- Badger 快照不覆盖较新的 Redis 状态
- auth 和 manage 使用相同 ProviderSubject 时仍完全隔离

### 15.3 REST 和 WebSocket 集成测试

真实进程集成测试必须覆盖：

1. Callback 签发内部 Access/Refresh Token
2. Private Token 访问 Private API
3. Manage Token 访问 Manage API
4. 请求前 Hook 阻断冻结用户
5. Webhook 登出后旧 REST Token 返回 `401`
6. Webhook 登出后 Refresh 被拒绝
7. Webhook 登出后旧 WebSocket 会话关闭
8. 重新登录后新 Token 和 WebSocket 正常
9. Redis 停止后认证接口 fail closed，Public API 仍可用

集成测试不得访问生产 Casdoor。测试使用独立假 Casdoor HTTP 服务、临时 Badger 目录和 Docker Redis。

### 15.4 质量门禁

实施完成后至少运行：

```bash
go test -race ./pkg/server/api/public ./pkg/server/router ./pkg/server/safe ./pkg/server/trans/rest ./pkg/server/trans/websocket -count=1
go test ./pkg/server/config/... -count=1
go vet ./pkg/server/...
./scripts/check-logging.sh
./scripts/ci.sh required/contracts
```

外部依赖集成测试使用显式环境变量开启，默认单元测试不得依赖 Docker。

## 16. 实施边界

本设计预计修改 `15` 至 `22` 个文件，新增约 `600` 至 `1000` 行实现和测试。实施应拆成可独立验收的小节：

1. 双 Casdoor Client 与配置隔离
2. Token Provider 和 Generation Claims
3. Badger 与 Redis 撤销管理器
4. Callback 和 Refresh 在线验证
5. REST 请求前 Hook
6. Casdoor Webhook 与 EventBridge
7. WebSocket 撤销
8. 密钥迁移、兼容登记和集成测试

每节先增加可失败的契约测试，再做最小实现。每节通过定向测试和外部只读审查后，才能进入下一节。

## 17. 完成定义

满足以下条件后，本设计才算实现完成：

- [ ] Auth 和 Manage 使用独立 Casdoor Client
- [ ] Callback 和 Refresh 在线验证身份状态
- [ ] 请求前 Hook 覆盖 REST 与 WebSocket
- [ ] 单节点撤销状态重启后保留
- [ ] 多节点认证世代由 Redis 权威管理
- [ ] Webhook 可靠、幂等且不泄露敏感信息
- [ ] 登出和身份变化立即撤销旧 Token
- [ ] Redis 或 Badger 故障时认证路径 fail closed
- [ ] 旧 Token 密钥迁移说明完整
- [ ] race、vet、日志和发布契约门禁通过
- [ ] 外部只读审查无 P0/P1
