# Manage 角色权限示例

本示例演示 Core 内建的 Manage 管理员、角色和精确命令授权。业务服务不需要再定义管理员模型、用户角色关系、repository、store 或 `IManageRoleProvider`；标准 `run.NewWebServer()` 会从可信 Manage 身份建立主体，并把角色编码写入 Manage Access Token。

入口同时注册 [08 管理界面示例](../08-admin-manage-ui) 的 `CatalogService`，提供「分类管理」「资料条目」两个真实业务页面及其 Manage API。08 关注控件和页面交互，09 在同一套业务操作上演示 RBAC；业务模型、持久化和 View schema 直接复用，不另建管理员表或演示用探针 API。

## 可授权的业务 Manage API

| 页面 | API 路径 | command |
| --- | --- | --- |
| 分类管理 | `/api/manage/catalog/categorymanage/{command}` | `view`、`search`、`add`、`edit`、`remove`、`submit`、`release` |
| 资料条目 | `/api/manage/catalog/catalogitemmanage/{command}` | 上述七项及 `importdata`、`exportdata`、`cloneitem` |

分类和条目 CRUD 使用真实本地 SQLite 存储。`cloneitem` 会复制条目；Import/Export 的后端仅返回演示提示，Excel 处理由前端完成，详见 08 README。空表首次 Search 会写入默认演示数据，先打开分类再打开条目。

本示例保持 08 的稳定服务名 `catalog`。角色权限必须匹配实际注册的 `Service + Path + Command`，不能把目录标题或示例文件夹名当作 service。

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

控制面由框架自动初始化，业务服务需要显式注册：

```go
if err := models.EnsureStorage(); err != nil { // models 为 08 的 models 包
    panic(err)
}
server := run.NewWebServer()
server.AddIService(&adminui.CatalogService{}) // adminui 为 examples/08-admin-manage-ui
server.Start()
```

不要增加 `ConfigureManageModels`、消费方管理员表、`NewAdminService(repository...)`、`NewManageRoleProvider(store...)` 或 `New...ManageWithAction`。控制面 action 只在 Core 内部流转。

## 启动与验证

```bash
cd examples/09-admin-manage-rbac/main
go build -o admin-manage-rbac .
./admin-manage-rbac -view 8888
```

首次运行会在可执行文件所在目录生成 `etc/server.json` 和 `etc/catalog.json`。打开 `http://127.0.0.1:8888`；默认业务服务端口为 `8081`，内部系统管理端口为 `8080`。09 复用的业务库名是 `adminui`，与 Core 控制面库分开；集成测试在独立临时运行目录启动，避免改动手工演示数据。

08 和 09 的入口都会先调用 models 的 `EnsureStorage()`，通过框架数据访问初始化分类、条目、明细和规格表，失败时终止启动。

本示例不需要 Redis 或 MQ。代码中的 `MQ.Mode` 默认是 `off`；只有需要跨节点消息的业务服务才在自己的 `etc/catalog.json` 等服务配置中明确启用并填写 Broker 地址。若已有 `etc/server.json` 或 `etc/catalog.json` 是旧版本生成的、其中保存了 `MQ.Mode=auto`，该显式值会保留，需改为 `off` 后重启，才能停止自动连接 Redis。

开发期未启用 Manage Casdoor 时，可从本地视图取得 TestToken。验证真实注册顺序时，需要在创建 `ServiceContext` 前配置 `ManageAuth.CasDoor.Enable=true`、有效的 Casdoor YAML、独立的 Access/Refresh/Webhook Secret 和撤销配置，然后重启。

Manage Casdoor 使用：

- `/api/casdoor?type=manage`
- `/api/casdoor/callback?type=manage&code=...&state=...`
- `/api/refresh`

界面验证顺序：

「内部系统管理」应包含「角色管理」「角色权限管理」「管理员」「管理员角色绑定」。这些是 Core 内置菜单，由 `getmenu` 直接提供，业务「更新菜单」不会生成 server 目录的入口；升级旧二进制后需刷新页面重新获取菜单。

1. 使用 TestToken 进入后台，在「内部系统管理」→「菜单管理」点击「更新菜单」。确认侧栏出现「资料目录」下的「分类管理」「资料条目」，依次打开并加载默认数据；新增一个临时分类，确认数据真实保存。TestToken 不创建管理员主体。
2. 使用第一个真实 Casdoor Manage 用户登录，确认“管理员”和“管理员角色绑定”中出现 `core.system_admin`。
3. 使用第二个真实用户登录，确认默认角色为 `core.viewer`。
4. 用第二个用户打开分类和条目页，确认 `view/search` 成功，分类「新增」和条目「复制」均返回 HTTP 403、公开码 `40300`、消息 `permission denied`。
5. 切回首位管理员，新建 `demo.category_editor` 自定义角色，选中该行点击「配置权限」。弹窗读取 `getmenu`，按服务→菜单→命令显示勾选树；在「资料目录」→「分类管理」勾选 `view/search/add` 并保存，无需手填服务和路径。也可从「角色权限管理」的「配置角色权限」打开并选择角色。再次打开确认已有权限已勾选。在管理员角色绑定页给第二个用户绑定该 RoleCode。
6. 第二个用户 refresh 或重新登录后，分类「新增」成功，「编辑」「删除」仍返回 403；条目「新增」「复制」也仍返回 403。原有 `core.viewer` 关系可以保留，角色权限按并集判定。
7. 管理员重新打开权限树，取消分类 `add` 的勾选并保存。第二个用户使用原 Token 重试新增，确认下一请求立即返回 403。全部取消并保存会清空该自定义角色的权限；关闭或取消弹窗不会保存。

树中「分类管理」→「新增」对应的精确权限（供 API 验证使用）：

| 字段 | 值 |
| --- | --- |
| RoleCode | `demo.category_editor` |
| Service | `catalog` |
| Path | `/api/manage/catalog/categorymanage/add` |
| Command | `add` |

若要允许条目复制，需要另行添加 `Service=catalog`、`Path=/api/manage/catalog/catalogitemmanage/cloneitem`、`Command=cloneitem`。允许分类新增不会连带允许其他页面或其他 command。

当前版本故意不隐藏菜单和按钮，以便直接验证服务端授权。前端可见性不构成授权依据；后端始终再次鉴权。第一版不修改 `web/admin`，也不改变 `/api/servermanage/getmenu`。

## 测试

```bash
go test ./examples/09-admin-manage-rbac/... -count=1
go test ./examples/integration/09-admin-manage-rbac -count=1
```

示例包内 HTTP 测试使用测试探针，经过 JWT、角色 claim、Core Authorizer、RouterInfo 执行和最终 Response，覆盖 viewer 只读、system_admin、自定义精确权限、403、权限变化即时生效和存储异常安全 500。进程集成测试另行构建并启动本示例入口和开发视图，通过视图端口执行「更新菜单」并检查 `getmenu` 返回分类/条目菜单，再验证页面 schema、真实分类新增和查询、viewer 只读及新增 403；同时检查默认 MQ 关闭且没有 Redis 连接失败日志。测试角色 Token 仅在隔离测试中按临时配置签发，不替代真实 Casdoor 登录验收。
