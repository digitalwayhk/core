# Auth Hook 设计方案

## 背景

当前框架通过 Casdoor 完成 OAuth2 认证，`/api/callback` 拿到第三方 token 后直接返回给前端。
各业务服务无法在令牌颁发时插入自己的逻辑（黑名单、权限注入等），且前端持有的是第三方 token，
无法携带业务数据。

本方案引入 **Auth Hook 机制**，统一拦截令牌颁发节点，实现：

1. 业务服务可在令牌颁发时拒绝登录（黑名单、资格检查）
2. 业务服务可向内置 JWT 注入业务数据（会员等级、权限 ID 等）
3. 前端统一持有内置 JWT，不再直接使用第三方 token

---

## 令牌流程

### 登录（Casdoor）

```
前端                          后端
 │                             │
 │── GET /api/casdoor ────────▶│ 获取 Casdoor 配置（endpoint, clientID 等）
 │◀─ CasdoorResponse ─────────│
 │                             │
 │   [跳转 Casdoor 登录页]      │
 │                             │
 │── GET /api/callback         │
 │   ?code=xxx&state=xxx ─────▶│ 1. GetOAuthToken(code, state)
 │                             │ 2. ParseJwtToken → 得到 Casdoor Claims
 │                             │ 3. safe.NewClaims(uid, email)
 │                             │ 4. 调用服务 AuthHook
 │                             │    ├─ 黑名单检查（返回 error → 拒绝）
 │                             │    └─ AddData("shop_level", level)
 │                             │ 5. 颁发 access_token（短期，2h）
 │                             │ 6. 颁发 refresh_token（长期，30d）
 │◀─ {access_token,            │
 │    refresh_token} ──────────│
 │                             │
 │   [存储两个 token，后续请求  │
 │    使用 access_token]        │
```

### 刷新

```
前端                          后端
 │                             │
 │── POST /api/refresh ────────▶│ 1. 验证 refresh_token（RefreshSecret）
 │   {token: "refresh_token"}  │ 2. 取出 uid
 │                             │ 3. 再次调用 AuthHook（刷新权限、重查黑名单）
 │                             │ 4. 颁发新 access_token
 │◀─ {access_token} ───────────│
```

---

## Auth Hook 接口定义

```go
// pkg/server/types/interface.go

// IClaimsMutator 允许 OnAuth 向即将颁发的内置 JWT 注入自定义数据。
// safe.Claims 已实现 AddData，天然满足此接口，无需跨包引用。
type IClaimsMutator interface {
    AddData(key, value string)
}

type AuthType string

const (
    AuthTypeUser         AuthType = "auth"
    AuthTypeManage       AuthType = "manage"
    AuthTypeServerManage AuthType = "servermanage"
)

type AuthSource string

const (
    AuthSourceCallback  AuthSource = "callback"
    AuthSourceRefresh   AuthSource = "refresh"
    AuthSourceTestToken AuthSource = "testtoken"
)

// AuthHookArgs 是框架在签名前构造的不可缺省上下文。
// UID 必须非空；时间字段与最终 JWT 完全一致。
type AuthHookArgs struct {
    UID              string
    Username         string
    AuthType         AuthType
    Source           AuthSource
    IssuedAt         time.Time
    AccessExpireSeconds  int64
    RefreshExpireSeconds int64
    AccessExpiresAt  time.Time
    RefreshExpiresAt time.Time // servermanage 不颁发 refresh token 时为零值
    Extra            interface{}
    Claims           IClaimsMutator
}

// IAuthHookProvider 服务实现此接口后，框架在颁发内置 JWT 前自动调用
// OnAuth。无需显式注册，NewServiceContext 会检测并注入。
//
// Callback 的 Extra 为 *casdoorsdk.Claims；Refresh/TestToken 为 nil。
// 返回 error 则拒绝颁发，等同拒绝登录或刷新。
type IAuthHookProvider interface {
    OnAuth(ctx context.Context, args *AuthHookArgs) error
}
```

