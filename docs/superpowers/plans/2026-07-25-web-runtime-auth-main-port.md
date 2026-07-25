# Web Runtime Auth Main Port Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 `feat/web-runtime-auth` 已完成的权威选择、bootstrap、同源认证/业务代理和 OpenAPI 安全处理精准移植到当前 `main`，且不恢复任何已删除能力。

**Architecture:** 先补 Token 权威 claim，再建立 `WebServer` 启动前的 Manage Auth 权威选择；随后逐层接入 bootstrap、REST 外部执行适配、HTMLServer 同源 mux 和 OpenAPI Host 处理。旧分支文件只作为参考，所有改动以当前 `main` 类型、生命周期和内外 OpenAPI 分层为事实源。

**Tech Stack:** Go 1.26、go-zero REST、Casdoor、JWT、`net/http`、OpenAPI 3、`testify`、现有 Router/ServiceContext/AuthState。

---

### Task 1: 将 Casdoor Token 绑定到 Manage Auth 权威

**Files:**
- Modify: `pkg/server/types/auth.go`
- Modify: `pkg/server/safe/tokenissuer.go`
- Modify: `pkg/server/safe/tokenissuer_test.go`
- Modify: `pkg/server/authstate/types.go`
- Modify: `pkg/server/authstate/manager_test.go`
- Modify: `pkg/server/api/public/auth_helpers.go`
- Modify: `pkg/server/api/public/auth_test.go`
- Modify: `pkg/server/types/publicerror.go`

- [ ] **Step 1: 写 AuthorityService claim 的失败测试**

从旧分支 `a2d00da`、`116b0a0` 的测试意图迁移以下场景：

```go
func TestIssueAndValidateCasdoorTokenPreservesAuthorityService(t *testing.T) {
	request := TokenIssueRequest{
		Identity: types.AuthIdentity{
			UID:              "42",
			Username:         "admin",
			AuthType:         types.ManageAuth,
			Provider:         types.AuthProviderCasdoor,
			ProviderSubject:  "casdoor-user",
			Generation:       3,
			AuthorityService: " User-Service ",
		},
	}
	pair, err := IssueTokenPair(request, testAuthSecret(), time.Now())
	require.NoError(t, err)

	identity, err := ValidateAccessToken(pair.AccessToken, testAuthSecret().AccessSecret, time.Now())
	require.NoError(t, err)
	require.Equal(t, "user-service", identity.AuthorityService)
}

func TestValidateTokenRejectsAmbiguousAuthorityClaim(t *testing.T) {
	token := signTestToken(t, jwt.MapClaims{
		"auth_provider":          types.AuthProviderCasdoor,
		"auth_authority_service": "   ",
	})
	_, err := ValidateAccessToken(token, "secret", time.Now())
	require.ErrorContains(t, err, "auth_authority_service")
}

func TestIssueTokenRejectsAuthorityForNonCasdoorProvider(t *testing.T) {
	_, err := IssueTokenPair(TokenIssueRequest{
		Identity: types.AuthIdentity{
			UID: "42", AuthType: types.ManageAuth, AuthorityService: "user-service",
		},
	}, testAuthSecret(), time.Now())
	require.ErrorContains(t, err, "AuthorityService")
}
```

不得迁移旧分支 `AuthProviderLogto` 常量或 Logto 测试。

- [ ] **Step 2: 运行 Token 测试确认 RED**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/safe -run "AuthorityService|AmbiguousAuthority" -count=1
```

Expected: FAIL，`AuthIdentity` 尚无 `AuthorityService`，Token 未保存权威 claim。

- [ ] **Step 3: 添加当前契约所需的最小 claim**

在 `AuthIdentity` 增加：

```go
// AuthorityService 是 Casdoor Token 签名中的 Manage Auth 权威服务名。
// 为空时撤销命名空间回退到当前服务，以兼容旧 Token。
AuthorityService string
```

在 `tokenissuer.go` 定义私有 claim：

```go
const authAuthorityServiceClaim = "auth_authority_service"
```

只允许 Casdoor Identity 携带该字段；签发时规范化为小写，验证时拒绝：

- 非字符串
- 空白字符串
- 非 Casdoor Token 携带该 claim

旧 Token 没有该 claim 时继续兼容。

- [ ] **Step 4: 写撤销命名空间测试**

```go
func TestIdentityKeyUsesVerifiedAuthorityService(t *testing.T) {
	key := identityKey("order-service", types.AuthIdentity{
		AuthType:         types.ManageAuth,
		Provider:         types.AuthProviderCasdoor,
		ProviderSubject:  "subject",
		AuthorityService: "User-Service",
	})
	require.Equal(t, "user-service", key.Service)
}

