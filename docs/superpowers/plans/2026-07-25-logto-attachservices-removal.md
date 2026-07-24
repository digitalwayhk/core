# Logto、旧服务依赖与顶层配置清理实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 从 Core 中彻底删除 Logto 认证和旧 ServiceAttach/Observe 服务依赖系统，同步调用统一使用 `ServiceContext + ServiceResolver`，异步调用统一使用 EventBridge。

**Architecture:** REST 受保护路由收敛为框架 Access Token 验证，Casdoor 继续负责现有外部身份生命周期和撤销检查。配置和运行时均不再保存 AttachService；同进程调用走 `ServiceContext` 注册表，跨进程调用走 `ServiceResolver`，跨服务事件通过 `ServiceContext.SubscribeEvent` 和 EventBridge 显式声明。

**Tech Stack:** Go 1.26、go-zero REST/config、Core `ServiceContext`/`ServiceResolver`、`testify/require`、apidiff、release-contract。

---

## 文件边界

- `pkg/server/config/serverconfig.go`：只负责现行配置、默认值和旧 JSON 清理，不再声明 Logto、静态地址或运行时派生字段。
- `pkg/server/trans/rest/server.go`、`authrequest.go`：只组装和验证框架 Access Token，不再持有 JWKS 生命周期。
- `pkg/server/types/service.go`、`server.go`、`routerinfo.go`：删除 ServiceAttach、SubscribeRouters 和 Router Observe 生命周期。
- `pkg/server/router/servicecontext.go`、`pkg/server/run/server.go`：删除旧依赖关系构建、Observe 注册和配置地址回填。
- `pkg/server/api/public/{attach,observe,notify}.go`、`pkg/server/api/private/setserviceaddress.go`、`pkg/server/api/release/routes.go`：删除旧系统路由。
- `pkg/server/api/manage/servicemanage.go`：删除 Attach、调用路由和 Observe 关系展示/编辑。
- `internal/compat/removed_capabilities_test.go`：锁定已删除配置、包、依赖和现行文档契约。
- `docs/codex/*`、`CHANGELOG.md`、公开 API 基线：记录迁移和 MAJOR 破坏范围。

### Task 1: 建立 Logto 删除契约

**Files:**
- Create: `internal/compat/removed_capabilities_test.go`
- Modify: `pkg/server/trans/rest/server_security_test.go`
- Modify: `pkg/server/trans/rest/authrequest_test.go`

- [ ] **Step 1: 写配置与依赖失败测试**

创建中文文件级注释，并加入以下测试：

```go
package compat

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/stretchr/testify/require"
)

func TestRemovedAuthenticationAndServiceAttachStayAbsent(t *testing.T) {
	_, hasLogto := reflect.TypeOf(config.AuthSecret{}).FieldByName("Logto")
	require.False(t, hasLogto)

	_, hasAttachServices := reflect.TypeOf(config.ServerConfig{}).FieldByName("AttachServices")
	require.False(t, hasAttachServices)
}

func TestRemovedLogtoDependenciesStayAbsent(t *testing.T) {
	root := repositoryRoot(t)
	goMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	require.NotContains(t, string(goMod), "github.com/MicahParks/keyfunc")
	require.NotContains(t, string(goMod), "github.com/golang-jwt/jwt/v5")

	_, err = os.Stat(filepath.Join(root, "pkg/server/safe/logto"))
	require.ErrorIs(t, err, os.ErrNotExist)
}
```

- [ ] **Step 2: 运行测试并确认 RED**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache go test ./internal/compat \
  -run 'TestRemoved(AuthenticationAndServiceAttachStayAbsent|LogtoDependenciesStayAbsent)' \
  -count=1
