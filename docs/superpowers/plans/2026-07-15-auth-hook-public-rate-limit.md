# Auth Hook 与 Public API 限流实施计划

> **执行要求：** 使用 `superpowers:executing-plans` 按任务实施；本项目不调用内部子 Agent，实现完成后由用户指定的外部 Agent 只读审查。步骤使用复选框（`- [x]`）跟踪。

**Goal:** 实现可拒绝颁发、可注入 Claims 的服务级 Auth Hook，将 Casdoor 登录换发为内置 Access/Refresh JWT，并为可外部访问的系统 Public API 增加无 Redis 依赖的本地限流。

**Architecture:** `ServiceContext` 是 AuthHook Provider 和 Public API 限流器的唯一所有者。`safe` 包统一构造带 `token_use/auth_type` 的 Token，Public API 只负责解析输入和选择 auth/manage 配置。`RouterInfo` 在注册期冻结限流元数据，REST 层在认证前按“服务+路由+可信 IP”执行 `x/time/rate` 令牌桶。

**Tech Stack:** Go 1.24、go-zero REST/JWT、`github.com/golang-jwt/jwt/v4`、`golang.org/x/time/rate`、Testify、真实进程 HTTP 集成测试。

---

## 执行状态

| 任务 | 状态 | 提交 |
|------|------|------|
| 1. Auth Hook 公共契约与 ServiceContext 绑定 | 已完成 | `c3d427f` |
| 2. TokenIssuer 与 Refresh Token 严格验证 | 已完成 | `d2a8c49` |
| 3. RefreshSecret 默认值和历史配置迁移 | 已完成 | `5291110` |
| 4. Callback、Refresh 与 TestToken 调用 Hook | 已完成 | `07326ae` |
| 5. Casdoor 只作身份交换 | 已完成 | `a8353c4` |
| 6. RouterInfo 限流元数据与本地 Manager | 已完成 | `ef09823` |
| 7. REST 限流包装与系统 Public 路由配置 | 已完成 | `f8fd617` |
| 8. 真实进程集成与兼容契约 | 已完成 | `5252a34` |
| 9. 总验收与外部审查交接 | 已完成 | `8fbbf22` |

正文保留 TDD 实施步骤作为审计记录；现行进度与提交证据以上表为准。

---

## 文件边界

- `pkg/server/types/auth.go`：AuthType/AuthSource、Hook 参数、Provider 接口。
- `pkg/server/safe/tokenissuer.go`：Token 默认 Claims、签名、刷新验证和响应 DTO。
- `pkg/server/config/serverconfig.go`：RefreshSecret/RefreshExpire 默认值与历史配置迁移。
- `pkg/server/router/servicecontext.go`：Hook 和限流器所有权。
- `pkg/server/api/public/auth_helpers.go`：根据 auth/manage 选择配置并调用 Hook。
- `pkg/server/api/public/callback.go`、`refresh.go`、`testtoken.go`：三个颁发入口。
- `pkg/server/ratelimit/manager.go`：ServiceContext 级令牌桶与空闲清理。
- `pkg/server/types/routerinfo.go`、`pkg/server/router/routerinfooption.go`：冻结限流策略。
- `pkg/server/trans/rest/server.go`：限流、认证、安全响应头的包装顺序。
- `pkg/server/api/public/ipwhitelist.go`：恢复 ServerArgs 访问控制。
- `examples/integration/helpers.go`：兼容 TestToken 的结构化响应。

---

### Task 1: Auth Hook 公共契约与 ServiceContext 绑定

**Files:**
- Create: `pkg/server/types/auth.go`
- Modify: `pkg/server/router/servicecontext.go`
- Test: `pkg/server/router/servicecontext_auth_test.go`

- [x] **Step 1: 写 ServiceContext 自动检测 Provider 的失败测试**

```go
type authHookService struct{ captured *types.AuthHookArgs }

func (s *authHookService) OnAuth(_ context.Context, args *types.AuthHookArgs) error {
    s.captured = args
    return nil
}

func TestServiceContextCapturesAuthHookProvider(t *testing.T) {
    service := &authHookService{}
    sc := NewServiceContextWithConfig(service, testServerConfig(t, "auth-hook"))
    require.Same(t, service, sc.AuthHookProvider)
}
```

