# Manage Control Plane Store Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 Core 整个 Manage 控制面可由 `etc/server.json` 选择 SQLite 或共享 MySQL，并在监听前完成表初始化，同时修正 09 示例的配置与启动接线。

**Architecture:** `server.json` 是唯一存储配置源；`WebServer` 构建控制面 runtime，并在内部 models/store 绑定同一连接。系统 Manage CRUD 只调用 `smodels.NewManageModelList[T]()`，不得接收 `IDataAction`；默认 PrincipalProvider 与 RBAC Authorizer 使用同一 runtime。管理员主体和角色关系属于 Core，消费方不建立对应模型、repository/store 或 `ConfigureManageModels` 桥接；外部 IAM 才覆盖 `IManageRoleProvider`。

**Tech Stack:** Go、go-zero JSON config、Core `IDataAction`/`ModelList`、GORM SQLite/MySQL、`testify/require`。

---

### Task 1: 定义只属于 `server.json` 的 ManageStore 配置

**Files:**
- Create: `pkg/server/config/managestore.go`
- Modify: `pkg/server/config/serverconfig.go`
- Modify: `pkg/server/config/adminview_test.go`
- Test: `pkg/server/config/managestore_test.go`

- [ ] **Step 1: 写默认、旧 JSON 兼容、业务配置拒绝和 MySQL 校验的失败测试**

```go
func TestServerDefaultConfigOwnsManageStore(t *testing.T) {
	server := NewServiceDefaultConfig("server", 18080)
	require.NotNil(t, server.ManageStore)
	require.Equal(t, ManageStoreDriverSQLite, server.ManageStore.Driver)

	business := NewServiceDefaultConfig("orders", 18081)
	require.Nil(t, business.ManageStore)
	business.ManageStore = DefaultManageStoreConfig()
	require.ErrorContains(t, business.Validate(), "server")
}

func TestManageStoreMySQLRequiresConnectionFields(t *testing.T) {
	cfg := DefaultManageStoreConfig()
	cfg.Driver = ManageStoreDriverMySQL
	require.Error(t, cfg.Validate())
}
```

- [ ] **Step 2: 运行 RED**

Run: `go test ./pkg/server/config -run 'Test(ServerDefaultConfigOwnsManageStore|ManageStoreMySQL|AdminView)' -count=1`

Expected: FAIL，`ManageStoreConfig`/`ServerConfig.ManageStore` 尚不存在。

- [ ] **Step 3: 实现配置与校验**

```go
const (
	ManageStoreDriverSQLite = "sqlite"
	ManageStoreDriverMySQL  = "mysql"
)

type ManageStoreConfig struct {
	Driver       string
	Database     string
	Host         string
	Port         int
	Username     string
	Password     string
	MaxIdleConns int
	MaxOpenConns int
}

func DefaultManageStoreConfig() *ManageStoreConfig {
	return &ManageStoreConfig{Driver: ManageStoreDriverSQLite, Database: "core_manage", Port: 3306, MaxIdleConns: 10, MaxOpenConns: 50}
}
```

`ServerConfig` 使用 `ManageStore *ManageStoreConfig` 并标记 `json:",omitempty"`；新建 `server` 配置显式写入 `core_manage`，旧配置缺少字段时 `ApplyDefaults` 补为历史 `models`，`Validate` 拒绝业务服务配置中的非空值。

- [ ] **Step 4: 增加 Password 脱敏/保留测试并运行 GREEN**

Run: `go test ./pkg/server/config/... -count=1`

Expected: PASS；`AdminView` 中 Password 为 `[REDACTED]`，`MergeProtectedFields` 保留原口令。

- [ ] **Step 5: 提交**

```bash
git add pkg/server/config/managestore.go pkg/server/config/managestore_test.go pkg/server/config/serverconfig.go pkg/server/config/adminview_test.go
git commit -m "feat(config): add shared manage store settings"
```

### Task 2: 创建控制面 runtime 和启动初始化

**Files:**
- Create: `pkg/server/manageauth/controlplane.go`
- Test: `pkg/server/manageauth/controlplane_test.go`
- Modify: `pkg/server/manageauth/store.go`