func TestIdentityKeyFallsBackForLegacyToken(t *testing.T) {
	key := identityKey("order-service", types.AuthIdentity{
		AuthType: types.ManageAuth, Provider: types.AuthProviderCasdoor,
		ProviderSubject: "subject",
	})
	require.Equal(t, "order-service", key.Service)
}
```

- [ ] **Step 5: 实现撤销命名空间选择**

```go
func identityAuthorityService(managerService string, identity types.AuthIdentity) string {
	if authority := strings.TrimSpace(identity.AuthorityService); authority != "" {
		return strings.ToLower(authority)
	}
	return managerService
}
```

`identityKey` 只读取已经验证的 `AuthIdentity`，不从请求参数或 Header 读取权威服务名。

- [ ] **Step 6: 迁移 Refresh 公开错误测试和最小实现**

旧分支为 Refresh 区分：

- `40101`：无效、过期、类型错误
- `40102`：撤销、禁用、世代变化
- `50301`：撤销存储或 Casdoor 权威不可用

在 `publicerror.go` 增加对应稳定常量；`refreshForServiceWithDependenciesAt` 必须保留已验证
Refresh Token 中的 `AuthorityService`，不得根据请求重新计算。错误通过
`types.NewPublicError` 映射，内部依赖错误不直接暴露。

- [ ] **Step 7: 运行 Auth 相关包确认 GREEN**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/safe ./pkg/server/authstate ./pkg/server/api/public ./pkg/server/types -count=1
```

Expected: PASS。

- [ ] **Step 8: 提交 Token 权威绑定**

```bash
rtk git add pkg/server/types/auth.go pkg/server/safe/tokenissuer.go pkg/server/safe/tokenissuer_test.go pkg/server/authstate/types.go pkg/server/authstate/manager_test.go pkg/server/api/public/auth_helpers.go pkg/server/api/public/auth_test.go pkg/server/types/publicerror.go
rtk git commit -m "feat(auth): bind casdoor tokens to manage authority"
```

### Task 2: 精准移植 Manage Auth 权威选择

**Files:**
- Create: `pkg/server/run/manageauth.go`
- Create: `pkg/server/run/manageauth_test.go`
- Modify: `pkg/server/run/server.go`
- Modify: `pkg/server/run/server_concurrency_test.go`

- [ ] **Step 1: 迁移权威选择失败测试**

以旧分支 `manageauth_test.go` 为测试基准，但将 public 字段写入改为 setter：

```go
func TestResolveManageAuthAuthorityRequiresExplicitSelection(t *testing.T) {
	first := manageContextForTest(t, "first", true)
	second := manageContextForTest(t, "second", true)
	_, err := resolveManageAuthAuthority([]*router.ServiceContext{first, second}, "")
	require.ErrorContains(t, err, "多个 Manage 服务")
}

func TestResolveManageAuthAuthorityRejectsServiceWithoutManage(t *testing.T) {
	manage := manageContextForTest(t, "manage", true)
	plain := manageContextForTest(t, "plain", false)
	_, err := resolveManageAuthAuthority([]*router.ServiceContext{manage, plain}, "plain")
	require.ErrorContains(t, err, "不存在")
}

func TestSetManageAuthAuthorityRejectsMutationAfterInitialization(t *testing.T) {
	server := NewWebServer()
	require.NoError(t, server.SetManageAuthAuthority("user"))
	server.beginInitialization()
	require.Error(t, server.SetManageAuthAuthority("order"))
}
```

同时迁移 Access/Refresh、Expire、Casdoor Enable、shared revocation 和 Casdoor 配置不兼容测试；
断言错误文本包含字段名但不包含 Secret 值。

