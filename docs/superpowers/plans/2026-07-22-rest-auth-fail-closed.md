# REST 认证内层 fail-closed 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. 仓库 `AGENTS.md` 要求任务在主线程顺序执行，不派发子代理。

**Goal:** 让受保护的 REST 路由在进入 `RouterInfo.Exec` 前验证可信身份，缺失或认证域不匹配时返回稳定 401。

**Architecture:** `RouteHandler` 复用 `verifiedRequestIdentity` 检查内部 JWT 或 Logto 已验证上下文。新增一个私有策略解析函数，让外层中间件和内层 handler 使用同一套 User/Manage Auth 及 JWT/Logto 选择规则。不修改 `Request` 公共 API，不重复 token、撤销权威或 Hook 验证。

**Tech Stack:** Go 1.26、`net/http`、`httptest`、Core `PublicErrorContract`、JWT/Logto 认证上下文、Go race detector、`release-contract`。

---

## 文件映射

| 文件 | 职责 |
| --- | --- |
| `pkg/server/trans/rest/server.go` | 解析路由认证策略，在 `RouteHandler` 内执行 fail-closed 身份检查 |
| `pkg/server/trans/rest/server_security_test.go` | 覆盖无身份 401、Public 放行、内部 JWT 放行、Logto 放行和 User/Manage 认证域隔离 |

### Task 1: 锁定最内层 REST 认证边界

**Files:**

- Modify: `pkg/server/trans/rest/server_security_test.go`
- Read: `pkg/server/trans/rest/authrequest_test.go`

- [ ] **Step 1: 保留现有失败样例并补充不泄露断言**

为 `server_security_test.go` 增加中文文件级注释，并在 `TestRouteHandlerRejectsNilAuthenticatedRequest` 的状态码断言后增加：

```go
require.NotContains(t, recorder.Body.String(), "verified access identity missing")
require.NotContains(t, recorder.Body.String(), "internal server error")
```

文件开头应为：

```go
// 本文件验证 REST 启动选项、安全响应头和最内层认证拒绝边界。
package rest
```

- [ ] **Step 2: 运行现有用例确认 RED**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/trans/rest -run '^TestRouteHandlerRejectsNilAuthenticatedRequest$' -count=1 -v
```

Expected: FAIL，明确显示 `expected: 401` 与 `actual: 500`。日志中可见 `router_execution_panicked`，但响应体不应暴露内部 panic 原因。

- [ ] **Step 3: 增加可执行 Router 测试工厂**

在 `nilRequestTestRouter` 方法之后增加：

```go
func newExecutableRouteHandlerTestContext(
	name, path string,
	pathType types.ApiType,
	requiresAuth bool,
) *router.ServiceContext {
	info := &types.RouterInfo{
		Path: path, Method: http.MethodGet, Auth: requiresAuth,
		PathType: pathType, ServiceName: name,
	}
	api := &nilRequestTestRouter{info: info}
	info.SetInstance(api)
	service := &nilRequestTestService{name: name, api: api}
	sc := &router.ServiceContext{
		Config: config.NewServiceDefaultConfig(name, 18083),
		Service: &types.Service{Name: name, Routers: []types.IRouter{api}},
	}
	sc.Config.Auth.AccessSecret = "user-access-secret"
	sc.Config.ManageAuth.AccessSecret = "manage-access-secret"
	sc.Router = router.NewServiceRouter(sc, service)
	return sc
}