```

Expected: FAIL，指出 `AuthSecret.Logto`、`ServerConfig.AttachServices`、Logto 依赖或目录仍存在。

- [ ] **Step 3: 把 REST 成功路径测试改为现行 JWT 契约**

删除 `TestNewLogtoHandlerRejectsInvalidConfig`、`TestRouteHandlerAllowsVerifiedLogtoIdentity` 和 `TestLogtoIdentityRunsBusinessHookWithoutCasdoorAuthority`。保留并强化现有 JWT 测试，断言 User、Manage、ServerManage 域 token 不可串用：

```go
func TestVerifiedAccessIdentityMustMatchRouteAuthType(t *testing.T) {
	for _, tc := range []struct {
		name     string
		route    types.AuthType
		identity types.AuthType
	}{
		{name: "user token cannot enter manage", route: types.AuthTypeManage, identity: types.AuthTypeUser},
		{name: "manage token cannot enter user", route: types.AuthTypeUser, identity: types.AuthTypeManage},
		{name: "server-manage token cannot enter manage", route: types.AuthTypeManage, identity: types.AuthTypeServerManage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request = request.WithContext(context.WithValue(
				request.Context(),
				verifiedAccessContextKey{},
				verifiedAccessContext{identity: types.AuthIdentity{UID: "1", AuthType: tc.identity}},
			))
			_, _, err := verifiedRequestIdentity(request, testServiceContext(t), tc.route)
			require.Error(t, err)
		})
	}
}
```

- [ ] **Step 4: 运行 REST 测试并记录当前编译失败**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache go test ./pkg/server/trans/rest -count=1
```

Expected: FAIL，`verifiedRequestIdentity` 仍要求 `authMode` 参数，证明测试已锁定简化后的 API。

### Task 2: 删除 Logto 实现并收敛 REST 认证

**Files:**
- Delete: `pkg/server/safe/logto/authmiddleware.go`
- Delete: `pkg/server/safe/logto/authmiddleware_test.go`
- Modify: `pkg/server/config/serverconfig.go`
- Modify: `pkg/server/trans/rest/server.go`
- Modify: `pkg/server/trans/rest/authrequest.go`
- Modify: `pkg/server/types/auth.go`
- Modify: `scripts/test.sh`

- [ ] **Step 1: 删除公开配置与身份常量**

将 `AuthSecret` 收敛为：

```go
type AuthSecret struct {
	AccessSecret  string
	AccessExpire  int64
	RefreshSecret string
	RefreshExpire int64
	CasDoor       CasDoorConfig
}
```

删除 `LogtoConfig`、三处默认 Logto 初始化和 `AuthProviderLogto`。

- [ ] **Step 2: 删除 REST 的模式分支和 HandlerFactory**

`Server` 不再包含 `logtoHandlers`。`resolveRouteAuthPolicy` 返回两项：

```go
func resolveRouteAuthPolicy(
	rou *router.ServiceRouter,
	path string,
) (config.AuthSecret, types.AuthType) {
	auth := rou.Service.Config.Auth
	authType := types.AuthTypeUser
	if rou.HasRouter(path, types.ServerManagerType) {
		auth = rou.Service.Config.ServerManageAuth
		authType = types.AuthTypeServerManage
	} else if rou.HasRouter(path, types.ManageType) {
		auth = rou.Service.Config.ManageAuth
		authType = types.AuthTypeManage
	}
	return auth, authType
}
```

受保护路由统一组装：

```go
auth, authType := resolveRouteAuthPolicy(own.context.Router, path)
handler = authRequestHandler(own.context, api, authType, handler)
handler = internalJWTAuthorize(auth.AccessSecret, authType, handler)
```

删除 `newLogtoHandler` 两个构造入口及 New/Stop/register 中的 Logto Close。

- [ ] **Step 3: 删除 Logto 身份解析**

把签名改为：

```go
func verifiedRequestIdentity(
	r *http.Request,
	sc *router.ServiceContext,
	authType types.AuthType,
) (types.AuthIdentity, map[string]interface{}, error)
```

删除 `"uid"`/`"uname"` Logto context 分支，仅接受 `verifiedAccessContextKey{}` 注入的验签结果。同步删除 `authRequestHandler` 的 `mode` 参数。

- [ ] **Step 4: 删除包并更新 security gate**

删除 `pkg/server/safe/logto`，并从 `scripts/test.sh security` 的 `security_packages` 中移除 `./pkg/server/safe/logto`。

- [ ] **Step 5: 运行 Logto 删除契约和 REST race**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache go test ./internal/compat \
  -run 'TestRemovedLogtoDependenciesStayAbsent' -count=1