- [ ] **Step 1: 写 action 选择、七表初始化、默认主体 Provider 和内置角色幂等测试**

```go
func TestControlPlaneRuntimeInitializesAllModelsAndBuiltInRoles(t *testing.T) {
	action := newIsolatedSQLiteAction(t)
	runtime, err := NewControlPlaneRuntime(action)
	require.NoError(t, err)
	require.NotNil(t, runtime.Authorizer())
	for _, code := range []string{types.ManageRoleSystemAdmin, types.ManageRoleViewer} {
		role, findErr := runtime.Store().FindRole(context.Background(), code)
		require.NoError(t, findErr)
		require.NotNil(t, role)
	}
}
```

- [ ] **Step 2: 运行 RED**

Run: `go test ./pkg/server/manageauth -run 'TestControlPlaneRuntime' -count=1`

Expected: FAIL，控制面 runtime 尚不存在。

- [ ] **Step 3: 实现 runtime**

```go
type ControlPlaneRuntime struct {
	action     persistencetype.IDataAction
	store      *ModelStore
	authorizer *Authorizer
}

func NewControlPlaneRuntime(action persistencetype.IDataAction) (*ControlPlaneRuntime, error) {
	if action == nil { return nil, errors.New("manage control-plane action is required") }
	runtime := &ControlPlaneRuntime{action: action}
	runtime.store = NewModelStore(action)
	runtime.authorizer = NewAuthorizer(runtime.store)
	if err := runtime.EnsureStorage(); err != nil { return nil, err }
	return runtime, nil
}
```

`EnsureStorage` 逐一空读 `DirectoryModel`、`MenuModel`、`PermissionsModel`、`ManageRoleModel`、`ManageRolePermissionModel`，然后幂等补入两个内置角。唯一冲突后重查，未知错误返回。

- [ ] **Step 4: 根据 `ManageStoreConfig` 构建 SQLite/MySQL action**

```go
func NewControlPlaneAction(cfg config.ManageStoreConfig) (persistencetype.IDataAction, error) {
	switch cfg.Driver {
	case config.ManageStoreDriverSQLite:
		return entity.GetGlobalSqliteInstance(cfg.Database), nil
	case config.ManageStoreDriverMySQL:
		return oltp.NewMySQL(&oltp.Config{Host: cfg.Host, Port: cfg.Port, Username: cfg.Username, Password: cfg.Password, Database: cfg.Database, MaxIdleConns: cfg.MaxIdleConns, MaxOpenConns: cfg.MaxOpenConns}), nil
	default:
		return nil, errors.New("unsupported manage store driver")
	}
}
```

- [ ] **Step 5: 运行 GREEN 与 race**

Run: `go test ./pkg/server/manageauth/... -count=1`

Run: `go test -race ./pkg/server/manageauth/... -count=1`

Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add pkg/server/manageauth/controlplane.go pkg/server/manageauth/controlplane_test.go pkg/server/manageauth/store.go
git commit -m "feat(manage): initialize shared control-plane store"
```

### Task 3: 让 SystemManage 通过 models 无参数工厂共用控制面存储

**Files:**
- Modify: `pkg/server/server.go`
- Modify: `pkg/server/api/manage/dmpbase.go`
- Modify: `pkg/server/api/manage/directorymanage.go`
- Modify: `pkg/server/api/manage/menumanage.go`
- Modify: `pkg/server/api/manage/managerolemanage.go`
- Modify: `pkg/server/api/manage/managerolepermissionmanage.go`
- Modify: `pkg/server/api/manage/menu_persistence.go`
- Create: `pkg/server/internal/managestore/store.go`
- Create: `pkg/server/smodels/manage_store.go`
- Test: `pkg/server/smodels/manage_store_test.go`

- [ ] **Step 1: 写证明系统模型无参数工厂使用统一存储且隔离事务状态的失败测试**

```go
func TestNewManageModelListClonesMutableAction(t *testing.T) {
	require.NoError(t, managestore.Configure(newIsolatedSQLiteAction(t)))
	first := smodels.NewManageModelList[smodels.ManageRoleModel]().GetAction()
	second := smodels.NewManageModelList[smodels.ManageRoleModel]().GetAction()
	require.NotSame(t, first, second)
}
```

- [ ] **Step 2: 运行 RED**

Run: `go test ./pkg/server/smodels ./pkg/server/api/manage -run 'Test(NewManageModelList|SystemManageModels)' -count=1`

Expected: FAIL，models 控制面工厂尚不存在。

- [ ] **Step 3: 增加 Core 内部 store 与 smodels 无参数工厂**

```go
func NewManageModelList[T persistencetype.IModel]() *entity.ModelList[T] {
	return entity.NewModelList[T](managestore.Clone())
}
```

`DmpBase.GetList()`、角色权限绑定 store 和菜单同步统一调用 `smodels.NewManageModelList[T]()`。禁止增加 `NewSystemManage(action)`、`New...ManageWithAction`、`DataActionProvider` 或 `DmpBase.action`；数据库选择不得越过 models 边界。

- [ ] **Step 4: 运行 GREEN**

Run: `go test ./pkg/server/api/manage/... ./pkg/server/... -count=1`

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add pkg/server/internal/managestore pkg/server/smodels/manage_store.go pkg/server/smodels/manage_store_test.go pkg/server/api/manage
git commit -m "refactor(manage): share control-plane persistence"
```

