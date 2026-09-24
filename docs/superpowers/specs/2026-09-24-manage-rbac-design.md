# Manage RBAC 设计

## 目标

为 Core Manage 域增加角色授权能力，在不把权限明细写入 Token、不修改 `web/admin`、不隐藏菜单和按钮的前提下，实现：

- Core 统一管理角色和角色权限明细；
- 消费方管理自己的管理员用户以及用户与角色的绑定；
- Manage Token 只携带稳定 RoleCode；
- 所有 Manage Router 在 Parse/Validation/Do 之前按 `service + path + command` 强制鉴权；
- 首个通过 Casdoor Manage 域注册的管理员成为系统管理员，后续用户成为只读管理员；
- Manage TestToken 始终具有系统管理员角色，便于开发和测试。

## 非目标

本次不实现：

- 菜单过滤或按钮隐藏；
- `web/admin` 代码、子模块指针或内嵌前端产物修改；
- 把权限明细或权限哈希写入 Token；
- Casdoor Role、Permission 或角色继承与 Core 角色的自动同步；
- 角色继承、通配符权限、权限 revision 或主动撤销现有 Token；
- Bitzoom 用户模型、管理员页面或本地兼容代码。

## 权威边界

```text
Casdoor
└── 身份认证、账号状态、登录、刷新、撤销

消费方 IManageRoleProvider
└── 管理员用户、用户与 RoleCode 的绑定、首次用户初始化

Core
├── 角色目录
├── 角色权限明细
├── Manage Token 的 RoleCode Claim
└── Router 前服务端鉴权
```

Casdoor 只作为身份权威。Core 不读取 Casdoor Role/Permission，也不继承 Casdoor 的子角色关系。

## 数据模型

### ManageRoleModel

Core 新增系统角色模型：

```go
type ManageRoleModel struct {
	*entity.Model
	Code        string `json:"code" gorm:"size:128;uniqueIndex"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	IsSystem    bool   `json:"isSystem"`
	IsDefault   bool   `json:"isDefault"`
	Policy      string `json:"policy"`
}
```

`Code` 是公开稳定键，创建后不可修改。RoleCode 参与 Token、用户角色绑定和权限明细，不使用数据库 ID 建立跨边界关系。

`Policy` 第一版只接受：

- `grant_all`：允许全部 Manage command；
- `read_only`：只允许 command 精确等于 `view` 或 `search`；
- `explicit`：按权限明细精确匹配。

### ManageRolePermissionModel

```go
type ManageRolePermissionModel struct {
	*entity.Model
	RoleCode string `json:"roleCode" gorm:"size:128;index:idx_manage_role_permission,unique"`
	Service  string `json:"service" gorm:"size:128;index:idx_manage_role_permission,unique"`
	Path     string `json:"path" gorm:"size:512;index:idx_manage_role_permission,unique"`
	Command  string `json:"command" gorm:"size:128;index:idx_manage_role_permission,unique"`
}
```

唯一性为 `RoleCode + Service + Path + Command`。不保存 `MenuModelID`、`PermissionsModelID` 或 `ManageRoleModel.ID`，避免菜单同步、重建和环境差异导致关系漂移。

模型继续使用 Core 首次数据访问自动建表，不增加 migration、初始化 SQL 或业务 `AutoMigrate`。

## 内置角色

Core 保证以下角色幂等存在：

| Code | Policy | IsSystem | IsDefault | 行为 |
| --- | --- | --- | --- | --- |
| `core.system_admin` | `grant_all` | true | false | 允许所有 Manage Router |
| `core.viewer` | `read_only` | true | true | 只允许精确的 `view`、`search` |

内置角色：

- 不允许删除、禁用、修改 Code、Policy、IsSystem 或 IsDefault；
- 不创建权限明细；
- 菜单变化后动态获得对应能力，无需重新同步权限；
- 在角色管理界面正常显示，但字段只读。

自定义角色必须使用 `explicit`，权限来自 `ManageRolePermissionModel`。第一版不支持把自定义角色设为默认角色；唯一默认角色固定为内置 `core.viewer`。消费方需要额外权限时，由管理员显式建立用户与自定义 RoleCode 的关系。

## 公共接口

Core 在 `pkg/server/types` 增加：

```go
type ManageRoleRef struct {
	Code string `json:"code"`
}

type ManagePrincipalRequest struct {
	Identity     AuthIdentity
	Source       AuthSource
	DefaultRoles []ManageRoleRef
}

type ManagePrincipal struct {
	Roles []ManageRoleRef
}