- [ ] **Step 2: 运行权威测试确认 RED**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/run -run "ManageAuthAuthority|ManageAuthCompatibility" -count=1
```

Expected: FAIL，setter、选择器和兼容检查尚不存在。

- [ ] **Step 3: 移植 `manageAuthAuthority` 私有模型和选择器**

从旧分支精准移植以下私有函数：

- `manageAuthContexts`
- `selectManageAuthContext`
- `normalizeServiceName`
- `validateManageAuthCompatibility`
- `compareManageAuthSecrets`
- `validateSharedCasdoorContract`
- `incompatibleManageAuthField`

`resolveManageAuthAuthority` 的规则保持：

```go
func resolveManageAuthAuthority(
	contexts []*router.ServiceContext,
	configured string,
) (*manageAuthAuthority, error)
```

不得引用 Logto、`AttachServices`、Observe/Notify 或旧运行地址字段。

- [ ] **Step 4: 添加启动前 setter**

`WebServer` 增加私有字段：

```go
manageAuthAuthorityService string
```

setter：

```go
func (own *WebServer) SetManageAuthAuthority(serviceName string) error {
	own.Lock()
	defer own.Unlock()
	if own.initializing.Load() || own.runStarted.Load() {
		return errors.New("Manage Auth 权威只能在启动前配置")
	}
	own.manageAuthAuthorityService = normalizeServiceName(serviceName)
	return nil
}
```

读取时使用锁内快照，不让 HTMLServer 直接读取可变字段。

- [ ] **Step 5: 在 `initializeServers` 监听前解析权威**

构造 HTMLServer 后、创建 listener 前：

```go
authority, err := resolveManageAuthAuthority(contexts, own.manageAuthAuthoritySnapshot())
if err != nil {
	return nil, fmt.Errorf("初始化 Manage Auth 权威失败: %w", err)
}
htmls.SetManageAuthAuthority(authority)
```

准备失败走现有 rollback，不能留下半初始化 HTMLServer。

- [ ] **Step 6: 运行 run 包测试确认 GREEN**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/run -run "ManageAuthAuthority|ManageAuthCompatibility|Initialization" -count=1
```

Expected: PASS。

- [ ] **Step 7: 提交权威选择**

```bash
rtk git add pkg/server/run/manageauth.go pkg/server/run/manageauth_test.go pkg/server/run/server.go pkg/server/run/server_concurrency_test.go
rtk git commit -m "feat(web): select manage auth authority before startup"
```

### Task 3: 精准移植 Web Bootstrap

**Files:**
- Create: `pkg/server/run/webbootstrap.go`
- Create: `pkg/server/run/webbootstrap_test.go`
- Modify: `pkg/server/run/htmlserver.go`

- [ ] **Step 1: 迁移 bootstrap 失败测试**

保留旧分支最终响应结构，覆盖：

```go
func TestWebBootstrapTestTokenMode(t *testing.T) {
	authority := testManageAuthority(t, false)
	response := buildWebBootstrap(authority, localRequest(t))
	require.Equal(t, "test_token", response.Auth.Mode)
	require.Equal(t, "manage", response.Auth.Type)
	require.Equal(t, "authority", response.Auth.AuthorityService)
	require.Equal(t, "/api/servermanage/testtoken", *response.Endpoints.AcquireToken)
	require.Nil(t, response.Endpoints.CasdoorConfig)
}

func TestWebBootstrapCasdoorMode(t *testing.T) {
	authority := testManageAuthority(t, true)
	response := buildWebBootstrap(authority, localRequest(t))
	require.Equal(t, "casdoor", response.Auth.Mode)
	require.Equal(t, "/api/casdoor", *response.Endpoints.CasdoorConfig)
}

func TestWebBootstrapContainsNoSecrets(t *testing.T) {
	recorder := httptest.NewRecorder()
	newWebBootstrapHandler(testManageAuthority(t, true)).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodGet, "/api/web/bootstrap", nil),
	)
	body := strings.ToLower(recorder.Body.String())
	for _, forbidden := range []string{"accesssecret", "refreshsecret", "clientsecret", "webhooksecret", "password", "token"} {
		require.NotContains(t, body, forbidden)
	}
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
}
```