### 默认参数与 Claims

Callback 和 TestToken 在调用 Hook 前必须构造完整的 `AuthHookArgs`。`UID` 为空、`Claims` 为 nil 或 Access 超时秒数非正数时直接拒绝，不得调用 Hook，也不得签名 Token。auth/manage 的 Refresh 超时也必须为正数；servermanage 不颁发 Refresh Token，对应字段为零值。

Callback/TestToken 颁发新 Token 时，`AccessExpiresAt` 和 `RefreshExpiresAt` 必须由同一个 `IssuedAt` 加对应秒数得出，签名时不得重新读取时钟。Refresh 请求中，`RefreshExpiresAt` 取已验证 Refresh Token 的 `exp`，`RefreshExpireSeconds` 表示它在 Hook 调用时的剩余有效秒数。

Access Token 默认包含：

- `uid`、`uname`
- `auth_type=auth|manage|servermanage`
- `token_use=access`
- `iat`、`exp`
- Hook 通过 `Claims.AddData` 注入的业务字段

Refresh Token 默认只包含：

- `uid`、`uname`
- `auth_type=auth|manage`
- `token_use=refresh`
- `iat`、`exp`

Refresh Token 不携带 Hook 注入的业务权限；刷新时重新执行 Hook，用最新数据生成 Access Token。`auth` 和 `manage` 分别使用自己的 Access/Refresh 密钥。`servermanage` 仅保留现有测试 Access Token，不颁发 Refresh Token。

### Token 响应

Callback 和 auth/manage TestToken 返回统一结构，超时字段与 Hook 收到的参数相同：

```go
type TokenPairResponse struct {
    AccessToken      string `json:"access_token"`
    RefreshToken     string `json:"refresh_token,omitempty"`
    TokenType        string `json:"token_type"` // Bearer
    AccessExpiresIn  int64  `json:"access_expires_in"`
    RefreshExpiresIn int64  `json:"refresh_expires_in,omitempty"`
}
```

Refresh 返回同一结构，但 `refresh_token` 和 `refresh_expires_in` 为空，不做 Refresh Token Rotation。servermanage TestToken 也只返回 Access Token 及其超时时间。

---

## 配置变更

`AuthSecret` 新增刷新 token 相关字段（`AccessSecret` 与 `RefreshSecret` 使用不同密钥，
确保两者不可互换）：

```go
// pkg/server/config/serverconfig.go

type AuthSecret struct {
    AccessSecret  string // 现有：access token 签名密钥（短期）
    AccessExpire  int64  // 现有：access token 有效期（秒）
    RefreshSecret string // 新增：refresh token 签名密钥（长期）
    RefreshExpire int64  // 新增：refresh token 有效期（秒），建议 2592000（30天）
    CasDoor       CasDoorConfig
}
```

- 新建配置默认 `AccessExpire=7200`、`RefreshExpire=2592000`。
- 启用 Callback/Refresh 时，`AccessSecret` 与 `RefreshSecret` 必须非空且不相等。
- 历史配置通过一次性迁移生成 RefreshSecret 并按 `0600` 回写，避免每次启动随机生成导致旧 Refresh Token 立即失效。
- 程序化构造且不使用刷新能力的消费方可保持 RefreshSecret 为空；一旦调用 Callback/Refresh 则 fail closed。

---

## Public API 限流

### 边界

- 仅限制 `pkg/server/api/public` 中能够接受外部请求的系统 API，不自动影响业务服务自定义的 Public Router。
- 限流键为 `service + route + trusted client IP`，IP 使用 `TrustedProxies` 后的 `ClientPublicIP`。
- 可确认的本机直连请求跳过限流。IP 解析失败时使用共享的 `unknown` 键 fail closed，不得因空 IP 绕过。
- `TestToken` 保持现有 `ServerArgs.Validation` 语义：仅本机或显式开启 `RemoteAccessManageAPI` 时可访问，并明确排除限流。
- `IpWhiteList.Validation` 恢复调用 `ServerArgs.Validation`，不得再无条件允许外部修改白名单。

