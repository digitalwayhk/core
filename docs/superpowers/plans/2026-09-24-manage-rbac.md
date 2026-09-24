# Manage RBAC Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 Core Manage 域增加基于角色与 `path + command` 的服务端授权、内置角色、系统管理页和 09 集成示例，并保持未启用 RBAC 的现有项目兼容。

**Architecture:** 身份提供方只把标准化 RoleCode 列表写入 JWT；REST 认证中间件在 token 撤销校验之后、业务认证 hook 与 Router 之前调用 Core Manage authorizer。权限数据由 Core 系统模型保存，`core.system_admin` 与 `core.viewer` 动态计算，不展开存储。第一版不裁剪菜单和按钮，拒绝操作统一返回 403，便于验证配置即时生效。

**Tech Stack:** Go、Gin、GORM、Core ModelList/Manage、JWT、现有 integration test harness。

---

## 约束

- 不改 `web/admin` 子模块和 `pkg/server/run/dist`。
- 不改 `/api/servermanage/getmenu` 的返回与认证域。
- 不加入用户名或环境变量绕过，也不把权限明细写入 token。
- 不使用数据库迁移脚本；沿用 Core 首次访问自动建表。
- 不删除现有公开类型；需要替代的类型只加 Deprecated 说明。
- 每项行为修改先看到目标测试失败，再做最小实现。

### Task 1: 定义角色、权限和稳定 command 公共契约

**Files:**
- Create: `pkg/server/types/manageauth.go`
- Create: `pkg/server/types/manageauth_test.go`
- Modify: `pkg/server/types/routerinfo.go`
- Modify: `pkg/server/types/routerinfo_test.go`

- [x] 写失败测试：内置 RoleCode、角色结果结构、权限键和值校验，以及 Manage Router 的稳定 command（忽略泛型后缀）。
- [x] 运行 `go test ./pkg/server/types/... -count=1`，确认 RED。
- [x] 增加标准角色引用、Principal、`IManageRoleProvider`、内置角色常量和校验函数。
- [x] 为 `RouterInfo` 增加 `GetCommand()`，非 Manage 路由返回空字符串。
- [x] gofmt 并重跑测试，确认 GREEN。
- [x] 提交：`feat(auth): define manage role contracts`。

### Task 2: 增加 Core 角色及权限明细模型

**Files:**
- Create: `pkg/server/smodels/managerolemodel.go`
- Create: `pkg/server/smodels/managerolepermissionmodel.go`
- Create: `pkg/server/smodels/managerolemodel_test.go`
- Modify: `pkg/server/smodels/userpermissionsmodel.go`

- [x] 写失败测试：RoleCode 唯一且不可变；权限按 `RoleCode + Path + Command` 唯一；内置角色标识可识别。
- [x] 运行 `go test ./pkg/server/smodels/... -count=1`，确认 RED。
- [x] 实现 `ManageRoleModel` 与 `ManageRolePermissionModel`，权限集合使用结构化行而不是逗号分隔字符串。
- [x] 保留旧模型并加 Deprecated 注释，不做破坏性删除。
- [x] gofmt 并重跑测试，确认 GREEN。
- [x] 提交：`feat(auth): add manage role persistence models`。

### Task 3: 实现授权策略与持久化查询

**Files:**
- Create: `pkg/server/manageauth/authorizer.go`
- Create: `pkg/server/manageauth/authorizer_test.go`
- Create: `pkg/server/manageauth/store.go`
- Create: `pkg/server/manageauth/store_test.go`

- [x] 写失败测试：`core.system_admin` 放行全部；`core.viewer` 只放行精确 `view/search`；自定义角色按 path/command 并集授权；无权限拒绝。
- [x] 写失败测试：权限修改后下一次请求读取新结果；未知/数据库错误不放行。
- [x] 运行 `go test ./pkg/server/manageauth/... -count=1`，确认 RED。
- [x] 实现 authorizer 和基于 Core 系统模型的 store，禁止字符串拆分权限。
- [x] 将拒绝转换为明确的 Forbidden PublicError，内部查询错误保持安全错误消息。
- [x] gofmt 并重跑测试，确认 GREEN。
- [x] 提交：`feat(auth): authorize manage commands by role`。

### Task 4: 只把 RoleCode 写入并恢复 JWT