另覆盖 unavailable、405、`Allow: GET` 和权威服务名规范化。

- [ ] **Step 2: 运行 bootstrap 测试确认 RED**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/run -run WebBootstrap -count=1
```

Expected: FAIL，bootstrap 类型和 handler 尚不存在。

- [ ] **Step 3: 精准移植 `webbootstrap.go`**

保留 schema version 1 和旧分支三种模式。端点固定为：

```go
const (
	webBootstrapPath          = "/api/web/bootstrap"
	webBootstrapAcquireToken  = "/api/servermanage/testtoken?userid=12345&type=1"
	webBootstrapCasdoorConfig = "/api/casdoor?type=manage"
	webBootstrapCallback      = "/callback"
	webBootstrapRefresh       = "/api/refresh"
	webBootstrapOpenAPI       = "/swagger/"
)
```

TestToken 模式调用现有 `public.TestToken.Validation` 只判断本地访问策略，不签发 Token。

- [ ] **Step 4: 在 HTML mux 注册匿名 bootstrap**

HTMLServer 使用已经冻结的 `manageAuthAuthority` 快照注册：

```go
mux.Handle(webBootstrapPath, newWebBootstrapHandler(authority))
```

不得从每个请求重新选择权威。

- [ ] **Step 5: 运行 bootstrap 与生命周期测试**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/run -run "WebBootstrap|HTMLServer" -count=1
```

Expected: PASS。

- [ ] **Step 6: 提交 bootstrap**

```bash
rtk git add pkg/server/run/webbootstrap.go pkg/server/run/webbootstrap_test.go pkg/server/run/htmlserver.go
rtk git commit -m "feat(web): expose runtime auth bootstrap"
```

### Task 4: 精准移植 REST 外部执行适配与认证代理

**Files:**
- Create: `pkg/server/trans/rest/externalrouter.go`
- Create: `pkg/server/trans/rest/externalrouter_test.go`
- Modify: `pkg/server/run/htmlserver.go`
- Create: `pkg/server/run/htmlserver_auth_test.go`

- [ ] **Step 1: 迁移 ExternalRouter 安全链失败测试**

测试必须证明：

- IP 白名单拒绝发生在 Parse/Do 前。
- Auth、ManageAuth 和 ServerManageAuth 使用原 RouterInfo。
- ResponseHandlerFunc 被保留。
- public error 状态与 JSON 语义保持。

测试 API：

```go
handler, err := rest.NewExternalRouterHandler(serviceRouter, routerInfo)
require.NoError(t, err)
handler.ServeHTTP(recorder, request)
```

- [ ] **Step 2: 运行 REST 测试确认 RED**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/trans/rest -run ExternalRouter -count=1
```

Expected: FAIL，公开给 HTMLServer 使用的 handler 构造器不存在。

- [ ] **Step 3: 移植 ExternalRouterHandler**

从旧分支 `externalrouter.go` 移植最小导出入口：

```go
func NewExternalRouterHandler(
	service *router.ServiceRouter,
	info *types.RouterInfo,
) (http.Handler, error)
```

实现必须调用当前 REST Server 已有的认证策略和 Router 执行函数；不得复制第二套 Token 校验，
不得相信请求 Header 自报服务身份。

- [ ] **Step 4: 迁移四个认证代理路径**

HTMLServer 通过权威 `ServiceRouter` 查找并挂载：

- `/api/servermanage/testtoken`
- `/api/casdoor`
- `/callback`
- `/api/refresh`

路径不存在时：

- bootstrap 对应模式返回 unavailable。
- HTMLServer 不注册虚假 handler。
- 必需路径与已选择模式矛盾时，准备阶段返回错误。

- [ ] **Step 5: 运行 REST 与 auth proxy 测试确认 GREEN**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/trans/rest ./pkg/server/run -run "ExternalRouter|HTMLServerAuth" -count=1
```

Expected: PASS。

- [ ] **Step 6: 提交认证同源代理**

```bash
rtk git add pkg/server/trans/rest/externalrouter.go pkg/server/trans/rest/externalrouter_test.go pkg/server/run/htmlserver.go pkg/server/run/htmlserver_auth_test.go
rtk git commit -m "feat(web): proxy auth routes through html server"
```

