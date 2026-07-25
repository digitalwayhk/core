# Web Runtime Auth 精准移植设计

## 背景

`feat/web-runtime-auth` 已实现 Manage Auth 权威选择、Web bootstrap、HTMLServer 同源认证
代理、Swagger 同源业务路由和 OpenAPI Host 安全处理，但这些文件未进入当前 `main`：

- `pkg/server/run/manageauth.go`
- `pkg/server/run/webbootstrap.go`
- `pkg/server/trans/rest/externalrouter.go`
- 对应的 HTMLServer/OpenAPI 测试和 Web Admin 构建链

旧分支同时包含当前已经删除的 Logto、`AttachServices`、Observe/Notify、`RunIp` 和不同的
生命周期实现，因此不能整体 merge 或按提交组直接 cherry-pick。

本设计不是重新发明 Web runtime auth，而是以旧分支最终实现和测试为基准，逐文件精准移植
到当前 `main`。兼容代码优先原样保留，只适配已经变化的当前契约。

## 目标

- 为独立 ViewPort 提供可匿名读取、无敏感信息的 `/api/web/bootstrap`。
- 从实际注册 Manage 路由的服务中选择唯一 Manage Auth 权威。
- 在 ViewPort 同源代理现有认证入口和业务 Manage/Public/Private 路由。
- 所有代理复用当前 Router 的认证、IP、解析、校验、执行和响应链。
- 保持匿名外部 OpenAPI 与受 `ServerManageAuth` 保护的内部 OpenAPI 分层。
- 保持旧分支已经验证的 IPv4、IPv6、非法 Host/端口和 mux 冲突防护。
- 不恢复任何已删除能力。

## 非目标

- 不恢复 Logto。
- 不恢复 `AttachServices`、`Service.AttachService`、Observe/Notify 或 `RunIp`。
- 不把 HTMLServer 合并进业务 REST Server，也不改变 ViewPort 部署模型。
- 不改变现有 Auth、ManageAuth、ServerManageAuth 的职责划分。
- 不在本批处理业务启动 admission barrier、示例 07 UAT fixture 或 Web Admin 构建脚本。
- 不整体 merge `feat/web-runtime-auth`。

## 移植策略

以旧分支最终文件和测试为参考，按当前 `main` 逐项落地：

1. 先迁移测试意图，在 `main` 证明能力缺失。
2. 对未依赖旧契约的逻辑优先保持原实现。
3. 只调整当前 API 冲突、生命周期和已经删除的配置。
4. 每个功能组独立 RED→GREEN 并提交。
5. 不携带旧分支中的无关文件、生成产物或历史重构。

## Manage Auth 权威

### 配置入口

`WebServer` 新增：

```go
func (own *WebServer) SetManageAuthAuthority(serviceName string) error
```

该关系属于同一 `WebServer` 进程的服务编排，不写入每个 `ServerConfig`。

- 只允许在 `Start()` 前设置。
- 服务名去除首尾空格并转为小写比较。
- 空字符串清除显式选择，恢复自动选择规则。
- 启动或初始化已经开始后调用返回错误。
- 不暴露可在运行期静默改变认证权威的 public 字段。

### 选择规则

候选服务必须真实注册至少一个 Manage Router。

- 没有候选：权威为空，bootstrap 返回 `unavailable`。
- 只有一个候选：自动选中。
- 多个候选：必须通过 `SetManageAuthAuthority` 显式指定。
- 指定服务不存在或没有 Manage Router：初始化失败。

框架内建 `server` 服务与业务服务同时提供 Manage 时，也执行相同规则；示例和调用方必须明确
选择真正具备业务认证 Hook/Claims 的权威服务。

### 兼容检查

所有 Manage 候选与权威至少检查：

- `ManageAuth.AccessSecret`
- `ManageAuth.AccessExpire`
- `ManageAuth.RefreshSecret`
- `ManageAuth.RefreshExpire`
- `ManageAuth.CasDoor.Enable`

Casdoor 启用且存在多个 Manage 服务时，同时检查：

- `AuthRevocation.Mode=shared`
- Redis 地址、密码和 Prefix 一致
- `ManageAuth.CasDoor.WebhookSecret` 一致
- Casdoor Endpoint、ClientID、ClientSecret、Certificate、Organization、Application 和
  FrontendURL 一致

错误只返回服务名和不兼容字段，不输出 Secret 值。

## Web Bootstrap

### 路径与缓存

- 路径：`GET /api/web/bootstrap`
- 匿名访问
- 所有响应包含 `Cache-Control: no-store`
- 非 GET 返回 `405` 和 `Allow: GET`

### 响应能力

响应只描述当前前端可使用的能力：

- `test_token`
- `casdoor`
- `unavailable`

同时返回规范化后的权威服务名和现有同源端点：

- `/api/servermanage/testtoken`
- `/api/casdoor`
- `/callback`
- `/api/refresh`
- `/swagger/`

bootstrap 不包含：

- Access/Refresh Secret
- Token
- Casdoor ClientSecret、Certificate 或 WebhookSecret
- Redis 密码
- 内部服务地址
- internal caller 白名单

TestToken 模式只通过现有本地访问策略判断是否可用，不提前签发 Token。

## HTMLServer 同源网关

### 保留入口

ViewPort 继续提供：

- `/`
- `/swagger/`
- `/api/openapi`
- 现有 QueryService 与 Demo 静态挂载

新增或恢复：

