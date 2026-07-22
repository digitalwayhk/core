# 如何让 REST 认证路由在内层拒绝缺失身份的请求

本文定义 REST 认证门禁的最小修复。受保护路由在进入 `RouterInfo.Exec`、`Parse` 或业务 `Do` 之前必须确认请求上下文已包含当前认证模式生成的可信身份。验证失败时返回稳定 401，不修改 `Request` 公共 API，也不重构 JWT、Logto 或 Manage Auth 中间件。

## 问题和目标

当前外层认证中间件会验证 JSON Web Token（JWT）或 Logto 身份，再将结果写入 HTTP 请求上下文。正常注册路径会在 `RouteHandler` 之前拒绝非法请求。

`RouteHandler` 目前只检查 `router.NewRequest` 是否返回 nil。`NewRequest` 在缺失 `uid` 和 `uname` 时仍返回对象，所以直接调用最内层 handler 会继续执行受保护路由。当测试 Router 实例创建失败时，请求最终变成 500，而不是预期的 401。

修复完成以下目标：

1. 受保护路由在业务路由实例创建前拒绝缺失可信身份的请求
2. 内部 JWT、Logto 和 Manage Auth 继续使用各自现有的验证证据
3. 非认证 Public 路由不受影响
4. 认证失败使用现有 `PublicErrorContract`，不泄露 token、claims 或内部错误
5. `release-contract` 中的 REST 安全测试恢复通过

## 方案选择

本次采用在 `RouteHandler` 内复用现有 `verifiedRequestIdentity` 的方案。该函数已知道如何区分内部 JWT 和 Logto 的可信证据，可避免将一个普通 `uid` 文本误当作已验证身份。

本次不采用以下方案：

- 不仅检查 `req.GetUser()`：该值只表示上下文存在用户字段，不能单独证明内部 JWT 已验证
- 不让 `NewRequest` 在身份缺失时返回 nil：该函数同时承担路由请求构造，改变返回语义会扩大到非 REST 调用方
- 不向 `Request` 增加新的导出认证状态：这会扩大公共 API，但不会比复用现有可信上下文提供更强保障

## 处理流程

`RouteHandler` 保留现有 IP 白名单和路由查找顺序。它在取得 `RouterInfo` 后执行以下逻辑：

1. `RouterInfo.Auth` 为 false 时直接进入原执行路径
2. `RouterInfo.Auth` 为 true 时，根据路由是否属于 `ManageType` 选择 User Auth 或 Manage Auth
3. 根据对应 `AuthSecret` 选择内部 JWT 或 Logto 模式
4. 调用 `verifiedRequestIdentity` 检查该模式的可信上下文
5. 验证失败时记录现有脱敏拒绝事件，并写入 `ErrorKindUnauthenticated` 对应的 401
6. 验证成功后才调用 `info.Exec(req)`

该内层检查是防御性边界。它不替代外层 token 校验、Casdoor 撤销权威或认证 Hook。

## 错误和日志

身份缺失或与路由认证域不匹配时，`verifiedRequestIdentity` 返回 `ErrorKindUnauthenticated`。`RouteHandler` 通过 `ResolvePublicError` 生成稳定公共错误，响应保持 401、公共错误码和 `authentication failed`。

拒绝日志复用 `logAuthRequestDenied`，只记录服务、路由、认证类型、脱敏身份摘要和公共错误码。日志不记录 Authorization header、token、claims 或请求体。

## 测试和验收

现有 `TestRouteHandlerRejectsNilAuthenticatedRequest` 已稳定复现 401 变成 500 的缺陷，并且在 `pkg/utils` 修改前的提交上也会失败。实现前保留该 RED 证据，修复后要求它返回 401 且不进入 Router 执行。

测试覆盖以下边界：

- 无身份的受保护路由返回 401
- 非认证 Public 路由继续执行
- 内部 JWT 已验证请求继续执行
- Logto 已验证请求继续执行
- User Auth token 不能通过 Manage Auth 路由
- 身份拒绝响应不包含 token、claims 或内部原因

实现后运行：

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/trans/rest -run 'TestRouteHandler|TestAuthRequest|TestInternalJWT' -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/server/trans/rest -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy ./scripts/test.sh release-contract
```

## 范围边界

该修复不改变 HTTP 路径、JSON 响应字段、成功响应、token 签发、Casdoor 撤销、Logto 校验、Manage Hook 或 `Request` 公共接口。它只在外层认证被绕过或丢失上下文时，将错误结果从不可控的 500 收紧为标准 401。

Otel 依赖升级不纳入该修复提交。只有 REST 定向测试、race 和 `release-contract` 通过后，才开始下一个独立升级任务。