### Task 5: 精准移植 HTMLServer 业务同源路由

**Files:**
- Modify: `pkg/server/run/htmlserver.go`
- Create: `pkg/server/run/htmlserver_secure_routes_test.go`
- Modify: `pkg/server/run/htmlserver_lifecycle_test.go`

- [ ] **Step 1: 迁移安全路由失败测试**

覆盖 Manage、普通 Public、Private：

```go
func TestHTMLServerSameOriginRoutesUseOriginalSecurityChain(t *testing.T) {
	handler := preparedHTMLHandlerForTest(t, serviceWithSecureRoutes(t))
	requireRouteRejectedBeforeDo(t, handler, "/api/manage/shop/order/search")
	requireRouteRejectedBeforeDo(t, handler, "/api/shop/private-order")
	requireRouteExecutesAfterValidAuth(t, handler, "/api/shop/public-product")
}
```

另断言：

- `WithInternalCallers` 非空的 Public 路由返回 404。
- `server` 系统服务普通 Public/Private 不挂载。
- 相同 pattern 冲突在 listener 前返回错误。
- HTMLServer 未 Prepare 时 Start 不监听。

- [ ] **Step 2: 运行 HTML 安全测试确认 RED**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/run -run "HTMLServerSameOrigin|HTMLServerSecure|InternalCallers" -count=1
```

Expected: FAIL，当前 HTMLServer 仍用手工 URL 拆分和直接 Exec。

- [ ] **Step 3: 将 HTMLServer 拆为 Prepare 与 Start**

移植旧分支最终结构：

- `Prepare()`：构建并验证 mux。
- `Start()`：只负责监听已经准备好的 handler。
- `Stop()`：幂等关闭。

`Start()` 不得在内部临时注册路由，也不得使用 package-level `http.DefaultServeMux`。

- [ ] **Step 4: 统一 pattern 预占**

增加私有 registry：

```go
type htmlRouteRegistry struct {
	patterns map[string]string
}

func (registry *htmlRouteRegistry) reserve(pattern, owner string) error
```

空 pattern、重复 pattern、非法 same-origin 端口均返回带 owner 的错误。

- [ ] **Step 5: 挂载业务路由**

按稳定服务名排序处理 ServiceRouter：

1. Manage：全部挂载。
2. Public：过滤 `GetInternalCallers()` 非空项。
3. Private：普通业务服务挂载。
4. `server`：跳过普通 Public/Private。

每个 handler 必须来自 `rest.NewExternalRouterHandler`。

- [ ] **Step 6: 运行 HTMLServer 全包测试**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/run -count=1
```

Expected: PASS。

- [ ] **Step 7: 提交业务同源路由**

```bash
rtk git add pkg/server/run/htmlserver.go pkg/server/run/htmlserver_secure_routes_test.go pkg/server/run/htmlserver_lifecycle_test.go
rtk git commit -m "fix(web): route same-origin business APIs through security chain"
```

### Task 6: 精准移植 OpenAPI Host 与 Swagger 同源处理

**Files:**
- Modify: `pkg/server/run/openapi.go`
- Modify: `pkg/server/run/openapi_test.go`
- Create: `pkg/server/run/htmlserver_swagger_routes_test.go`
- Modify: `pkg/server/run/htmlserver.go`

- [ ] **Step 1: 迁移 IPv4/IPv6/非法 Host 失败测试**

覆盖：

```go
func TestOpenAPIServerURLUsesIPv6SafeHost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://[2001:db8::1]:8080/api/openapi", nil)
	doc := GetOpenApiForPort(req, 9090, testServiceRouter(t))
	require.Equal(t, "http://[2001:db8::1]:9090", doc.Servers[0].URL)
}

func TestOpenAPIServerURLFallsBackForInvalidHost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/openapi", nil)
	req.Host = "bad host:::"
	doc := GetOpenApiForPort(req, 9090, testServiceRouter(t))
	require.Equal(t, "http://127.0.0.1:9090", doc.Servers[0].URL)
}
```

另覆盖端口 `0`、负数、`>65535` 和 Host 无端口。