### 实现

- 复用 `golang.org/x/time/rate` 令牌桶，不自行实现算法，不强制 Redis。
- `RouterInfo` 通过注册期 Option 保存不可变限流元数据；运行期只通过 Getter 读取。
- 限流器由 `ServiceContext` 持有，不使用进程级全局单例，关闭服务时清理该服务的客户端状态。
- REST 包装顺序为：安全响应头 -> 外部 IP 限流 -> 认证 -> RouteHandler。这样超限请求不会消耗 JWT/Casdoor 验证资源，429 仍包含安全响应头。
- 超限使用现有 `rate_limited` 公开错误契约，返回 HTTP 429；日志只记录稳定事件、服务、路由和已脱敏 IP，不记录 Token、查询参数或请求体。

### 默认额度

| 路由类型 | 速率 | Burst |
|---|---:|---:|
| Callback、Refresh | 5/s | 10 |
| Casdoor、GetMenu、外部管理查询 | 10/s | 20 |
| Health | 20/s | 40 |

水平扩展时，本地令牌桶仅保护单实例；全局额度由入口网关或 WAF 负责。本次不引入 Redis 限流切换。

---

## 需要改动的文件

| 文件 | 改动内容 |
|------|----------|
| `pkg/server/types/interface.go` | 追加 `IClaimsMutator`、`AuthHookArgs`、`IAuthHookProvider` |
| `pkg/server/config/serverconfig.go` | `AuthSecret` 加 `RefreshSecret`、`RefreshExpire` |
| `pkg/server/router/servicecontext.go` | `ServiceContext` 加 AuthHook Provider 和本地 Public API 限流器；初始化时检测并注入 |
| `pkg/server/safe/jwt.go` | 增加带 `token_use/auth_type` 的双 Token 颁发与 Refresh Token 严格验证 |
| `pkg/server/api/public/callback.go` | `Do()` 在 `GetOAuthToken` 后解析 Casdoor Claims → 运行 AuthHook → 颁发双 token |
| `pkg/server/api/public/refresh.go` | **新建**：验证 refresh token → 运行 AuthHook → 颁发新 access token |
| `pkg/server/api/public/testtoken.go` | 在签名前运行 AuthHook；auth/manage 返回双 Token，servermanage 仅返回 Access Token |
| `pkg/server/types/routerinfo.go` | 增加冻结的限流元数据和 Getter |
| `pkg/server/router/routerinfooption.go` | 增加外部 Public API 限流 Option |
| `pkg/server/ratelimit` | 新增 ServiceContext 级本地令牌桶、空闲键清理和关闭契约 |
| `pkg/server/trans/rest/server.go` | 注册路由时包装本地限流器，并使 Casdoor 模式的业务路由改用内置 JWT |
| `pkg/server/api/public/ipwhitelist.go` | 恢复 ServerArgs 访问控制 |
| `examples/integration/helpers.go` | TestToken 公共工具适配双 Token 响应，对测试返回 Access Token |

`getUserIDAndName()` 中直接读 `context["user"]`（Casdoor User）的分支必须移除，REST 路由注册也必须从 Casdoor middleware 切换为内置 JWT 验证。只做其中一项会导致 Callback 返回的内置 Token 无法使用，或者原始 Casdoor Token 仍可绕过 Hook。

---

## 业务服务示例（ShopService）

```go
// examples/01-simple-shop/service.go

// OnAuth 实现 types.IAuthHookProvider
func (own *ShopService) OnAuth(ctx context.Context, args *types.AuthHookArgs) error {
    // 1. 黑名单检查
    if shopBlacklist.Contains(args.UID) {
        return errors.New("该账户已被禁止访问本商城")
    }
    // 2. 注入会员等级（写入 JWT，前端 decode 可直接读）
    level := shopDB.GetMemberLevel(args.UID)
    args.Claims.AddData("shop_level", level)
    return nil
}
```

下单路由里读取会员等级：

