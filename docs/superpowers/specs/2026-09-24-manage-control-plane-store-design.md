# Manage 控制面共享存储设计

## 目标

Core 的目录、菜单、按钮权限、角色和角色权限属于同一个 Manage 控制面。默认 SQLite 仍保持零配置兼容；正式环境可在 `etc/server.json` 中选择 MySQL，使同一部署的多进程/多副本共享一份管理授权数据。

本次不把 `IDataAction`、repository 或 store 构造参数暴露给消费方 `AdminService`、Provider 或 Manage API。消费方只管理 JSON 配置，Core 负责连接、初始化、CRUD 与运行时鉴权接线。

## 配置契约

`config.ServerConfig` 增加可选指针 `ManageStore *ManageStoreConfig`。该字段只属于进程级 `SystemManage`：`config.NewServiceDefaultConfig("server", port)` 生成的 `etc/server.json` 包含，其他业务服务 JSON 保持省略：

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

约束：

- `etc/server.json` 是整个 Core Manage 控制面存储的唯一权威配置；业务服务 JSON 不各自选择角色库。
- `NewServiceDefaultConfig` 只在 `servicename == "server"` 时创建默认 `ManageStore`；`shop.json`、`orders.json`、`users.json` 等业务配置不输出该字段。
- `NewServiceDefaultConfig("server", ...)` 为新配置写入 SQLite `core_manage`；`ApplyDefaults` 对缺少字段的旧 `server.json` 补为历史 SQLite `models`，避免升级后目录和菜单看似丢失。业务服务配置若显式出现 `ManageStore`，`Validate` 必须拒绝，防止多份权威配置漂移。
- `Driver` 第一版只接受 `sqlite|mysql`；其他值在启动时 fail closed。
- 缺失 `ManageStore` 的旧 `server.json` 经 `ApplyDefaults()` 明确解析为 SQLite `models`，不破坏旧部署。
- MySQL 模式必须提供 `Host`、正数 `Port`、`Username` 和 `Database`；不支持无声回退 SQLite。
- 不提供完整 DSN 字段，避免带口令连接串进入响应、日志或错误。
- `Password` 复用 `AdminView`/`MergeProtectedFields` 现有递归脱敏与保护逻辑；配置文件继续以 `0600` 保存。
- `ManageStore` 是构造期配置；通过 Manage 配置页修改后需重启，不在运行中热换连接。
- 同一进程首次成功初始化后锁定控制面数据库；再次构造相同数据库可复用，绑定不同数据库必须 fail closed，且失败初始化不能覆盖已经生效的绑定。
- 初始化完成前必须验证 `ManagePrincipalModel.BootstrapSlot` 列和唯一索引真实存在；驱动补列/建索引失败、历史重复数据或权限不足都必须阻止监听，不能降级成仅靠进程锁的首管理员仲裁。

## 数据范围

以下 Core 系统模型必须使用同一个控制面 `IDataAction`：

- `smodels.DirectoryModel`
- `smodels.MenuModel`
- `smodels.PermissionsModel`
- `smodels.ManageRoleModel`
- `smodels.ManageRolePermissionModel`
- `smodels.ManagePrincipalModel`
- `smodels.ManagePrincipalRoleModel`

内置 `core.system_admin` 和 `core.viewer` 的授权策略仍由代码动态计算。为了在角色管理页展示，启动初始化会幂等保证两条内置角色目录记录存在；不为内置角色创建权限明细行。管理员主体与角色关系也由 Core 管理，消费方不重复建表。

## 运行时结构

Core 内部增加一个由 `WebServer` 持有的 Manage 控制面运行时，保存根据 `server.json` 已验证配置创建的 `IDataAction`、`manageauth.ModelStore` 和 `manageauth.Authorizer`。同一 `WebServer` 中注册的所有业务服务共享该运行时，不从各自的服务 JSON 重复构造。该运行时只有一个存储选择，不允许目录/菜单留在 SQLite 而角色/权限分离到 MySQL。

```text
etc/server.json
  → ManageStoreConfig.Validate
  → SQLite 或 MySQL IDataAction
  → ControlPlaneRuntime
       ├─ SystemManage 目录/菜单/按钮/角色/权限/管理员/角色绑定 CRUD
       ├─ 默认 PrincipalProvider 在 callback/refresh 解析 RoleCode
       ├─ 菜单同步与角色权限绑定事务
       └─ Manage Authorizer 每请求精确权限查询
```

