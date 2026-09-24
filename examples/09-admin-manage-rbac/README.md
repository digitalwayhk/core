# Manage 角色权限示例

本示例演示 Core Manage 域的角色授权接入。Core 保存系统角色和 `path + command` 权限；消费方保存管理员用户及用户与 `RoleCode` 的关系，并通过 `IManageRoleProvider` 在登录或刷新时把角色编码写入 Manage Access Token。

本示例不把权限明细写入 token，也不把 Casdoor 角色同步为 Core 角色。Casdoor 只提供可信身份；Core 角色关系以消费方数据库为准。

## 数据模型

消费方只维护两张表：

- `AdminUserModel`：以认证身份的稳定 `UID` 作为 `Code`，同时保存 Casdoor provider 和 subject，防止身份错配。
- `AdminUserRoleModel`：只保存 `UserCode + RoleCode`，不保存角色数据库 ID。

管理员角色绑定页的用户和角色都使用现有 Manage 外键选择交互。角色候选来自 Core 的 `ManageRoleModel`，选择值明确使用稳定 `code`；展示关联字段带 `gorm:"-"`，不会建立跨库外键或额外持久化 RoleID。

Core 自身维护：

- `ManageRoleModel`：角色目录。
- `ManageRolePermissionModel`：自定义角色的结构化 `RoleCode + Path + Command` 权限明细。

## 内置角色

两个内置角色不能删除，也不展开保存权限明细：

- `core.system_admin`：动态允许全部 Manage command。
- `core.viewer`：动态允许所有 Manage 页面的精确 `view` 和 `search` command。

真实 Casdoor Manage callback 第一次创建的管理员分配 `core.system_admin`，后续新管理员分配 `core.viewer`。这个规则只在 callback 创建用户时执行；refresh 不会补建未知管理员。

TestToken 由 Core 直接赋予 `core.system_admin`，不会调用本示例 Provider，也不会占用“首个真实管理员”名额。

## Token 与变更生效

Manage Access Token 只包含规范化后的 RoleCode 列表：

- 修改用户与角色关系后，已签发 token 不会变化；用户需要刷新或重新登录取得新角色快照。
- 修改某个自定义角色的权限明细后，不需要重新登录；Core authorizer 在下一次请求读取当前权限。
- 权限判断始终在服务端执行，匹配目标 Manage 路由的稳定 `path + command`。

当前版本故意不隐藏菜单和按钮。用户仍能看到并点击无权操作，服务端会返回 HTTP 403；这便于直接验证权限配置是否生效。前端可见性不构成授权依据。

## Provider 接入

`AdminService` 实现 `IManageRoleProvider`，Core 会在 Manage Casdoor callback 和 refresh 签发 token 前调用：

```go
func (s *AdminService) ResolveManagePrincipal(
    ctx context.Context,
    request types.ManagePrincipalRequest,
) (types.ManagePrincipal, error)
```

Provider 只接受：

- `AuthType=manage`；
- `Provider=casdoor`；
- 来源为 callback 或 refresh；
- 完整且与本地记录一致的 `UID + ProviderSubject`。

任何查询、身份匹配或角色结果异常都会 fail closed，不会回退为默认放行。

## 启动

```bash
cd examples/09-admin-manage-rbac/main
go build -o admin-manage-rbac .
./admin-manage-rbac -view 8888
```

首次运行按 Core 规则生成配置。开发期未启用 Manage Casdoor 时，可从本地视图取得 TestToken；验证真实注册顺序时，需要在创建 `ServiceContext` 前配置 `ManageAuth.CasDoor.Enable=true`、有效的 Casdoor YAML、独立的 Access/Refresh/Webhook Secret 和撤销配置，然后重启。

Manage Casdoor 使用：

- `/api/casdoor?type=manage`
- `/api/casdoor/callback?type=manage&code=...&state=...`
- `/api/refresh`

不要用 Auth 用户域 token 访问 Manage API，也不要依赖 Casdoor 中的角色继承。

## 界面验证顺序

1. 使用 TestToken 登录，确认可访问全部 Manage command；这不会创建管理员记录。
2. 使用第一个真实 Casdoor Manage 用户登录，确认管理员记录和 `core.system_admin` 关系被创建。
3. 使用第二个真实用户登录，确认默认关系为 `core.viewer`。
4. 打开“管理员角色绑定”，从系统角色列表按 RoleCode 增删关系；刷新或重新登录后验证新角色。
5. 打开 Core 的角色与权限管理页，为自定义角色绑定精确 `path + command`。
6. 使用该角色点击允许和不允许的按钮；允许项成功，不允许项返回 403。
7. 修改同一角色的权限明细后直接重试请求，验证下一请求立即使用新权限。

`core.viewer` 可以查看和搜索全部 Manage 页面，但 Add、Edit、Remove 以及自定义 command 都必须返回 403。第一版不修改 `web/admin`，也不改变 `/api/servermanage/getmenu`。

## 测试

```bash
CGO_ENABLED=0 go test ./examples/09-admin-manage-rbac/... -count=1
```

真实 HTTP 授权覆盖另见本目录的 HTTP 测试。