- [ ] **Step 2: 运行 OpenAPI 测试确认 RED**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/run -run "OpenAPI.*Host|OpenAPI.*IPv6|SwaggerSameOrigin" -count=1
```

Expected: FAIL，当前 `openapi.go` 不重写 ViewPort servers URL。

- [ ] **Step 3: 移植安全 Host/端口函数**

私有函数职责：

- `validatedOpenAPIHost`
- `splitRequestHost`
- `validateSameOriginPort`
- `openAPIServerURL`

必须使用 `net.SplitHostPort`、`net.ParseIP`、`net.JoinHostPort`；禁止字符串切割 IPv6。

- [ ] **Step 4: 保持内外 OpenAPI 分层**

当前 `openapidoc.ExternalAudience` 与 `InternalAudience` 是唯一生成器，继续复用：

```go
func GetOpenApi(req *http.Request, services ...*router.ServiceRouter) interface{}
func GetInternalOpenApi(req *http.Request, services ...*router.ServiceRouter) interface{}
```

只新增 ViewPort server URL 包装，不复制第二套 schema/operation 生成逻辑。

ViewPort 注册：

- `/api/openapi`：ExternalAudience，匿名。
- `/api/internal/openapi`：复用当前 `pkg/server/api/public.OpenAPI` 的
  `ServerManageAuth` RouterInfo/执行链。

- [ ] **Step 5: 迁移 Swagger 同源测试**

断言 Swagger 文档中的 servers URL 指向 ViewPort，同源调用 Manage/Public/Private；内部专用
路由只出现在通过 `ServerManageAuth` 获取的内部文档。

- [ ] **Step 6: 运行 run/public 测试确认 GREEN**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/run ./pkg/server/api/public -count=1
```

Expected: PASS。

- [ ] **Step 7: 提交 OpenAPI 同源处理**

```bash
rtk git add pkg/server/run/openapi.go pkg/server/run/openapi_test.go pkg/server/run/htmlserver.go pkg/server/run/htmlserver_swagger_routes_test.go
rtk git commit -m "fix(openapi): serve ipv6-safe same-origin routes"
```

### Task 7: 适配示例的 Manage Auth 权威

**Files:**
- Modify: `examples/04-shop-performance/main/main.go`
- Modify: `examples/05-shop-casdoor-rbac/main/main.go`
- Create: `examples/06-shop-microservices/bootstrap/manageauth.go`
- Create: `examples/06-shop-microservices/bootstrap/manageauth_test.go`
- Modify: `examples/06-shop-microservices/main/all-in-one/main.go`
- Modify: `examples/06-shop-microservices/main/order/main.go`
- Modify: `examples/06-shop-microservices/main/supplier/main.go`
- Modify: `examples/06-shop-microservices/main/user/main.go`
- Create: `examples/07-shop-order-scale/bootstrap/manageauth.go`
- Create: `examples/07-shop-order-scale/bootstrap/manageauth_test.go`
- Modify: `examples/07-shop-order-scale/main/all-in-one/main.go`
- Modify: `examples/07-shop-order-scale/main/order/main.go`
- Modify: `examples/07-shop-order-scale/main/supplier/main.go`
- Modify: `examples/07-shop-order-scale/main/user/main.go`

- [ ] **Step 1: 迁移显式 peer 同步失败测试**

06/07 bootstrap 测试证明：

- 只同步显式 peer，默认只包含同进程 `server`。
- 不遍历并改写全局无关 ServiceContext。
- Access/Refresh Secret 和过期时间与权威一致。
- Casdoor 启用时按当前配置校验 shared revocation。

- [ ] **Step 2: 运行示例 bootstrap 测试确认 RED**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/06-shop-microservices/bootstrap ./examples/07-shop-order-scale/bootstrap -count=1
```

Expected: FAIL，Manage Auth 同步 helper 尚不存在。

- [ ] **Step 3: 移植 helper，删除旧配置引用**

从旧分支移植：

```go
func ApplySharedManageAuthFields(cfg *config.ServerConfig)
func SyncManageAuthFromAuthority(authorityService string, peers ...string)
```

必须移除旧补丁中的：

- `RunIp`
- `AttachServices`
- 全局 GetContexts 遍历

环境变量只读取现有 `SHOP_MANAGE_ACCESS_SECRET`、`SHOP_MANAGE_REFRESH_SECRET`；不硬编码生产
Secret。

- [ ] **Step 4: 在示例启动前设置权威**

使用 setter，并显式处理错误：

```go
if err := server.SetManageAuthAuthority(authorityService); err != nil {
	panic(err)
}
```

示例 `main` 无测试框架时，对 setter 错误使用明确 panic 或日志后退出，不能忽略。

- [ ] **Step 5: 运行示例单元与全仓编译**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/04-shop-performance/... ./examples/05-shop-casdoor-rbac/... ./examples/06-shop-microservices/... ./examples/07-shop-order-scale/... -run "^$" -count=1
```