GOCACHE=/private/tmp/core-codex-gocache go test -race \
  ./pkg/server/config ./pkg/server/trans/rest ./pkg/server/types -count=1
```

Expected: Logto 目录相关断言仍会因 go.mod 依赖存在而 FAIL；REST/config/types 测试 PASS。

### Task 3: 删除无用顶层配置并建立运行时地址

**Files:**
- Create: `pkg/server/config/serverconfig_removed_features_test.go`
- Modify: `pkg/server/config/serverconfig.go`
- Modify: `pkg/server/config/serverconfig_migration_error_test.go`
- Modify: `pkg/server/router/servicecontext.go`
- Modify: `pkg/server/router/request.go`
- Modify: `pkg/server/router/reponse.go`
- Modify: `pkg/server/router/serviceresolver.go`
- Modify: `pkg/server/trans/quic/server.go`
- Modify: `pkg/server/trans/quic/server_stub.go`
- Modify: `examples/06-shop-microservices/bootstrap/config.go`
- Modify: `examples/07-shop-order-scale/bootstrap/config.go`

- [ ] **Step 1: 写旧 JSON 清理和字段缺失测试**

```go
func TestMigrateConfigRemovesRetiredTopLevelFields(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.json")
	data := []byte(`{
	  "Name":"legacy",
	  "RunIp":"127.0.0.1",
	  "ParentServerIP":"127.0.0.2",
	  "Debug":true,
	  "CustomerDataList":[{"Key":"legacy","Value":"value"}],
	  "AttachServices":{"orders":{"Name":"orders","Address":"127.0.0.1","Port":8081}},
	  "Auth":{"Logto":{"Enable":true}},
	  "ManageAuth":{"Logto":{"Enable":true}},
	  "ServerManageAuth":{"Logto":{"Enable":true}},
	  "FutureField":{"keep":true}
	}`)
	require.NoError(t, os.WriteFile(file, data, 0o600))
	require.NoError(t, migrateConfig(file))

	var values map[string]interface{}
	migrated, err := os.ReadFile(file)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(migrated, &values))
	for _, key := range []string{
		"RunIp", "ParentServerIP", "Debug", "CustomerDataList", "AttachServices",
	} {
		require.NotContains(t, values, key)
	}
	require.Equal(t, map[string]interface{}{"keep": true}, values["FutureField"])
	for _, key := range []string{"Auth", "ManageAuth", "ServerManageAuth"} {
		require.NotContains(t, values[key].(map[string]interface{}), "Logto")
	}
}

func TestRetiredTopLevelFieldsAreNotPublicConfig(t *testing.T) {
	serverType := reflect.TypeOf(ServerConfig{})
	for _, name := range []string{
		"RunIp", "ParentServerIP", "Debug", "CustomerDataList", "AttachServices",
	} {
		_, ok := serverType.FieldByName(name)
		require.False(t, ok, name)
	}
}
```

- [ ] **Step 2: 运行测试并确认 RED**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache go test ./pkg/server/config \
  -run 'Test(MigrateConfigRemovesRetiredTopLevelFields|RetiredTopLevelFieldsAreNotPublicConfig)' \
  -count=1
```

Expected: FAIL，旧键和公开字段仍存在。

- [ ] **Step 3: 实现幂等配置迁移**

```go
func migrateRetiredTopLevelConfig(m map[string]interface{}) bool {
	changed := false
	for _, key := range []string{
		"RunIp", "ParentServerIP", "Debug", "CustomerDataList", "AttachServices",
	} {
		if _, ok := m[key]; ok {
			delete(m, key)
			changed = true
		}
	}
	for _, key := range []string{"Auth", "ManageAuth", "ServerManageAuth"} {
		auth, ok := m[key].(map[string]interface{})
		if !ok {
			continue
		}
		if _, ok := auth["Logto"]; ok {
			delete(auth, "Logto")
			changed = true
		}
	}
	return changed
}
```

从 `migrateConfig` 调用该函数。删除 `RunIp`、`ParentServerIP`、`Debug`、`CustomerDataList`、`CustomerData`、`GetCustomerData`、`AttachServices`、`AttachAddress` 和 `SetAttachService`。