- [x] **Step 2: 运行测试确认 RED**

Run: `rtk go test ./pkg/server/router -run TestServiceContextCapturesAuthHookProvider -count=1`

Expected: FAIL，`types.AuthHookArgs` 或 `ServiceContext.AuthHookProvider` 未定义。

- [x] **Step 3: 实现类型与绑定**

```go
type IClaimsMutator interface { AddData(key, value string) }
type AuthType string
type AuthSource string

type AuthHookArgs struct {
    UID, Username string
    AuthType AuthType
    Source AuthSource
    IssuedAt time.Time
    AccessExpireSeconds, RefreshExpireSeconds int64
    AccessExpiresAt, RefreshExpiresAt time.Time
    Extra interface{}
    Claims IClaimsMutator
}

type IAuthHookProvider interface {
    OnAuth(context.Context, *AuthHookArgs) error
}
```

`initServiceContextPost` 在路由装配前通过 `service.(types.IAuthHookProvider)` 设置 `sc.AuthHookProvider`。
限流策略值定义在 `types`，`ratelimit.Manager` 只消费该只读值，禁止 `types` 反向依赖具体限流实现包。

- [x] **Step 4: 验证 GREEN 与 race**

Run: `rtk go test -race ./pkg/server/router -run 'TestServiceContextCapturesAuthHookProvider|TestServiceContext' -count=1`

Expected: PASS。

- [x] **Step 5: 提交**

```bash
rtk git add pkg/server/types/auth.go pkg/server/router/servicecontext.go pkg/server/router/servicecontext_auth_test.go
rtk git commit -m "feat: add service auth hook contract"
```

### Task 2: TokenIssuer 与 Refresh Token 严格验证

**Files:**
- Create: `pkg/server/safe/tokenissuer.go`
- Test: `pkg/server/safe/tokenissuer_test.go`
- Modify: `pkg/server/safe/jwt.go`

- [x] **Step 1: 写默认 Claims、Hook 字段隔离和用途校验的失败测试**

```go
func TestIssueTokenPairSeparatesAccessAndRefreshClaims(t *testing.T) {
    now := time.Unix(1_700_000_000, 0).UTC()
    claims := NewClaims("user-1", "User")
    claims.AddData("shop_level", "gold")
    pair, err := IssueTokenPair(TokenIssueRequest{ /* fixed now/secrets/expiries */ })
    require.NoError(t, err)
    access := parseMapClaims(t, pair.AccessToken, "access-secret")
    refresh := parseMapClaims(t, pair.RefreshToken, "refresh-secret")
    require.Equal(t, "gold", access["shop_level"])
    require.NotContains(t, refresh, "shop_level")
    require.Equal(t, "access", access["token_use"])
    require.Equal(t, "refresh", refresh["token_use"])
}

func TestValidateRefreshTokenRejectsAccessToken(t *testing.T) { /* access token + refresh secret/type must fail */ }
func TestValidateRefreshTokenRejectsWrongAuthType(t *testing.T) { /* auth token under manage config must fail */ }
```

- [x] **Step 2: 运行测试确认 RED**

Run: `rtk go test ./pkg/server/safe -run 'TestIssueTokenPair|TestValidateRefreshToken' -count=1`

Expected: FAIL，颁发/验证 API 未定义。

- [x] **Step 3: 实现最小 TokenIssuer**

```go
type TokenPairResponse struct {
    AccessToken string `json:"access_token"`
    RefreshToken string `json:"refresh_token,omitempty"`
    TokenType string `json:"token_type"`
    AccessExpiresIn int64 `json:"access_expires_in"`
    RefreshExpiresIn int64 `json:"refresh_expires_in,omitempty"`
}
```

`IssueTokenPair` 使用单一 `IssuedAt`；Access 复制 Hook 后 Claims，Refresh 只写 UID/Uname/AuthType/TokenUse/iat/exp。`ValidateRefreshToken` 限制 HS256、密钥、`token_use=refresh`、预期 AuthType、UID 非空和 exp。