Expected: PASS。

- [ ] **Step 6: 提交示例权威适配**

```bash
rtk git add examples/04-shop-performance/main/main.go examples/05-shop-casdoor-rbac/main/main.go examples/06-shop-microservices examples/07-shop-order-scale
rtk git commit -m "fix(examples): configure manage auth authority explicitly"
```

### Task 8: 运行移植门禁并更新分支审计

**Files:**
- Modify: `docs/codex/BRANCH_CONSOLIDATION_AUDIT.md`
- Modify: `pkg/server/README.md`

- [ ] **Step 1: 格式和删除能力扫描**

```bash
rtk git diff --check
rtk rg -n "Logto|AttachServices|AttachService|Observe|Notify|RunIp" pkg/server/run pkg/server/trans/rest examples/04-shop-performance examples/05-shop-casdoor-rbac examples/06-shop-microservices examples/07-shop-order-scale
```

Expected: 本批新增/修改内容没有旧能力引用；已有 removed-capability fixture 可出现于专门测试。

- [ ] **Step 2: 运行定向测试**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/safe ./pkg/server/authstate ./pkg/server/api/public ./pkg/server/trans/rest ./pkg/server/run -count=1
```

Expected: PASS。

- [ ] **Step 3: 运行 race**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/server/run ./pkg/server/trans/rest -count=1
```

Expected: PASS。

- [ ] **Step 4: 运行全仓顺序编译**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -p 1 ./... -run "^$" -count=1
```

Expected: exit 0；顺序执行避免集成 `TestMain` 并行争抢连续端口。

- [ ] **Step 5: 运行配置和发布契约**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy ./scripts/test.sh config-contract
GOCACHE=/private/tmp/core-codex-gocache rtk proxy ./scripts/test.sh release-contract
```

Expected: 两项 PASS；未创建 tag、未 push、未发布。

- [ ] **Step 6: 更新审计分类**

把已经完成的以下组改为“已合入”，记录新提交 SHA 和验证命令：

- `53a81cd..1ff92b4`
- `42f517d..206b914`
- `1da3b35`
- `9dd1274`
- `23a27cb`
- `dea0753`
- `9974e32..ac4550c`

只在对应行为全部进入 `main` 后更新。Web 构建链、启动 admission 和示例 07 fixture 继续保持
“需要补入”。

- [ ] **Step 7: 更新现行服务文档**

在 `pkg/server/README.md` 的认证与 OpenAPI 章节写明：

- 多 Manage 服务通过 `WebServer.SetManageAuthAuthority` 在启动前显式选择权威。
- `/api/web/bootstrap` 匿名但不包含敏感配置。
- ViewPort 同源代理复用原 Router 安全链。
- `/api/openapi` 匿名过滤内部路由，`/api/internal/openapi` 使用 `ServerManageAuth`。

删除或修正与当前行为冲突的旧描述，不复制设计文档全文。

- [ ] **Step 8: 提交审计和现行文档更新**

```bash
rtk git add docs/codex/BRANCH_CONSOLIDATION_AUDIT.md pkg/server/README.md
rtk git commit -m "docs: record web runtime auth main port"
```

- [ ] **Step 9: 证明仍未满足删除门禁**

```bash
rtk rg -n "\\| 需要补入 \\|" docs/codex/BRANCH_CONSOLIDATION_AUDIT.md
```

Expected: Web Admin 构建链、启动 admission/UAT 等条目仍存在；不得创建 archive tag、删除
`core-api-web` worktree 或删除 `feat/web-runtime-auth` 分支。
