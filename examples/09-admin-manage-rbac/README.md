# Manage 角色权限示例

本示例演示 Core 内建的 Manage 管理员、角色和精确命令授权。业务服务不需要再定义管理员模型、用户角色关系、repository、store 或 `IManageRoleProvider`；标准 `run.NewWebServer()` 会从可信 Manage 身份建立主体，并把角色编码写入 Manage Access Token。

## Core 控制面模型

`server.json.ManageStore` 统一承载七类 Core 模型：

- `DirectoryModel`、`MenuModel`、`PermissionsModel`；
- `ManageRoleModel`、`ManageRolePermissionModel`；
- `ManagePrincipalModel`、`ManagePrincipalRoleModel`。

管理员主体以可信身份的稳定 `UID` 作为 `Code`，同时保存 Casdoor provider 和 subject，防止身份错配。主体角色关系只保存 `PrincipalCode + RoleCode`，不保存角色或用户数据库 ID。角色权限以 `RoleCode + Service + Path + Command` 表示；Token 只保存 RoleCode，不保存权限列表、权限哈希、菜单或按钮。

首位真实 Casdoor 管理员及其引导产生的 `core.system_admin` 关系受 Core 保护，不能停用、删除或解绑。管理员、角色、权限不按业务服务拆表；它们共同位于进程级 `ManageStore`，各服务只提供自己的 Manage 路由供统一 Authorizer 校验。

只有角色权威确实位于外部 IAM 时，消费方才实现 `IManageRoleProvider` 覆盖 Core 默认主体映射。自定义 Provider 只返回标准角色列表，不接收 Core store/action，也不能替代 Router 前的权限判定。

## 内置角色和首用户

- `core.system_admin`：动态允许全部 Manage command。
- `core.viewer`：动态只允许所有 Manage 页面的 `view`、`search`。

第一个真实 Casdoor Manage callback 由 Core 在同一事务中创建主体并绑定 `core.system_admin`；后续新主体绑定 `core.viewer`。跨进程竞争由 nullable unique `BootstrapSlot` 仲裁，进程锁不是最终保障。refresh 不创建未知主体。

TestToken 由 Core 直接赋予 `core.system_admin`，不会创建主体，也不会占用首个真实管理员名额。

角色关系改变后，用户需要 refresh 或重新登录取得新 Token。自定义角色权限明细由 Authorizer 在每次请求读取，修改后下一请求立即生效。

## 控制面存储配置

整个进程只在 `etc/server.json` 配置一次；业务服务 JSON 不得重复出现 `ManageStore`。默认 SQLite 配置为：

```json
{
  "ManageStore": {
    "Driver": "sqlite",
    "Database": "core_manage",
    "Host": "",
    "Port": 3306,
    "Username": "",
    "Password": "",
    "MaxIdleConns": 10,
    "MaxOpenConns": 50
  }
}
```

上面是新生成配置。若升级前的 `server.json` 没有 `ManageStore` 字段，Core 会继续使用历史 `models` SQLite 库；不要仅为采用新默认名而直接改成 `core_manage`，除非已经迁移目录、菜单和授权数据。

多进程或多副本环境应改用共享 MySQL，填写 `Host`、`Port`、`Username`、`Password` 和 `Database`。连接或七类表初始化失败会在监听前终止启动，不会降级回 SQLite；SQLite 与 MySQL 之间不自动迁移既有数据。

启动代码不需要额外接线：

```go
server := run.NewWebServer()
server.Start()
```

不要增加 `ConfigureManageModels`、消费方管理员表、`NewAdminService(repository...)`、`NewManageRoleProvider(store...)` 或 `New...ManageWithAction`。控制面 action 只在 Core 内部流转。

## 启动与验证

```bash
cd examples/09-admin-manage-rbac/main
go build -o admin-manage-rbac .
./admin-manage-rbac -view 8888
```

开发期未启用 Manage Casdoor 时，可从本地视图取得 TestToken。验证真实注册顺序时，需要在创建 `ServiceContext` 前配置 `ManageAuth.CasDoor.Enable=true`、有效的 Casdoor YAML、独立的 Access/Refresh/Webhook Secret 和撤销配置，然后重启。

Manage Casdoor 使用：

- `/api/casdoor?type=manage`
- `/api/casdoor/callback?type=manage&code=...&state=...`
- `/api/refresh`

界面验证顺序：

1. 使用 TestToken 登录，确认可访问全部 Manage command，且不创建管理员主体。
2. 使用第一个真实 Casdoor Manage 用户登录，确认“管理员”和“管理员角色绑定”中出现 `core.system_admin`。
3. 使用第二个真实用户登录，确认默认角色为 `core.viewer`。
4. 新建自定义角色，在角色权限页绑定菜单默认 `view/search` 或精确 command。
5. 在管理员角色绑定页按稳定 RoleCode 增删关系，refresh 或重新登录。
6. 点击允许和不允许的按钮：允许项成功，不允许项返回 HTTP 403、公开码 `40300`、消息 `permission denied`。
7. 修改自定义角色权限后直接重试，确认下一请求立即使用新权限。

当前版本故意不隐藏菜单和按钮，以便直接验证服务端授权。前端可见性不构成授权依据；后端始终再次鉴权。第一版不修改 `web/admin`，也不改变 `/api/servermanage/getmenu`。

## 测试

```bash
go test ./examples/09-admin-manage-rbac/... -count=1
```

HTTP 测试真实经过 JWT、角色 claim、Core Authorizer、RouterInfo 执行和最终 Response，覆盖 viewer 只读、system_admin、自定义精确权限、403、权限变化即时生效和存储异常安全 500。