- [x] **Step 4: 验证 GREEN**

Run: `rtk go test -race ./pkg/server/safe -run 'TestIssueTokenPair|TestValidateRefreshToken' -count=20`

Expected: PASS。

- [x] **Step 5: 提交**

```bash
rtk git add pkg/server/safe/tokenissuer.go pkg/server/safe/tokenissuer_test.go pkg/server/safe/jwt.go
rtk git commit -m "feat: issue scoped access and refresh tokens"
```

### Task 3: RefreshSecret 默认值和历史配置迁移

**Files:**
- Modify: `pkg/server/config/serverconfig.go`
- Test: `pkg/server/config/serverconfig_auth_test.go`

- [x] **Step 1: 写默认值、迁移幂等和 0600 权限测试**

```go
func TestNewConfigCreatesDistinctRefreshSecrets(t *testing.T) {
    cfg := NewServiceDefaultConfig("auth-defaults", 18081)
    require.Equal(t, int64(7200), cfg.Auth.AccessExpire)
    require.Equal(t, int64(2592000), cfg.Auth.RefreshExpire)
    require.NotEmpty(t, cfg.Auth.RefreshSecret)
    require.NotEqual(t, cfg.Auth.AccessSecret, cfg.Auth.RefreshSecret)
}

func TestMigrateConfigPersistsRefreshSecretsOnce(t *testing.T) {
    // 写入仅有 AccessSecret 的历史 JSON，连续 migrateConfig 两次，断言密钥不变且 mode=0600。
}
```

- [x] **Step 2: 运行测试确认 RED**

Run: `rtk go test ./pkg/server/config -run 'TestNewConfigCreatesDistinctRefreshSecrets|TestMigrateConfigPersistsRefreshSecretsOnce' -count=1`

Expected: FAIL，Refresh 字段/迁移缺失。

- [x] **Step 3: 实现迁移**

`AuthSecret` 增加字段；`NewServiceDefaultConfig` 为 Auth/ManageAuth 生成独立 UUID。`migrateConfig` 在 `Auth`/`ManageAuth` 子表缺失 RefreshSecret 时生成并回写，已存在时不更改。

- [x] **Step 4: 验证 GREEN 和旧配置契约**

Run: `rtk go test -race ./pkg/server/config -count=1`

Expected: PASS。

- [x] **Step 5: 提交**

```bash
rtk git add pkg/server/config/serverconfig.go pkg/server/config/serverconfig_auth_test.go
rtk git commit -m "feat: add persistent refresh token secrets"
```

### Task 4: Callback、Refresh 与 TestToken 调用 Hook

**Files:**
- Create: `pkg/server/api/public/auth_helpers.go`
- Create: `pkg/server/api/public/refresh.go`
- Create: `pkg/server/api/public/auth_test.go`
- Modify: `pkg/server/api/public/callback.go`
- Modify: `pkg/server/api/public/testtoken.go`
- Modify: `pkg/server/api/release/routes.go`

- [x] **Step 1: 写 Hook 参数、拒绝、Claims 注入和 auth/manage 刷新测试**

```go
func TestIssueForServiceCallsHookBeforeSigning(t *testing.T) {
    // Provider 断言 UID/AuthType/Source/IssuedAt/Expire 完整，注入 shop_level。
    // 解析返回 Access Token 断言 shop_level，Refresh Token 断言不包含。
}

func TestIssueForServiceRejectsEmptyUIDBeforeHook(t *testing.T) { /* hook calls == 0 */ }
func TestIssueForServiceReturnsNoTokenWhenHookRejects(t *testing.T) { /* response zero value */ }
func TestRefreshAcceptsAuthAndManageSecretsIndependently(t *testing.T) { /* table auth/manage */ }
```

- [x] **Step 2: 运行测试确认 RED**

Run: `rtk go test ./pkg/server/api/public -run 'TestIssueForService|TestRefresh' -count=1`

Expected: FAIL，辅助函数和 Refresh 路由未定义。