- [ ] **Step 4: 建立 ServiceContext 运行时地址**

在 `ServiceContext` 增加不可持久化字段并提供只读入口：

```go
type ServiceContext struct {
	// ... existing fields ...
	runtimeAddress string
}

// RuntimeAddress 返回当前实例实际使用的节点地址。
func (own *ServiceContext) RuntimeAddress() string {
	if own == nil {
		return ""
	}
	if own.Config != nil {
		if address := strings.TrimSpace(own.Config.Cluster.AdvertiseAddress); address != "" {
			return address
		}
	}
	return own.runtimeAddress
}
```

在创建 `ServiceContext` 时只计算一次 `runtimeAddress = utils.GetLocalIP()`。将 router response、request target、resolver、membership 和 QUIC 中的 `Config.RunIp` 替换为 `RuntimeAddress()`。配置验证使用当前动态本地地址：

```go
if err := con.Transport.ValidateForServer(con.Cluster, utils.GetLocalIP()); err != nil {
	return err
}
```

示例 bootstrap 不再写 `cfg.RunIp`；本地固定地址使用 `cfg.Cluster.AdvertiseAddress = "127.0.0.1"`。

- [ ] **Step 5: 验证配置和运行时地址 GREEN**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache go test -race \
  ./pkg/server/config ./pkg/server/router ./pkg/server/trans/quic \
  ./examples/06-shop-microservices/... ./examples/07-shop-order-scale/... \
  -count=1
```

Expected: PASS。

### Task 4: 删除 ServiceAttach 与 Observe/Notify 系统

**Files:**
- Delete: `pkg/server/api/public/attach.go`
- Delete: `pkg/server/api/public/observe.go`
- Delete: `pkg/server/api/public/notify.go`
- Delete: `pkg/server/api/private/setserviceaddress.go`
- Modify: `pkg/server/types/service.go`
- Modify: `pkg/server/types/server.go`
- Modify: `pkg/server/types/observable.go`
- Modify: `pkg/server/types/routerinfo.go`
- Modify: `pkg/server/router/servicecontext.go`
- Modify: `pkg/server/router/servicerouter.go`
- Modify: `pkg/server/run/server.go`
- Modify: `pkg/server/api/manage/servicemanage.go`
- Modify: `pkg/server/api/release/routes.go`
- Modify: `pkg/server/api/release/routes_test.go`
- Modify: all concrete `IService` implementations returned by `rg -l "SubscribeRouters" pkg internal examples --glob '*.go'`

- [ ] **Step 1: 写公开类型和系统路由失败契约**

扩充 `internal/compat/removed_capabilities_test.go` 的源码契约，检查以下标识符不再存在：

```go
for _, fragment := range []string{
	"type ServiceAttach struct",
	"AttachService map[string]*ServiceAttach",
	"SubscribeRouters() []*ObserveArgs",
	"type ObserveArgs struct",
	"type NotifyArgs struct",
} {
	require.NotContains(t, source, fragment)
}
```

增加路由契约：

```go
func TestRemovedServiceAttachmentRoutesStayAbsent(t *testing.T) {
	removed := map[string]bool{
		"/api/servermanage/attach":            true,
		"/api/servermanage/observe":           true,
		"/api/servermanage/notify":            true,
		"/api/servermanage/setserviceaddress": true,
	}
	for _, item := range Routers() {
		require.False(t, removed[item.RouterInfo().GetPath()], item.RouterInfo().GetPath())
	}
}
```

- [ ] **Step 2: 运行删除契约并确认 RED**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache go test \
  ./internal/compat ./pkg/server/api/release \
  -run 'TestRemoved(ServiceAttachment|ServiceAttachmentRoutes)' -count=1
```

Expected: FAIL，旧类型和路由仍存在。

- [ ] **Step 3: 删除公开 ServiceAttach/Observe 类型**

`types.Service` 收敛为：

```go
type Service struct {
	Name           string
	Routers        []IRouter `json:"-"`
	HttpServer     IRunServer `json:"-"`
	internalServer []IRunServer `json:"-"`
	Instance       interface{} `json:"-"`
}
```

`types.IService` 收敛为：