- `/api/web/bootstrap`
- `/api/servermanage/testtoken`
- `/api/casdoor`
- `/callback`
- `/api/refresh`
- 业务 Manage
- 普通 Public
- Private
- 受保护的 `/api/internal/openapi`

### 路由过滤

- `WithInternalCallers` 非空的内部专用 Public 不挂载到 ViewPort。
- ViewPort 的匿名 OpenAPI 也不描述内部专用路由，且不输出 `x-internal-callers`。
- `server` 系统服务的普通 Public/Private 不做同源挂载，避免无意扩大系统入口。
- Manage 与 `ServerManageAuth` 入口按现有职责保留。

### 完整安全链

同源代理禁止直接调用业务 `Do`。每个请求必须复用当前 Router 执行链：

1. 使用当前 `ServiceRouter` 创建 IRequest。
2. 通过 Trusted Proxy 计算客户端 IP。
3. 执行 IP 白名单。
4. 使用原 RouterInfo 的 Auth/ManageAuth/ServerManageAuth。
5. 执行 Parse、Validation、Do。
6. 使用原 ResponseHandler 或统一 JSON 响应。
7. 只向外返回现有 public error，不泄露内部错误。

认证代理由选定权威服务处理，不复制 Token/Casdoor 业务逻辑。callback、refresh 必须保留原
状态码、必要 Header 和响应体；日志不得记录 Token、完整 Header、body 或 Claims。

## OpenAPI 与 Swagger

### 文档分层

- `/api/openapi`：匿名外部文档，包含可对外的普通 Public、Private 和 Manage 描述，过滤
  内部专用路由。
- `/api/internal/openapi`：完整文档，通过 `ServerManageAuth` 保护，可包含内部专用路由。
- Swagger UI 继续读取外部文档；内部文档只供内部团队和兼容性检查使用。

### Servers URL

沿用旧分支最终安全处理：

- 使用 `net.SplitHostPort` 解析带端口 Host。
- 使用 `net.JoinHostPort` 生成 IPv4/IPv6 URL。
- ViewPort 文档指向 ViewPort 的同源地址。
- 非法 Host、非法端口和缺失端口不 panic。
- 无法信任输入时回退到安全的 `127.0.0.1` 与已验证端口。

## mux 冲突和生命周期

HTMLServer 在开始监听前完成：

1. 获取稳定的 ServiceContext/Router 快照。
2. 解析并验证 Manage Auth 权威。
3. 预占所有静态、认证、OpenAPI 和业务路径。
4. 检查重复 pattern、非法端口和冲突路由。
5. 构建完整 mux。

任何准备失败：

- 不启动 listener。
- 不暴露半初始化路由。
- 返回包含服务名、路径或配置字段的错误。
- 不输出敏感配置值。

`SetManageAuthAuthority` 在初始化开始后返回错误。HTMLServer Stop 保持幂等并关闭 listener，
不会因未完成准备或重复 Stop 阻塞。

## 测试设计

优先移植旧分支现有测试，再增加当前契约断言。

### 权威测试

- 无 Manage 候选。
- 单候选自动选择。
- 多候选未指定时失败关闭。
- 显式选择存在但无 Manage Router 的服务时失败。
- Access/Refresh、过期时间或 Casdoor Enable 不兼容时失败。
- Casdoor shared revocation 或配置不兼容时失败。
- 错误信息不包含 Secret。
- 初始化开始后修改权威失败。

### Bootstrap 测试

- test_token、casdoor、unavailable 三种模式。
- 规范化权威服务名。
- GET、405、Allow Header、`no-store`。
- JSON 不包含任何 Secret、Token、Redis 密码或 internal caller。

### 同源代理测试

- 四个现有认证入口均由权威服务处理。
- Manage/Public/Private 复用完整安全链。
- 未认证、错误权限和 IP 白名单在 Parse/Do 前拒绝。
- 原响应状态码、Header、body 和 public error 语义保持。
- `WithInternalCallers` 路由和系统服务普通 Public/Private 不挂载。
- 重复 mux pattern 在监听前失败。

### OpenAPI 测试

- 匿名外部文档过滤内部专用路由。
- 内部文档受 `ServerManageAuth` 保护并包含完整路由。
- ViewPort 同源 servers URL。
- IPv4、IPv6、非法 Host、非法端口和回退。

### 生命周期与并发

- HTMLServer 未准备不能开始监听。
- 准备失败不保留半成品 handler。
- Stop 幂等。
- 多 WebServer/HTMLServer 不共享 mux 或权威状态。
- `pkg/server/run`、`pkg/server/trans/rest` 执行 race。

## 验证门禁

至少执行：

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/run ./pkg/server/trans/rest ./pkg/server/api/public -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/server/run ./pkg/server/trans/rest -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -p 1 ./... -run "^$" -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy ./scripts/test.sh config-contract
GOCACHE=/private/tmp/core-codex-gocache rtk proxy ./scripts/test.sh release-contract
```

全仓 `gofmt -l` 的历史格式债单独记录；本批修改文件必须格式化且 `git diff --check` 通过。

## 分支收敛衔接

完成后更新 `BRANCH_CONSOLIDATION_AUDIT.md` 中对应 runtime auth、HTMLServer 和 OpenAPI
提交组。只有相关行为和测试进入 `main` 后才能把这些行改为“已合入”。

Web Admin 构建链、启动 admission barrier 和示例 07 UAT 仍保持“需要补入”，因此本批完成
后仍不能立即删除 `feat/web-runtime-auth` worktree 或分支。