- [x] **Step 3: 实现颁发入口**

`issueForService(ctx, sc, uid, username, authType, source, extra)` 使用对应 AuthSecret 构造 Args，调用 Provider，再调用 `safe.IssueTokenPair`。Callback 先 `ParseJwtToken` 得到 UID/Email。Refresh 为 POST，只接受 body token，验证后重跑 Hook 并只返回新 Access Token。

- [x] **Step 4: 保持 TestToken ACL 并验证 GREEN**

Run: `rtk go test -race ./pkg/server/api/public -count=1`

Expected: PASS；TestToken 仍通过嵌入 ServerArgs 执行访问控制。

- [x] **Step 5: 提交**

```bash
rtk git add pkg/server/api/public pkg/server/api/release/routes.go
rtk git commit -m "feat: run auth hooks during token issuance"
```

### Task 5: Casdoor 只作身份交换

**Files:**
- Modify: `pkg/server/trans/rest/server.go`
- Modify: `pkg/server/router/request.go`
- Test: `pkg/server/trans/rest/server_auth_exchange_test.go`
- Test: `pkg/server/router/request_security_test.go`

- [x] **Step 1: 写 Casdoor 原始身份不再注入 Request 的失败测试**

```go
func TestRequestIgnoresCasdoorUserContext(t *testing.T) {
    ctx := context.WithValue(context.Background(), "user", casdoorsdk.User{Id: "bypass"})
    req := NewRequest(authenticatedRouter(t), httptest.NewRequest(http.MethodGet, "/private", nil).WithContext(ctx))
    require.Nil(t, req)
}
```

- [x] **Step 2: 运行测试确认 RED**

Run: `rtk go test ./pkg/server/router ./pkg/server/trans/rest -run 'TestRequestIgnoresCasdoor|TestCasdoorModeUsesInternalJWT' -count=1`

Expected: FAIL，原始 `context["user"]` 仍可提供身份或路由仍包装 Casdoor middleware。

- [x] **Step 3: 移除旧身份分支并统一内置 JWT**

`getUserIDAndName` 只读 `uid/uname` context。`handers` 在 Logto 未启用时，即使 CasDoor.Enable 也直接复用 go-zero 的 `handler.Authorize(AccessSecret)`；Casdoor middleware 不再包装 Private/Manage 路由。

- [x] **Step 4: 验证 GREEN**

Run: `rtk go test -race ./pkg/server/router ./pkg/server/trans/rest -count=1`

Expected: PASS。

- [x] **Step 5: 提交**

```bash
rtk git add pkg/server/router/request.go pkg/server/router/request_security_test.go pkg/server/trans/rest/server.go pkg/server/trans/rest/server_auth_exchange_test.go
rtk git commit -m "fix: require internal jwt after casdoor exchange"
```

### Task 6: RouterInfo 限流元数据与本地 Manager

**Files:**
- Create: `pkg/server/ratelimit/manager.go`
- Create: `pkg/server/ratelimit/manager_test.go`
- Modify: `pkg/server/types/routerinfo.go`
- Modify: `pkg/server/router/routerinfooption.go`
- Test: `pkg/server/router/routerinfo_rate_limit_test.go`

- [x] **Step 1: 写路由/IP/服务隔离和冻结元数据失败测试**

```go
func TestManagerIsolatesRouteAndClient(t *testing.T) {
    manager := NewManager("shop", time.Minute)
    policy := Policy{Rate: 1, Burst: 1}
    require.True(t, manager.Allow("/a", "203.0.113.1", policy))
    require.False(t, manager.Allow("/a", "203.0.113.1", policy))
    require.True(t, manager.Allow("/b", "203.0.113.1", policy))
    require.True(t, manager.Allow("/a", "203.0.113.2", policy))
}

func TestWithExternalRateLimitFreezesPolicy(t *testing.T) { /* Getter == configured; post-freeze mutation panic */ }
```

- [x] **Step 2: 运行测试确认 RED**

Run: `rtk go test ./pkg/server/ratelimit ./pkg/server/router -run 'TestManager|TestWithExternalRateLimit' -count=1`

