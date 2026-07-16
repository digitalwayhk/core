# 示例 05：Casdoor 双域认证与商城权限设计

## 1. 目标

在示例 03 的模型继承、Manage 继承、商品、供应商、支付和订单能力基础上，新建一个可独立运行的 Casdoor 权限示例。示例必须展示：

- Auth 与 Manage 使用两个完全隔离的 Casdoor 认证域。
- 普通用户可匿名查询公开数据，登录后可下单并处理本人订单。
- 管理员可管理基础数据，查询业务数据并执行受控后台命令。
- `IAuthHookProvider`、`IAuthRequestHookProvider` 和 `ICasdoorEventHookProvider` 三个 Hook 的标准业务用法。
- 使用本地 Fake Casdoor 的真实 HTTP 进程集成测试，默认不依赖 Docker 或外部 Casdoor。

本示例不修改示例 03，不将示例 03 抽成共享业务包，也不增加新的框架认证机制。

## 2. 目录与分层

新示例目录为 `examples/05-shop-casdoor-rbac`，保持与示例 03 一致的分层：

| 目录 | 职责 |
| --- | --- |
| `contract` | 服务名等无依赖稳定常量 |
| `models` | 模型继承、持久化方法与身份事件审计模型 |
| `business` | 订单、支付、基础数据校验和身份事件幂等写入 |
| `api/dto` | Public/Private 对外响应结构 |
| `api/public` | 匿名可访问的商品、供应商和支付类型查询 |
| `api/private` | 普通用户下单、本人订单、支付、删除与撤销 |
| `api/manage` | 管理员基础数据 CRUD、业务数据查询、受控命令和身份事件审计 |
| `main` | 服务启动组合根 |

示例 05 是可单独阅读、运行和修改的教学样例。允许与示例 03 有意重复，避免读者跨示例追踪业务实现。

## 3. 认证域与权限矩阵

### 3.1 双 Casdoor 域

- `Auth.CasDoor` 面向普通用户，使用独立 Client、Access Secret、Refresh Secret 和 Webhook Secret，签发 `AuthTypeUser` Token。
- `ManageAuth.CasDoor` 面向管理员，使用另一组独立配置，签发 `AuthTypeManage` Token。
- 两域不得共享 Client ID、Client Secret、Access Secret、Refresh Secret 或 Webhook Secret。
- Auth Token 不得访问 Manage API；Manage Token 不得访问 Private API。
- Public API 保持匿名可访问，不因启用 Casdoor 改变。

### 3.2 权限矩阵

| 能力 | 匿名 | 普通用户 Auth Token | 管理员 Manage Token |
| --- | --- | --- | --- |
| 查询商品、供应商、支付类型 | 允许 | 允许 | 允许 |
| 下单、本人订单、支付、删除或撤销 | 拒绝 | 允许 | 拒绝 |
| 基础数据 CRUD 与启禁用 | 拒绝 | 拒绝 | 允许 |
| 订单、支付流水和身份事件后台查询 | 拒绝 | 拒绝 | 允许 |
| 确认支付等受控后台命令 | 拒绝 | 拒绝 | 允许 |

管理员若需以最终用户身份下单，必须另行通过 Auth 域登录；Manage Token 不具备隐式超级权限。

## 4. ShopService 的三个 Hook

### 4.1 `IAuthHookProvider.OnAuth`

Hook 在 Casdoor 用户已在线验证、框架签发 Token 之前执行：

- UID 为空时拒绝签发。
- `AuthTypeUser` 注入 `role=user` 和 `shop_scope=order`。
- `AuthTypeManage` 注入 `role=administrator` 和 `shop_scope=manage`。
- 其他认证类型 fail closed。
- 角色只由框架已确认的 `AuthType` 派生，不信任请求字段或 Casdoor 自定义角色字符串。

### 4.2 `IAuthRequestHookProvider.OnAuthRequest`

Hook 在 Access Token 验签、用途隔离和撤销世代校验完成后、Router 执行前运行：

- Private 路由必须同时满足 `AuthTypeUser`、`role=user` 和 `shop_scope=order`。
- Manage 路由必须同时满足 `AuthTypeManage`、`role=administrator` 和 `shop_scope=manage`。
- UID 为空、Claim 类型错误或权限不匹配时返回类型化公开错误“权限不足”。
- Hook 只读取框架传入的已验证 `AuthRequestArgs`，不从 Header、Body 或 Query 重新提取身份。

### 4.3 `ICasdoorEventHookProvider.OnCasdoorEvent`

Hook 在 Casdoor Webhook 已验证、标准化并将撤销事实持久化后异步执行：

- 将事件幂等写入 `IdentityEventRecord`，幂等键为框架生成的事件 ID。
- 仅保存事件 ID、认证域、UID、标准事件类型、世代、blocked 和发生时间。
- 不保存 Token、Secret、Header、原始 Webhook Payload 或未标准化 Claims。
- 新增只读 `IdentityEventManage`，供管理员查询身份生命周期审计。
- 审计写入不重复实现撤销权威，也不决定当前请求是否放行。

## 5. 数据模型

商城模型、继承关系和业务规则与示例 03 保持一致，新增身份事件审计模型：