type IManageRoleProvider interface {
	ResolveManagePrincipal(context.Context, ManagePrincipalRequest) (ManagePrincipal, error)
}
```

Provider 的职责：

- 使用 `AuthIdentity.UID` 及 Provider 信息定位消费方管理员；
- 首次出现时建立管理员用户；
- 首次出现时按消费方明确策略建立用户角色关系；`DefaultRoles` 只是 Core 提供的建议默认值，09 示例固定首个真实用户为 `core.system_admin`、后续用户为 `core.viewer`；
- 返回当前有效 RoleCode；
- 使用消费方自己的事务、唯一约束或锁保证首次初始化幂等。

Provider 不返回权限明细，不决定 Router 是否允许。

服务实现 `IManageRoleProvider` 即显式启用 Manage RBAC。未实现时保持现有“Manage Token 认证成功即可访问”的兼容行为。Provider 已启用后，角色解析失败、Claim 缺失、Claim 非法、角色存储不可用或权限查询失败都必须 fail closed。

## Token 契约

Manage Access Token 增加保留 Claim：

```json
{
  "manage_roles": "[\"core.viewer\",\"ops.approver\"]"
}
```

当前 `IClaimsMutator` 只接受 string，因此值使用规范 JSON 数组字符串。Core 在签发前完成 trim、格式校验、去重、排序和数量/长度限制。业务 `IAuthHookProvider` 不得覆盖该保留键。

Token 不包含 path、command、权限列表、权限哈希、菜单 ID 或数据库 ID。

### TestToken

`AuthTypeManage + AuthSourceTestToken` 固定写入 `core.system_admin`，不调用消费方 Provider 创建管理员用户，也不占用 Casdoor 首用户 bootstrap。

### Casdoor Callback 与 Refresh

- Callback 完成 Casdoor Owner、Subject、用户状态和撤销权威校验后，再调用 Provider；
- Provider 只可根据可信 `AuthIdentity` 建立用户；
- Refresh 重新解析当前用户角色，因此用户角色变更最迟在刷新或重新登录后生效；
- Refresh 不得重新执行“首个 Casdoor 用户”判定，消费方 Provider 必须只在首次建立管理员记录时初始化角色。

## 首个 Casdoor 管理员

09 示例的 Provider 实现以下规则：

```text
第一个通过完整 Casdoor Manage Callback 建立的 AdminUser
→ 绑定 core.system_admin

后续首次建立的 AdminUser
→ 固定绑定 core.viewer
```

首用户判断和用户/角色写入必须在同一消费方事务或临界区完成。TestToken、Refresh、Auth 用户域、ServerManage 域均不能触发该规则。

首个系统管理员被禁用或删除后，不得自动把下一名用户提升为系统管理员。恢复必须通过已有系统管理员、受控 TestToken 环境或消费方运维流程完成。

该 bootstrap 只适用于关闭公开注册、首个账户由部署人员预建或邀请的 Casdoor Manage 应用。

## Router command

权限判断不能依赖对请求 URL 做包含判断。Core 为 Manage `RouterInfo` 提供稳定 command：标准操作为 `view/search/add/edit/remove/submit/release`，自定义命令使用注册 Router 的稳定类型名小写形式。

菜单发现快照显式携带 `Command`，`PermissionsModel.Name` 继续保存 command，`PermissionsModel.Url` 继续保存完整 path。旧快照缺少 `Command` 时允许从已验证的 RouterInfo/路径末段兼容恢复，但新节点必须发送 Command。

## 鉴权顺序

Manage REST 请求顺序：

```text
Access Token 验签与认证域隔离
→ Casdoor 撤销权威校验
→ Core Manage RBAC
→ 业务 IAuthRequestHookProvider
→ Router Parse / Validation / Do
```

Core 规则：

```go
switch role.Policy {
case "grant_all":
	allow = true
case "read_only":
	allow = command == "view" || command == "search"
case "explicit":
	allow = exactMatch(roleCode, service, path, command)
}
```

多角色取权限并集。任意角色允许即允许；角色不存在、禁用或全部拒绝时返回现有：

```text
ErrorKindForbidden
HTTP 403
code 40300
message permission denied
```

授权发生在 Router 执行前，不能放入 `ManageService.ValidationBefore`，也不能依赖标准 CRUD Hook，因此标准和自定义 command 都受同一边界保护。

角色权限明细在请求时读取，修改角色权限后下一次请求立即生效；用户与角色绑定发生变化后，用户需刷新或重新登录取得新的 RoleCode Claim。

## 管理界面

Core `SystemManage` 新增：

- 角色管理：管理自定义角色，展示并保护内置角色；
- 角色权限管理：按 RoleCode 管理显式 `service + path + command` 权限。

角色权限管理使用现有菜单扫描产生的 Permission 目录作为选择来源，保存时复制稳定的 Service、Path 和 Command，不保存 Permission ID。

本次保持现有菜单和 ViewModel commands 完全可见。用户可以点击未授权按钮，服务端返回 403，以便开发和测试阶段直接验证授权是否生效。权限隐藏另行设计。

## 09 示例

新增 `examples/09-admin-manage-rbac`：

- `AdminUserModel`：消费方管理员档案；
- `AdminUserRoleModel`：使用 `UserCode + RoleCode` 建立稳定绑定；
- 管理员用户 Manage；
- 用户角色绑定 Manage；
- `IManageRoleProvider` 实现；
- TestToken 系统管理员流程；
- Fake Casdoor 首用户和后续用户流程；
- 自定义角色授权与 403 验证。

09 只使用现有通用 Manage UI，不修改前端。

## 兼容性与发布

- 未实现 Provider 的服务保持旧行为；
- 新接口、新模型、新 Claim 和新系统 Manage 路由均为加性能力；
- `UserPeermissionsModel` 暂不删除，只登记 Deprecated；
- Manage/OpenAPI 默认响应字段不变；
- 本能力应作为 MINOR 发布，建议下一个版本为 `v1.3.0`；
- 发布前更新 API compatibility、废弃登记、消费方矩阵、skill 和 changelog；
- 不自动 tag、push、发布或部署。

## 验证

必须包含：

- 模型、内置角色、不可变约束和权限精确匹配单元测试；
- Token 签发、Claim 保留键、TestToken、Callback、Refresh 测试；
- Router 前真实 Type/Path/Command 鉴权和 403 响应测试；
- 首个 Casdoor 用户并发初始化测试；
- 09 真实进程 HTTP 与 Fake Casdoor 集成测试；
- 受影响包 `-race`；
- `quick`、`security`、`api-compat`、`public-api`、`config-contract`、`release-contract`；
- `gofmt`、`git diff --check` 和日志检查。