Expected: FAIL，package/Option 未定义。

- [x] **Step 3: 实现 Manager 与 Option**

```go
type Policy struct { Rate float64; Burst int }
type Manager struct { service string; clients map[string]*clientLimiter; mu sync.Mutex; closed bool }
```

`Policy` 定义在 `types`，Manager 内部再转为 `rate.Limit`。Key 为 `route + "\x00" + ip`，空 IP 归一为 `unknown`。使用懒清理而不启动常驻 goroutine；`Close` 清空 map 并使后续 Allow fail closed。RouterInfo 仅暴露 `GetExternalRateLimit()`。

- [x] **Step 4: 验证 GREEN 与 race**

Run: `rtk go test -race ./pkg/server/ratelimit ./pkg/server/router -run 'TestManager|TestWithExternalRateLimit' -count=20`

Expected: PASS。

- [x] **Step 5: 提交**

```bash
rtk git add pkg/server/ratelimit pkg/server/types/routerinfo.go pkg/server/router/routerinfooption.go pkg/server/router/routerinfo_rate_limit_test.go
rtk git commit -m "feat: add service scoped public api limiter"
```

### Task 7: REST 限流包装与系统 Public 路由配置

**Files:**
- Modify: `pkg/server/router/servicecontext.go`
- Modify: `pkg/server/trans/rest/server.go`
- Test: `pkg/server/trans/rest/server_rate_limit_test.go`
- Modify: `pkg/server/api/public/health.go`
- Modify: `pkg/server/api/public/casdoor.go`
- Modify: `pkg/server/api/public/callback.go`
- Modify: `pkg/server/api/public/refresh.go`
- Modify: `pkg/server/api/public/getmenu.go`
- Modify: `pkg/server/api/public/queryconfig.go`
- Modify: `pkg/server/api/public/queryrouters.go`
- Modify: `pkg/server/api/public/observe.go`
- Modify: `pkg/server/api/public/notify.go`
- Modify: `pkg/server/api/public/attach.go`
- Modify: `pkg/server/api/public/ipwhitelist.go`
- Modify: `pkg/server/api/public/queryservice.go`
- Modify: `pkg/server/api/public/openapi.go`

- [x] **Step 1: 写外部超限、本机跳过、unknown IP、TestToken 排除和安全头测试**

```go
func TestExternalRateLimitReturnsTyped429(t *testing.T) { /* burst+1 -> HTTP 429, code 42900 */ }
func TestExternalRateLimitBypassesDirectLoopback(t *testing.T) { /* many loopback calls pass */ }
func TestExternalRateLimitUsesUnknownBucketWhenClientIPFailsClosed(t *testing.T) { /* empty IP still limited */ }
func TestTestTokenHasNoRateLimitPolicy(t *testing.T) { require.Nil(t, (&public.TestToken{}).RouterInfo().GetExternalRateLimit()) }
func TestRateLimitedResponseIncludesSecurityHeaders(t *testing.T) { /* X-Content-Type-Options etc */ }
```

- [x] **Step 2: 运行测试确认 RED**

Run: `rtk go test ./pkg/server/trans/rest ./pkg/server/api/public -run 'TestExternalRateLimit|TestTestToken|TestRateLimited' -count=1`

Expected: FAIL，REST 尚未包装限流器或路由未声明策略。

- [x] **Step 3: 实现包装顺序和默认额度**

`ServiceContext` 创建/关闭 Manager。`handers` 使用 `ClientPublicIP` 和 `HasLocalIPAddr` 包装 handler，在认证前执行 Allow；最外层保持 `securityHeaders`。Callback/Refresh=5/10，Health=20/40，其余可外部系统 Public=10/20。

- [x] **Step 4: 恢复 IpWhiteList ACL 并验证 GREEN**

`IpWhiteList.Validation` 首先 `return own.ServerArgs.Validation(req)`；TestToken 不添加 Option。

Run: `rtk go test -race ./pkg/server/api/public ./pkg/server/trans/rest ./pkg/server/router ./pkg/server/ratelimit -count=1`

Expected: PASS。