func setRequestPath(request *http.Request, path string) {
	request.URL.Path = path
	request.RequestURI = path
}
```

- [ ] **Step 4: 增加 Public、JWT 和 Logto 放行特征测试**

在同一测试文件增加：

```go
func TestRouteHandlerAllowsPublicRequestWithoutIdentity(t *testing.T) {
	sc := newExecutableRouteHandlerTestContext(
		"public-route-test", "/public", types.PublicType, false,
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/public", nil)
	request.RemoteAddr = "198.51.100.10:4321"

	RouteHandler(sc.Router).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestRouteHandlerAllowsVerifiedInternalJWTIdentity(t *testing.T) {
	const path = "/private"
	sc := newExecutableRouteHandlerTestContext(
		"verified-user-route-test", path, types.PrivateType, true,
	)
	request := authenticatedRequest(t, sc.Config.Auth.AccessSecret, types.AuthIdentity{
		UID: "user-1", Username: "用户一", AuthType: types.AuthTypeUser,
	})
	setRequestPath(request, path)
	recorder := httptest.NewRecorder()
	handler := internalJWTAuthorize(
		sc.Config.Auth.AccessSecret,
		types.AuthTypeUser,
		RouteHandler(sc.Router),
	)

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestRouteHandlerAllowsVerifiedLogtoIdentity(t *testing.T) {
	const path = "/private-logto"
	sc := newExecutableRouteHandlerTestContext(
		"verified-logto-route-test", path, types.PrivateType, true,
	)
	sc.Config.Auth.Logto.Enable = true
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.RemoteAddr = "198.51.100.10:4321"
	ctx := context.WithValue(request.Context(), "uid", "logto-user")
	request = request.WithContext(context.WithValue(ctx, "uname", "Logto User"))
	recorder := httptest.NewRecorder()

	RouteHandler(sc.Router).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
}
```

同时在 import 中增加 `context`。

- [ ] **Step 5: 增加 User token 不能进入 Manage 路由的 RED 测试**

```go
func TestRouteHandlerRejectsVerifiedUserIdentityOnManageRoute(t *testing.T) {
	const path = "/manage"
	sc := newExecutableRouteHandlerTestContext(
		"manage-domain-route-test", path, types.ManageType, true,
	)
	request := authenticatedRequest(t, sc.Config.Auth.AccessSecret, types.AuthIdentity{
		UID: "user-1", Username: "用户一", AuthType: types.AuthTypeUser,
	})
	setRequestPath(request, path)
	recorder := httptest.NewRecorder()
	handler := internalJWTAuthorize(
		sc.Config.Auth.AccessSecret,
		types.AuthTypeUser,
		RouteHandler(sc.Router),
	)

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}
```

- [ ] **Step 6: 运行路由边界用例确认 RED 和已有成功语义**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/trans/rest -run '^TestRouteHandler' -count=1 -v
```

Expected:

- `TestRouteHandlerRejectsNilAuthenticatedRequest` FAIL，实际为 500
- `TestRouteHandlerRejectsVerifiedUserIdentityOnManageRoute` FAIL，实际为 200
- Public、已验证内部 JWT 和 Logto 用例 PASS

这些结果证明失败来自内层门禁缺失，不是测试工厂或 token 签发错误。

### Task 2: 复用认证策略并在业务执行前 fail closed

**Files:**

- Modify: `pkg/server/trans/rest/server.go`
- Test: `pkg/server/trans/rest/server_security_test.go`

- [ ] **Step 1: 提取外层和内层共用的路由认证策略**

在 `selectAuthMode` 之后增加：

```go
func resolveRouteAuthPolicy(
	rou *router.ServiceRouter,
	path string,
) (config.AuthSecret, types.AuthType, authMode) {
	auth := rou.Service.Config.Auth
	authType := types.AuthTypeUser
	if rou.HasRouter(path, types.ManageType) {
		auth = rou.Service.Config.ManageAuth
		authType = types.AuthTypeManage
	}
	return auth, authType, selectAuthMode(auth)
}
```

将 `handers` 中的重复策略选择：

```go
auth := own.context.Config.Auth
authType := types.AuthTypeUser
if own.context.Router.HasRouter(path, types.ManageType) {
	auth = own.context.Config.ManageAuth
	authType = types.AuthTypeManage
}
mode := selectAuthMode(auth)
```

替换为：

```go
auth, authType, mode := resolveRouteAuthPolicy(own.context.Router, path)
```

- [ ] **Step 2: 在 `RouteHandler` 调用 `Exec` 前验证可信身份**

在 `info == nil` 分支之后、`info.Exec(req)` 之前增加：

```go
if info.GetAuth() {
	_, authType, mode := resolveRouteAuthPolicy(rou, info.GetPath())
	identity, _, err := verifiedRequestIdentity(r, rou.Service, authType, mode)
	if err != nil {
		contract := types.ResolvePublicError(err)
		logAuthRequestDenied(rou.Service, info, authType, identity, contract)
		writePublicErrorContract(w, contract)
		return
	}
}
```

该检查不调用撤销 manager 或认证 Hook。外层 `authRequestHandler` 仍是这两类逻辑的唯一 owner。

- [ ] **Step 3: 运行定向用例确认 GREEN**

Run:

```bash
rtk gofmt -w pkg/server/trans/rest/server.go pkg/server/trans/rest/server_security_test.go
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/trans/rest -run '^TestRouteHandler' -count=1 -v
```

Expected: 全部 `TestRouteHandler*` PASS；无身份和认证域错误返回 401，其他三类请求返回 200。

- [ ] **Step 4: 运行完整 REST 单元测试和 race**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/trans/rest -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/server/trans/rest -count=1
```

Expected: 两条命令均 PASS，race detector 无报告。如果沙箱限制 `httptest` 监听本地端口，使用同一命令申请沙箱外重跑，不把沙箱失败记为代码通过。

- [ ] **Step 5: 运行日志和发布契约门禁**

Run:

```bash
rtk proxy ./scripts/check-logging.sh
GOCACHE=/private/tmp/core-codex-gocache rtk proxy ./scripts/test.sh release-contract
```

Expected: 日志静态门禁 PASS；`release-contract` 中公共 API 检查、REST 安全测试和后续包测试全部 PASS。

- [ ] **Step 6: 确认公共面和工作区**

Run:

```bash
rtk git diff --check
rtk git diff -- pkg/server/trans/rest/server.go pkg/server/trans/rest/server_security_test.go
rtk git status --short
```

Expected: 只有两个 REST 文件修改，没有新导出符号、HTTP 路径或 JSON 字段变化。

- [ ] **Step 7: 提交 REST 安全修复**

```bash
rtk git add pkg/server/trans/rest/server.go pkg/server/trans/rest/server_security_test.go
rtk git commit -m 'fix(rest): reject missing verified identity'
```

### Task 3: 五轴复核并交接 Otel 升级

**Files:**

- Review: `pkg/server/trans/rest/server.go`
- Review: `pkg/server/trans/rest/server_security_test.go`
- Verify: `docs/superpowers/specs/2026-07-22-rest-auth-fail-closed-design.md`

- [ ] **Step 1: 完成五轴复核**

逐项核对：

1. Correctness：拒绝发生在 `RouterInfo.Exec` 之前，Public/JWT/Logto 成功路径保持不变
2. Readability：User/Manage 和 JWT/Logto 策略只在一个私有函数内选择
3. Architecture：外层中间件仍负责 token、撤销权威和 Hook，内层只验证可信上下文存在
4. Security：User token 不能进入 Manage 路由，401 响应和日志不包含 token/claims/cause
5. Performance：没有重复 JWT 签名验证、网络请求、撤销查询或 Hook 执行

- [ ] **Step 2: 运行提交后的最终门禁**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/trans/rest -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/server/trans/rest -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy ./scripts/test.sh release-contract
rtk git status --short
rtk git log --oneline -4
```

Expected: 三条门禁全部 PASS，工作区干净，REST 修复和设计/计划均有独立提交。

- [ ] **Step 3: 进入独立 Otel 升级任务**

只有 Step 2 全部通过后，才创建 Otel 升级的独立计划和提交。REST 提交不修改 `go.mod`、`go.sum` 或任何 OpenTelemetry 依赖。