### Task 4: WebServer 启动屏障与 Manage Authorizer 接线

**Files:**
- Modify: `pkg/server/run/server.go`
- Modify: `pkg/server/run/manageauth.go`
- Modify: `pkg/server/router/servicecontext.go`
- Test: `pkg/server/run/manageauth_test.go`
- Test: `pkg/server/run/controlplane_store_test.go`

- [ ] **Step 1: 写启动前初始化、多服务共享和失败不监听的测试**

```go
func TestWebServerSharesSystemManageAuthorizerWithRoleProvider(t *testing.T) {
	web := newConfiguredWebServer(t, isolatedManageStore(t))
	web.AddIService(&manageRoleService{})
	system := web.serviceContexts["server"]
	business := web.serviceContexts["roles"]
	require.Same(t, system.ManageAuthorizer, business.ManageAuthorizer)
}
```

- [ ] **Step 2: 运行 RED**

Run: `go test ./pkg/server/run -run 'TestWebServer.*Manage|TestManageAuth' -count=1`

Expected: FAIL，当前每个 Provider 使用独立 `NewModelStore(nil)`。

- [ ] **Step 3: 由 WebServer 创建并持有 runtime**

`NewWebServer` 添加内置 `SystemManage` 后立即从其 `server.json` 配置构造 action/runtime；任何错误在 `Start` 前 panic/fail closed。`AddServiceContext` 默认绑定 WebServer 的 PrincipalProvider 与唯一 Authorizer；服务已显式实现 `IManageRoleProvider` 时保留该外部 IAM 映射。

- [ ] **Step 4: 删除 ServiceContext 中写死的 SQLite Authorizer**

```go
if sc.ManageRoleProvider == nil {
	sc.ManageRoleProvider = runtime.PrincipalProvider()
}
sc.ManageAuthorizer = runtime.Authorizer()
```

Authorizer 必须由 WebServer 控制面 runtime 绑定；没有 runtime 时 Manage RBAC 请求继续安全 500，不回退 SQLite。

- [ ] **Step 5: 运行 GREEN 与 race**

Run: `go test ./pkg/server/run/... ./pkg/server/router/... ./pkg/server/trans/rest/... -count=1`