**Files:**
- Create: `pkg/server/manageauth/claims.go`
- Create: `pkg/server/manageauth/claims_test.go`
- Modify: `pkg/server/safe/claims.go`
- Modify: `pkg/server/safe/claims_test.go`
- Modify: `pkg/server/safe/jwt_secret.go`

- [x] 写失败测试：角色 JSON 能往返；顺序稳定、去重、数量和长度受限；权限明细不进入 claim；业务 `AddData` 不能覆盖保留 key。
- [x] 运行相关测试，确认 RED。
- [x] 增加仅面向角色 claim 的窄接口，将 `manage_roles` 纳入保留字段。
- [x] 非法或缺失 claim 在 RBAC 已启用时由解析边界返回错误，供中间件 fail closed。
- [x] gofmt 并重跑测试，确认 GREEN。
- [x] 提交：`feat(auth): carry manage role codes in tokens`。

### Task 5: 接入登录、刷新与 TestToken

**Files:**
- Modify: `pkg/server/types/servicecontext.go`
- Modify: `pkg/server/api/public/auth_helpers.go`
- Modify: `pkg/server/api/public/callback.go`
- Modify: `pkg/server/api/public/refresh.go`
- Modify: `pkg/server/api/public/testtoken.go`
- Modify/Create: corresponding `*_test.go`

- [x] 写失败测试：存在 Provider 时登录/刷新加载角色；Provider 错误 fail closed；Provider 不存在保持兼容。
- [x] 写失败测试：Manage TestToken 固定携带 `core.system_admin`，且不触发真实用户“首个用户”分配。
- [x] 运行 `go test ./pkg/server/api/public/... ./pkg/server/router/... -count=1`，确认 RED。
- [x] 在 `ServiceContext` 发现 `IManageRoleProvider`，统一在 token 签发前写角色。
- [x] 保持 Casdoor 只负责身份；角色初始化由消费方 Provider 完成。
- [x] gofmt 并重跑测试，确认 GREEN。
- [x] 提交：`feat(auth): issue manage roles in auth tokens`。

### Task 6: 在 REST Manage 链路集中鉴权

**Files:**
- Modify: `pkg/server/trans/rest/authrequest.go`
- Modify: `pkg/server/trans/rest/authrequest_test.go`
- Modify: `pkg/server/trans/rest/restserver.go`

- [x] 写失败测试验证顺序：JWT/撤销校验 → Core RBAC → 业务 auth hook → Router。
- [x] 写失败测试：无权限返回 403；Router/Manage hook 未执行；Provider 缺失沿用旧行为；Provider 启用后 claim 缺失或非法 fail closed。
- [x] 运行 `go test ./pkg/server/trans/rest/... -count=1`，确认 RED。
- [x] 在认证中间件加入 Manage 路由 authorizer，不在 `ManageService.ValidationBefore` 重复实现。
- [x] 确保 `errors.Is/errors.As` 与 PublicError 响应链仍成立。
- [x] gofmt 并重跑测试，确认 GREEN。
- [x] 提交：`feat(auth): enforce manage RBAC in REST middleware`。

### Task 7: 为菜单快照保留稳定 command

**Files:**
- Modify: `pkg/server/types/servicecontext.go`
- Modify: `pkg/server/api/servermanage/menumanage.go`
- Modify/Create: corresponding `*_test.go`

- [x] 写失败测试：菜单扫描返回 route path 与标准 command，且旧字段完全保留。
- [x] 运行定向测试，确认 RED。
- [x] 给内部 `MenuRouterSnapshot` 增加 command 并用于权限绑定候选项。
- [x] 不过滤 `/api/servermanage/getmenu` 的菜单或按钮。
- [x] gofmt 并重跑测试，确认 GREEN。
- [x] 提交：`feat(auth): expose stable manage commands to role binding`。

### Task 8: 增加系统角色和权限绑定 Manage 页面

**Files:**
- Create: `pkg/server/api/manage/managerolemanage.go`
- Create: `pkg/server/api/manage/managerolepermissionmanage.go`
- Create: `pkg/server/api/manage/managerolemanage_test.go`
- Modify: `pkg/server/server.go`

- [x] 写失败测试：角色 CRUD、权限绑定、内置角色不可删除/改 Code/编辑静态权限。
- [x] 写失败测试：菜单绑定默认产生 `view`、`search` 两条权限，重复绑定幂等。
- [x] 运行定向 `pkg/server/api/manage`、`pkg/server/smodels` 与 `pkg/server` 测试，确认 RED。
- [x] 实现两个标准 Manage 页面并注册；文案保持框架通用，不出现业务项目概念。
- [x] 不添加菜单/command 隐藏逻辑。
- [x] gofmt 并重跑测试，确认 GREEN。
- [x] 提交：`feat(manage): add role and permission management`。