- [x] **Step 5: 提交**

```bash
rtk git add pkg/server/router/servicecontext.go pkg/server/trans/rest/server.go pkg/server/api/public
rtk git commit -m "feat: rate limit external system apis"
```

### Task 8: 真实进程集成与兼容契约

**Files:**
- Modify: `examples/integration/helpers.go`
- Modify: `examples/integration/01-simple-shop/helpers_test.go`
- Create: `examples/integration/01-simple-shop/auth_hook_test.go`
- Modify: `internal/compat/compat.go`
- Modify: `docs/codex/DEPRECATION_REGISTER.md` or current compatibility register if API diff requires it

- [x] **Step 1: 写 TestToken 结构化响应和 Hook Claims 的失败集成测试**

```go
func TestTestTokenReturnsHookedAccessToken(t *testing.T) {
    token := suite.TokenFor(t, "hook-user", 0)
    claims := decodeJWTWithoutTrustingItForAuthorization(t, token)
    require.Equal(t, "hook-user", claims["uid"])
    require.Equal(t, "access", claims["token_use"])
}
```

测试 Service 实现 Hook 注入固定测试 Claim，并断言 Private API 可通过 `req.GetClaims` 读取。

- [x] **Step 2: 运行集成测试确认 RED**

Run: `rtk go test ./examples/integration/01-simple-shop -run TestTestTokenReturnsHookedAccessToken -count=1 -timeout=15m`

Expected: FAIL，helper 仍只解析字符串或示例尚未实现 Hook fixture。

- [x] **Step 3: 适配 helper 和 API 兼容基线**

`TokenFor` 先解析 `TokenPairResponse.access_token`；为过渡期测试允许旧 JSON string，但新集成测试必须覆盖新结构。更新公共 API 快照和发布登记。

- [x] **Step 4: 运行真实进程、race 和兼容门禁**

Run:

```bash
rtk go test -race ./examples/integration/01-simple-shop -count=1 -timeout=15m
rtk go test ./internal/compat ./pkg/server/types ./pkg/server/config -count=1
rtk ./scripts/test.sh release-contract
```

Expected: PASS。

- [x] **Step 5: 提交**

```bash
rtk git add examples/integration internal/compat docs/codex
rtk git commit -m "test: cover auth hooks and public api limits"
```

### Task 9: 总验收与外部审查交接

**Files:**
- Modify: `docs/copilot/AUTH_HOOK_DESIGN.md` only if implementation names differ after verified refactor
- Create: `docs/codex/AUTH_HOOK_IMPLEMENTATION_REVIEW_PROMPT.md`

- [x] **Step 1: 运行格式化与定向验收**

```bash
rtk gofmt -w <all changed go files>
rtk go test ./pkg/server/safe ./pkg/server/config ./pkg/server/router \
  ./pkg/server/api/public ./pkg/server/ratelimit ./pkg/server/trans/rest -count=1
rtk go test -race ./pkg/server/safe ./pkg/server/router ./pkg/server/api/public \
  ./pkg/server/ratelimit ./pkg/server/trans/rest -count=1
```

Expected: PASS，无警告。

- [x] **Step 2: 运行真实集成和静态门禁**

```bash
rtk go test -race ./examples/integration/01-simple-shop -count=1 -timeout=15m
rtk go vet ./pkg/server/... ./examples/integration/...
rtk ./scripts/check-logging.sh
rtk ./scripts/test.sh release-contract
```

Expected: PASS。

- [x] **Step 3: 生成外部只读审查提示词**

提示词必须要求反馈：P0/P1/P2 Findings、Hook 调用时序、Token 密钥/用途隔离、Casdoor 绕过是否关闭、TestToken ACL、IpWhiteList ACL、限流路由/IP/生命周期隔离、测试真实性、兼容性和最终 `APPROVED|CHANGES_REQUIRED`。

- [x] **Step 4: 提交审查提示词**

```bash
rtk git add docs/codex/AUTH_HOOK_IMPLEMENTATION_REVIEW_PROMPT.md
rtk git commit -m "docs: add auth hook implementation review prompt"
```