```go
type IdentityEventRecord struct {
    *BusinessModel
    EventID    string
    AuthType   string
    UserID     string
    EventType  string
    Generation uint64
    Blocked    bool
    OccurredAt time.Time
}
```

约束：

- `GetHash` 使用 `EventID`，重试同一事件不得生成重复审计记录。
- 该模型不提供 Public/Private 返回 DTO，仅通过 Manage 只读列表展示。
- 该模型不允许通用 Manage Add/Update/Delete，只能由 Casdoor 事件 Hook 写入。

## 6. Casdoor 登录与事件数据流

### 6.1 登录

1. 客户端请求 `/api/casdoor?type=auth|manage`。
2. 框架根据认证域返回 Casdoor 登录 URL 和 `/api/casdoor/callback`。
3. Casdoor 返回 code 后，客户端请求 callback。
4. 框架使用对应域 Client 换取身份，在线验证用户状态，再调用 `OnAuth`。
5. 框架签发包含认证域、撤销世代和业务角色的 Access/Refresh Token。
6. 受保护请求经过验签、撤销校验和 `OnAuthRequest` 后才执行 Router。

### 6.2 Webhook

1. `/api/casdoor/webhook?type=auth|manage` 先使用对应域 Webhook Secret 验证。
2. 框架解析并标准化允许字段，持久化撤销世代和 blocked 状态。
3. 控制事件经 ServiceContext EventBridge 投递给持久化 worker。
4. worker 调用 `OnCasdoorEvent`，将标准化事件写入审计表。
5. 重复事件依靠 EventID 幂等收敛；写入失败由现有 worker 重试。

## 7. 错误与安全契约

- 未登录、Token 用途错误、撤销权威不可用统一 fail closed。
- Hook 只有类型化 `PublicError` 的安全消息可向前端返回；普通错误、panic 和超时统一脱敏。
- 角色和 scope 只信任已验签 Access Token 中的 Claims。客户端在 Body、Query 或 Header 伪造同名字段不得改变授权结果。
- Auth 和 Manage 的 Client、Secret、Token 和 Webhook 不得串域。
- 日志不记录 Token、Claims、Casdoor 原始用户对象或 Webhook Payload。
- `OnCasdoorEvent` 不将审计数据当作撤销事实源，避免双重权威。

## 8. 集成测试

集成测试目录为 `examples/integration/05-shop-casdoor-rbac`，复用 `examples/integration` 公共进程、HTTP 和 WebSocket 能力，并在该示例目录中实现业务专属的 Fake Casdoor。

Fake Casdoor 必须：

- 在本地随机端口启动 HTTP 服务。
- 区分 Auth 与 Manage 的 Client ID、Client Secret、用户和回调 code。
- 实现框架 DomainClient 需要的 token、JWT 解析和在线用户查询协议。
- 不跳过 `/api/casdoor`、`/api/casdoor/callback` 或框架 Access Token 签发链路。

测试文件与范围：

| 文件 | 验收内容 |
| --- | --- |
| `public_test.go` | 匿名查询商品、供应商和启用支付类型 |
| `auth_login_test.go` | 普通用户 Casdoor 登录、角色 Claim、Refresh |
| `manage_login_test.go` | 管理员 Casdoor 登录、角色 Claim、双域隔离 |
| `private_test.go` | 下单、本人订单、跨用户拒绝、支付、删除/撤销和 WebSocket |
| `manage_test.go` | 基础数据 CRUD/启禁用、业务只读和受控支付命令 |
| `authorization_test.go` | Auth Token 访问 Manage、Manage Token 访问 Private、伪造角色均被拒绝 |
| `webhook_test.go` | Webhook 后旧 Token 失效，幂等审计记录最终可查 |

保留 `TestPublicAPIs`、`TestPrivateAPIs` 和 `TestManageAPIs` 三个整组入口，每个 API 或 Manage command 仍有独立子测试。异步审计验证使用有界条件轮询，不使用无断言 `time.Sleep` 刷绿。

## 9. 兼容性与非目标

- 不修改框架公共 Go API、HTTP 路径规则、JSON 契约或配置结构。
- 不修改示例 03 的源码和既有集成测试。
- 不使用 Casdoor 全局 SDK，不自行挂载已废弃的 Casdoor `NewAuthHandler`。
- 不新建第三种管理员 Token，不用单 Casdoor 域的自定义 role 替代框架 AuthType 隔离。
- 不在本示例实现真实 Docker Casdoor 编排、多租户 RBAC 系统或权限管理 UI。
- 不在业务 Hook 里重复检查 Casdoor 用户在线状态或重复实现撤销世代。

## 10. 完成定义

- 示例 05 可使用首次运行自动生成的配置启动，README 清楚说明如何配置两个 Casdoor 域。
- 示例 03 的商城功能在示例 05 中保持，并增加双域登录、三 Hook 和身份审计。
- 上述权限矩阵由真实 HTTP 集成测试证明，不只是直接调用 Hook 或 Handler。
- Auth/Manage 串域、角色伪造、跨用户订单和 Webhook 撤销均有失败路径测试。
- 身份审计写入幂等，不保存任何认证凭据或原始 Webhook。
- 通过示例单元测试、真实进程集成测试、race、vet、日志检查和相关发布契约。