```go
type IService interface {
	ServiceName() string
	Routers() []IRouter
}
```

删除 `ServiceAttach`、`IAttachService`、`ObserveState`、`ObserveArgs`、`NotifyArgs`、`NewObserveArgs` 和未使用的旧 `Publisher/Subscriber`。保留仍被调用链使用的 `TargetInfo`，将其放入 `pkg/server/types/targetinfo.go`。

- [ ] **Step 4: 删除 RouterInfo 的隐式通知生命周期**

删除 `RouterInfo.Subscriber`、`eventCancels`、`Subscribe`、`UnSubscribe`、`observeEventType`、`subscriberSnapshot`、`requestNotify`、`responseNotify`、`errorNotify` 和 `publishObservation`。从 Router 执行路径移除三类异步通知调用；业务事件只能由显式 EventBridge 发布。

- [ ] **Step 5: 删除 ServiceContext 与 WebServer 旧链接流程**

`initService` 不再读取 `SubscribeRouters` 或扫描 `InitRequest.CallRouters` 建依赖表：

```go
func initService(iser types.IService, sc *ServiceContext) *types.Service {
	service := &types.Service{
		Name:     strings.ToLower(iser.ServiceName()),
		Routers:  iser.Routers(),
		Instance: iser,
	}
	req := &InitRequest{}
	for _, route := range service.Routers {
		safedo(route, req)
	}
	return service
}
```

删除 `addAttachService`、`SetAttachServiceAddress`、`GetServerConfig`、`RegisterObserveSub`、`RegisterObserve`、observe retry map、`observeCall` 和 `SendNotify`。`WebServer` 不再调用 `linkServiceContexts`。

- [ ] **Step 6: 删除四个路由和 Manage 展示**

从 `release.Routers()` 删除 `Attach`、`Observe`、`Notify`、`SetServiceAddress` 并删除四个实现文件。`ServiceManage` 删除 `AttachInfo`、`CallRouterInfo`、`ObserverRouterInfo` 以及对应 View/Child/Validation/DoBefore 逻辑，只保留当前服务运行信息。

- [ ] **Step 7: 删除所有 IService 空兼容方法并验证**

对以下区域删除 `SubscribeRouters()` 方法及只验证其返回 nil 的断言：

```text
examples/01-simple-shop
examples/02-shop-payment
examples/03-shop-inheritance
examples/04-shop-performance
examples/05-shop-casdoor-rbac
examples/06-shop-microservices
examples/07-shop-order-scale
examples/integration/casdoor-auth-lifecycle
pkg/persistence
pkg/server
internal/compat
```

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache go test -race \
  ./pkg/server/types ./pkg/server/router ./pkg/server/run \
  ./pkg/server/api/manage ./pkg/server/api/public ./pkg/server/api/private \
  ./pkg/server/api/release ./pkg/server/event ./internal/compat \
  -count=1