`manageauth.Store` 仍由 Core 的 `ModelStore` 实现，消费方不需实现。变化点是 `ModelStore` 接收控制面运行时的 action，不再在 `ServiceContext` 中写死 `NewModelStore(nil)`。

`SystemManage` 的专用 Manage 实现使用内部无参工厂获取同一运行时的 `ModelList`，不改变通用 `service/manage.ManageService.GetList()` 的公开默认行为，也不向业务 Manage 暴露 action。

WebServer 为每个 ServiceContext 绑定同一个默认 PrincipalProvider 与 Authorizer；服务显式实现 `IManageRoleProvider` 时只覆盖主体到 RoleCode 的映射，用于外部 IAM，不替换 Authorizer。HTMLServer 选定 `ManageAuthAuthorityService` 后仍按实际业务 RouterInfo 的 `service + path + command` 鉴权。

`ManageAuthAuthorityService` 只决定 Manage 身份签发和角色 Provider，不成为第二个存储配置源。独立部署的多个进程各自读取本进程的 `server.json`，但它们必须指向同一个 MySQL `Database`；这是部署配置同步要求，不是每个业务服务各有一份权限库。

## 启动与表初始化

初始化是服务启动屏障，不是请求期自愈：

```text
读取/生成 etc/server.json
→ 验证 ManageStore
→ 创建数据库 action
→ 逐一访问七类控制面模型，触发框架建表/补列
→ 幂等保证两个内置角色目录
→ 绑定 SystemManage CRUD 和 Authorizer
→ 才开始 HTTP/gRPC 监听
```

任一步失败都终止启动，不回退 SQLite，不带病监听。原始 DSN、口令和 SQL 错误不进入 HTTP 响应。

Core 自动结构处理只允许建库、建表和补列。删列、改类型、破坏性索引/约束变更不自动执行，必须走发布迁移流程。

多副本可同时启动；表初始化和内置角色写入必须幂等，唯一冲突按“其他副本已完成”处理，未知持久化错误仍中止启动。

## 错误与生命周期

- 配置非法：启动失败，错误只指明字段，不打印 Password。
- 数据库不可达/初始化失败：启动失败，不降级 SQLite。
- 鉴权查询失败：继续返回安全 Internal/500，不误报 403，不泄露数据库原文。
- 角色不存在、被禁用或没有命中权限：返回 Forbidden/403。
- 修改角色权限：因 Authorizer 不缓存明细，下一次请求生效。
- 修改用户 RoleCode：仍需 refresh 或重新登录后写入新 Token。

## 测试与兼容性

实现使用 RED → GREEN，至少覆盖：

1. 默认 `server.json` 生成 SQLite `ManageStore`，旧 JSON 缺字段仍按 SQLite 解析；业务服务 JSON 不生成该字段，显式写入时校验拒绝。
2. MySQL 配置的必填字段、连接池边界和未知 Driver fail closed。
3. `AdminView` 脱敏 Password，`MergeProtectedFields` 不用 `[REDACTED]` 覆盖真实口令。
4. 控制面七类模型使用同一 action；SystemManage CRUD、PrincipalProvider 与 Authorizer 查到同一份数据。
5. 启动初始化在监听前完成；初始化失败时不进入可服务状态。
6. 新库自动创建全部表并写入两个内置角目录，重复启动不重复写入。
7. 多副本并发初始化不产生重复内置角或将唯一冲突误报为启动失败。
8. 首个真实主体、后续 viewer、refresh 不建档、Manage RoleCode 签发、精确权限鉴权、菜单绑定与 09 真实 HTTP 测试在共享存储下通过。
9. 默认 SQLite 回归通过；有显式集成门禁时运行真实 MySQL 建表、CRUD 和鉴权测试。
10. `go test -race` 覆盖控制面运行时共享和并发初始化。

这是一个加性配置与可选持久化能力。默认 SQLite 行为保持不变；启用 MySQL 后，原 SQLite 数据不会自动搬迁，需在发布说明中明确先导出/导入或重建控制面数据。

## 文档与发布面

实现后同步：

- `docs/ai/core-skill/auth-casdoor-and-admin.md`
- `docs/ai/core-skill/models.md`
- `docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`
- `docs/codex/API_COMPATIBILITY_SURFACE.md`
- `docs/codex/CONSUMER_COMPATIBILITY_MATRIX.md`
- `docs/RELEASE_POLICY.md` 所要求的 release notes/兼容登记
- `examples/09-admin-manage-rbac/README.md` 及真实 HTTP 示例

不修改 `web/admin` 子模块，不部署，不发布。