### Task 9: 新增 09 管理员角色集成示例

**Files:**
- Create: `examples/09-admin-manage-rbac/README.md`
- Create: `examples/09-admin-manage-rbac/main/main.go`
- Create: `examples/09-admin-manage-rbac/models.go`
- Create: `examples/09-admin-manage-rbac/provider.go`
- Create: `examples/09-admin-manage-rbac/model_repository.go`
- Create: `examples/09-admin-manage-rbac/api/manage/*.go`
- Create: `examples/09-admin-manage-rbac/*_test.go`

- [x] 写失败测试：消费方管理员用户与用户角色关系只保存 RoleCode。
- [x] 写失败测试：首个真实注册用户原子分配 `core.system_admin`，后续用户分配 `core.viewer`；TestToken 不消费首用户名额。
- [x] 写失败测试：用户角色变更只在新 token 生效，权限明细变更下一请求生效。
- [x] 运行示例测试，确认 RED。
- [x] 实现 Provider、用户/关系 Manage 页面和 README；说明 Casdoor 不承载 Core 权限继承。
- [x] gofmt 并重跑测试，确认 GREEN。
- [x] 提交：`feat(examples): demonstrate manage RBAC integration`。

### Task 10: 覆盖真实 HTTP 授权行为

**Files:**
- Create/Modify: `examples/09-admin-manage-rbac/http_test.go`
- Modify: `pkg/server/trans/rest/authrequest_test.go`

- [x] 写端到端测试：viewer 的 view/search 成功，add/edit/remove 返回 403，自定义角色精确放行，system_admin 全放行。
- [x] 覆盖显式点击无权限 command 的响应状态、公开 code 和安全 message；断言业务 handler 未执行。
- [x] 覆盖 Provider 不存在的兼容路径，以及权限存储异常的 fail-closed 路径；Provider 签发异常由 Public API 测试覆盖。
- [x] 运行 HTTP 示例与 REST 测试，确认 GREEN。
- [x] 提交：`test(auth): cover manage RBAC over HTTP`。

### Task 11: 同步权威 skill 和公共兼容性文档

**Files:**
- Modify: `docs/ai/core-skill/SKILL.md`
- Modify: `docs/ai/core-skill/auth-casdoor-and-admin.md`
- Modify: `docs/ai/core-skill/manage.md`
- Modify: `docs/ai/core-skill/models.md`
- Modify: `docs/ai/core-skill/testing-and-release.md`
- Modify: `docs/codex/API_COMPATIBILITY_SURFACE.md`
- Modify: `docs/codex/CONSUMER_AI_SKILL_SETUP.md`
- Modify: `README.md`
- Modify: `CHANGELOG.md`

- [ ] 补充角色模型、Provider、claim、内置角色、首用户策略、鉴权顺序和 09 示例入口。
- [ ] 明确第一版不做菜单/按钮过滤，前端显示不等于授权，后端 403 是权威结果。
- [ ] 将新能力登记为 additive MINOR 公共契约；记录旧模型 Deprecated 状态，不做移除。
- [ ] 运行文档链接、skill 校验和兼容性检查。
- [ ] 提交：`docs(auth): document manage RBAC contracts`。

### Task 12: 全量验证与交付检查

**Files:**
- Verify only; only fix regressions directly caused by this branch.

- [ ] 运行 `gofmt` 检查和 `git diff --check`。
- [ ] 运行所有受影响包的定向测试与 `-race` 测试。
- [ ] 运行 09 示例真实 HTTP 测试。
- [ ] 运行 `CGO_ENABLED=0 ./scripts/test.sh quick`、完整相关测试、release/compatibility checks。
- [ ] 如本机 macOS SDK 仍阻止 cgo，保留原始失败并明确标记环境失败，不能写 PASS。
- [ ] 检查 `web/admin`、`pkg/server/run/dist`、原工作树均未改动。
- [ ] 做最终代码审查，修复后重新执行受影响验证。
- [ ] 提交必要修复；记录最终 commit、测试矩阵、公共契约变化和建议版本 `v1.3.0`。
- [ ] 明确未 push、未 tag、未发布、未部署。