```

Expected: PASS。

### Task 5: 清理依赖并登记破坏性迁移

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `CHANGELOG.md`
- Modify: `docs/codex/BREAKING_CHANGE_APPROVAL.md`
- Modify: `docs/codex/DEPRECATION_REGISTER.md`
- Modify: `docs/codex/API_COMPATIBILITY_SURFACE.md`
- Modify: `docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`
- Modify: `docs/codex/FRAMEWORK_USAGE_GUIDE.md`
- Modify: `docs/codex/GRPC_TRANSPORT_MIGRATION.md`
- Modify: `pkg/server/README.md`
- Modify: `.codex/skills/use-digitalway-core/SKILL.md`
- Modify: `.codex/skills/use-digitalway-core/references/core-backend-api.md`
- Modify: `.github/copilot/skills/core-backend-api.md`
- Modify: `api/public-api/*`

- [ ] **Step 1: 整理模块依赖**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache go mod tidy
go mod why -m github.com/MicahParks/keyfunc/v2
go mod why -m github.com/golang-jwt/jwt/v5
```

Expected: 两个 `go mod why` 均输出主模块不需要该模块；`go.mod`/`go.sum` 不再包含它们。

- [ ] **Step 2: 登记 MAJOR 破坏性变更**

在 `BREAKING_CHANGE_APPROVAL.md` 新增 `logto-legacy-service-config-removal-v1`，明确：

```markdown
1. 删除 Logto Go API、配置和运行时，消费方迁移到框架 Access Token 或 Casdoor。
2. 删除 ServerConfig.AttachServices、RunIp、ParentServerIP、Debug、CustomerDataList 及相关类型和方法。
3. 删除 Service.AttachService、ServiceAttach、IService.SubscribeRouters、ObserveArgs、NotifyArgs 和四个旧系统路由。
4. 同步调用迁移到 ServiceContext + ServiceResolver；异步调用迁移到 ServiceContext.SubscribeEvent + EventBridge。
5. 固定广播地址改用 Cluster.AdvertiseAddress；HTTP 监听继续使用 RestConf.Host/Port。
```

把 `DEPRECATION_REGISTER.md` 中 AttachServices 条目标记为已按本批准删除，并登记 Observe/Notify 迁移证据；在 `CHANGELOG.md` 的 `Removed` 段列出公开类型、字段、路由和依赖。

- [ ] **Step 3: 更新现行能力文档**

配置矩阵删除三组 Logto、旧服务依赖和五个顶层配置字段；认证指南改为“框架 Access Token 或 Casdoor”；服务发现指南明确静态配置和 Router Observe 已移除，异步事件使用 EventBridge。历史 `plans/`、旧审查和旧 specs 不改写。

- [ ] **Step 4: 生成并审查公开 API 基线**

先运行：

```bash
GOCACHE=/private/tmp/core-codex-gocache ./scripts/check-public-api.sh
```

Expected: FAIL，报告仅包含本次批准的 Logto、ServiceAttach/Observe 和顶层配置删除。

更新基线：

```bash
GOCACHE=/private/tmp/core-codex-gocache ./scripts/update-public-api.sh
GOCACHE=/private/tmp/core-codex-gocache ./scripts/test.sh public-api
```

Expected: PASS。

- [ ] **Step 5: 扩充仓库删除契约**

在删除契约中同时检查现行指南和 `scripts/test.sh` 不再出现 Logto、ServiceAttach/Observe 或已删除顶层配置的支持声明；允许历史设计中保留事实记录。运行：

```bash
GOCACHE=/private/tmp/core-codex-gocache go test ./internal/compat -count=1
```

Expected: PASS。

### Task 6: 全量验证与提交

**Files:**
- Verify: all changed files

- [ ] **Step 1: 格式与静态检查**

Run:

```bash
gofmt -w pkg/server internal/compat
git diff --check
./scripts/check-logging.sh
```

Expected: 均为 exit 0，`gofmt -l` 无相关输出。

- [ ] **Step 2: 定向 race**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache go test -race \
  ./pkg/server/config \
  ./pkg/server/trans/rest \
  ./pkg/server/router \
  ./pkg/server/run \
  ./pkg/server/api/manage \
  ./pkg/server/api/release \
  ./pkg/server/types \
  ./pkg/server/event \
  ./internal/compat \
  -count=1
```

Expected: PASS，无 race。

- [ ] **Step 3: 安全与发布门禁**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache ./scripts/test.sh security
GOCACHE=/private/tmp/core-codex-gocache ./scripts/test.sh api-compat
GOCACHE=/private/tmp/core-codex-gocache ./scripts/test.sh release-contract
```

Expected: 全部 PASS。

- [ ] **Step 4: 最终残留扫描**

Run:

```bash
rg -n "AuthProviderLogto|LogtoConfig|safe/logto|authModeLogto|AttachServices|ServiceAttach|SubscribeRouters|ObserveArgs|NotifyArgs|SetServiceAddress|Config\\.RunIp|ParentServerIP|CustomerDataList" \
  pkg internal scripts go.mod
```

Expected: 无输出；历史计划和历史设计可以保留事实记录。

- [ ] **Step 5: 提交**

```bash
git add \
  pkg/server internal/compat scripts go.mod go.sum api/public-api \
  CHANGELOG.md docs/codex .codex .github/copilot/skills/core-backend-api.md
git commit -m "refactor: remove legacy auth and service attachment"
```

Expected: 提交成功，工作区 clean。