Run: `go test -race ./pkg/server/run/... ./pkg/server/router/... ./pkg/server/trans/rest/... -count=1`

Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add pkg/server/run pkg/server/router/servicecontext.go pkg/server/trans/rest
git commit -m "feat(server): bind shared manage authorization store"
```

### Task 5: 内建管理员主体并简化 09 示例

**Files:**
- Create: `pkg/server/smodels/manageprincipalmodel.go`
- Create: `pkg/server/smodels/manageprincipalrolemodel.go`
- Create: `pkg/server/manageauth/principal_provider.go`
- Create: `pkg/server/api/manage/manageprincipalmanage.go`
- Create: `pkg/server/api/manage/manageprincipalrolemanage.go`
- Modify: `examples/09-admin-manage-rbac/main/main.go`
- Modify: `examples/09-admin-manage-rbac/http_test.go`
- Modify: `examples/09-admin-manage-rbac/README.md`
- Delete: `examples/09-admin-manage-rbac/models/`、消费方管理员 Manage、Provider 与 Service

- [ ] **Step 1: 写七表、首用户并发仲裁、refresh 不建档和稳定 Code 选择器的失败测试**

```go
func TestPrincipalProviderAssignsFirstAdminAndLaterViewer(t *testing.T) {
	runtime := newIsolatedControlPlane(t)
	// 两个并发 callback 只能得到一个 system_admin 和一个 viewer。
}
```

- [ ] **Step 2: 运行 RED**

Run: `go test ./pkg/server/manageauth ./pkg/server/api/manage ./pkg/server -run 'Test(PrincipalProvider|ManagePrincipal|SystemManage)' -count=1`

Expected: FAIL，Core 尚无默认 PrincipalProvider 与主体页面。

- [ ] **Step 3: 在 Core 控制面实现主体、关系、事务建档和页面**

默认 Provider 只接受可信 Casdoor Manage callback/refresh；callback 原子创建主体和初始角色，refresh 只读取已有主体。第一个 callback 用 nullable unique bootstrap slot 绑定 `core.system_admin`，冲突后以 `core.viewer` 重试。SystemManage 增加管理员和主体角色绑定页面，关系只使用稳定 Code。

- [ ] **Step 4: 更新 README JSON 示例与启动说明**

说明 `ManageStore` 只出现在 `server.json`，同进程多服务共享；业务侧不再实现管理员模型、Provider 或控制面存储接线。09 main 只需 `NewWebServer().Start()`。

- [ ] **Step 5: 运行 GREEN 与 race**

Run: `go test ./examples/09-admin-manage-rbac/... -count=1`

Run: `go test -race ./examples/09-admin-manage-rbac/... -count=1`

Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add examples/09-admin-manage-rbac
git commit -m "docs(example): use shared manage control-plane store"
```

### Task 6: 同步公共契约并执行全量验证

**Files:**
- Modify: `docs/ai/core-skill/auth-casdoor-and-admin.md`
- Modify: `docs/ai/core-skill/models.md`
- Modify: `docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`
- Modify: `docs/codex/API_COMPATIBILITY_SURFACE.md`
- Modify: `docs/codex/CONSUMER_COMPATIBILITY_MATRIX.md`
- Modify: release notes file selected by `docs/RELEASE_POLICY.md`

- [ ] **Step 1: 更新权威 skill 和配置/兼容契约**

文档必须明确：只有 `server.json` 持有 `ManageStore`；SQLite 是兼容默认；MySQL 失败不降级；启动屏障建表；旧 SQLite 数据不自动搬迁。

- [ ] **Step 2: 格式化与静态检查**

Run: `gofmt -w <all changed go files>`

Run: `git diff --check`

Expected: PASS。

- [ ] **Step 3: 定向与 race 测试**

Run: `go test ./pkg/server/config/... ./pkg/server/manageauth/... ./pkg/server/api/manage/... ./pkg/server/router/... ./pkg/server/run/... ./pkg/server/trans/rest/... ./examples/09-admin-manage-rbac/... -count=1`

Run: `go test -race ./pkg/server/manageauth/... ./pkg/server/api/manage/... ./pkg/server/run/... ./pkg/server/trans/rest/... ./examples/09-admin-manage-rbac/... -count=1`

Expected: PASS。

- [ ] **Step 4: 真实 MySQL 集成门禁**

Run: `CORE_TEST_MYSQL=1 go test -tags=integration ./pkg/server/manageauth/... ./examples/09-admin-manage-rbac/... -count=1`

Expected: 有可用专用 MySQL 时 PASS；环境未提供时必须报 `NOT RUN`，不写 PASS。

- [ ] **Step 5: 发布契约与日志门禁**

Run: `./scripts/check-logging.sh`

Run: `./scripts/test.sh release-contract`

Expected: PASS。

- [ ] **Step 6: 最终提交**

```bash
git add docs/ai/core-skill docs/codex <release-notes-file>
git commit -m "docs(manage): document shared control-plane database"
```

不 push，不部署，不发布，不修改 `web/admin` 子模块。