```go
func (own *AddOrder) Validation(req types.IRequest) error {
    level := req.GetClaims("shop_level") // 读取 JWT 中的业务字段
    // ...
}
```

---

## 安全说明

| 问题 | 结论 |
|------|------|
| 前端能否篡改 JWT 内容 | **不能**。JWT 由 HMAC-SHA256 签名，任何改动均导致签名失效 |
| access_token 与 refresh_token 能否互换使用 | **不能**。两者使用不同密钥签名，且严格验证 `token_use` |
| Casdoor 原始 token 能否直接访问 private 路由 | **不能**。Casdoor 只用于 Callback 身份交换；REST 业务路由只验证内置 JWT，并移除 `context["user"]` 身份分支 |
| 权限变更是否立即生效 | **不能**，延迟窗口 = access_token 有效期。高敏感操作建议实时查 DB |
| 未配置 Redis 时外部 Public API 是否不可用 | **否**。默认使用 ServiceContext 内本地令牌桶，无外部依赖 |

---

## 测试与验收

### Auth Hook 与 Token

1. Callback 和 TestToken 在签名前调用 Hook，Hook 收到非空 UID、准确的 AuthType/Source、秒数和到期时间。
2. Hook 注入的字段存在于 Access Token，不存在于 Refresh Token。
3. Hook 返回错误时不产生任何 Token，外部响应不泄露内部错误或原始 Claims。
4. Refresh Token 必须同时通过签名、`token_use=refresh`、`auth_type`、UID 和过期时间校验；Access Token、错误用途、错误密钥和过期 Token 全部拒绝。
5. `auth` Refresh Token 不能使用 ManageAuth.RefreshSecret，`manage` 也不能使用 Auth.RefreshSecret。
6. Casdoor 原始 Token 不能访问 Private/Manage 路由；Callback 换发的内置 Access Token 可以访问对应路由。
7. 历史配置迁移只执行一次，重启不改变 RefreshSecret，文件权限为 `0600`。
8. TestToken 继续受 `ServerArgs.Validation` 保护；公共集成测试 helper 使用响应中的 Access Token。

### Public API 限流

1. 同一服务/路由/IP 超出 Burst 后返回 HTTP 429 和 `rate_limited` 公开错误；不同路由、IP 和 ServiceContext 互不影响。
2. 本机直连跳过限流；只在 `TrustedProxies` 配置正确时使用 XFF/X-Real-IP；空 IP 使用 `unknown` 键而不是绕过。
3. TestToken 不进入限流器；IpWhiteList 在外部且未开启 `RemoteAccessManageAPI` 时先被访问控制拒绝。
4. 限流器的空闲键可清理，ServiceContext 关闭后无 goroutine 泄漏，`go test -race` 无竞争。
5. 429 响应仍包含安全响应头，日志不包含 Token、请求体、查询参数或完整 IP。

### 必跑命令

```bash
go test ./pkg/server/safe ./pkg/server/config ./pkg/server/router \
  ./pkg/server/api/public ./pkg/server/ratelimit ./pkg/server/trans/rest -count=1
go test -race ./pkg/server/safe ./pkg/server/router ./pkg/server/api/public \
  ./pkg/server/ratelimit ./pkg/server/trans/rest -count=1
go test -race ./examples/integration/01-simple-shop -count=1 -timeout=15m
go vet ./pkg/server/... ./examples/integration/...
./scripts/check-logging.sh
./scripts/test.sh release-contract
```

---

## 命名说明

| 旧名（被否决）| 新名 | 原因 |
|---|---|---|
| `ExchangeHook func(...)` | `IAuthHookProvider.OnAuth(...)` | 函数类型改为直接实现接口方法，去掉"返回函数"的间接层 |
| `AuthHooks() []AuthHook` | `OnAuth(...)` | 无需 slice，多个逻辑写在同一方法体内即可 |
| `IExchangeHookProvider` | `IAuthHookProvider` | Exchange 语义过窄，Auth 适用于登录和刷新两个节点 |
