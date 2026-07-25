# AuthHook 首轮外部审查修复复审提示词

请对 AuthHook 与系统 Public API 限流的首轮外部审查修复进行**只读复审**。不要修改代码、文档、Git 索引或提交历史。

## 审查范围

```bash
cd /Users/vincent/Documents/存档文稿/MyCode/digitalway.hk/core-codex

# 首轮审查后的修复差异
git diff 03b6456..0648d45

# 整项实现最终状态
git diff 4d736da..0648d45
```

- 首轮审查目标：`85560f9`
- 首轮裁定：`CHANGES_REQUIRED`
- 修复目标：`0648d45`
- P0 修复提交：`c95b0ab`
- P1 修复提交：`aa0ecfc`
- 修复台账：`0648d45`
- 设计规格：`docs/copilot/AUTH_HOOK_DESIGN.md`
- 实施计划：`docs/superpowers/plans/2026-07-15-auth-hook-public-rate-limit.md`

当前主工作区存在范围外脏文件。必须在 detached 干净 worktree 或 `git archive 0648d45` 中验证修复目标，不得用脏树绿色替代提交态证据，也不得把范围外脏文件计为本修复缺陷。

## 上轮必须复核的问题

### P0-1：ServerRouterInfo 签名与调用方不一致

重点检查：

1. `ServerRouterInfo(item, options...)` 是否源代码兼容旧单参数调用。
2. 默认 servermanage 路径是否通过注册期 Option 生成，调用方 Option 是否确实被应用。
3. `TestToken.RouterInfo` 是否通过 `WithMethod` 声明 GET，注册后不再写冻结字段。
4. 干净 `0648d45` 下 `pkg/server/api/public` 是否能独立 build/test/vet。
5. 新测试是否使用真实 Public 路由类型并严格断言路径与 Method。

### P1-1：Casdoor 开启时 WebSocket 接受第三方原始 Token

重点检查：

1. `ValidateJWTToken` 是否彻底移除 `CasDoor.Enable -> casdoor.TokenParse` 分支。
2. WebSocket Logon 是否始终使用当前服务 `Auth.AccessSecret` 验证内置 Access Token。
3. 是否只接受 HS256，并强制 `uid`、`token_use=access`、`auth_type=auth`、有效 `iat/exp`。
4. Casdoor 开启时，框架签发的内置 Access Token 是否仍能成功登录。
5. RS256 Casdoor/第三方 Token、Refresh Token、旧无用途 Token 是否被确定性拒绝。
6. 这项收紧是否符合“WebSocket 仅面向最终外部用户，使用 auth Token”的既定架构，是否误伤 Manage/ServerManage 路径。

### P1-2：Casdoor AuthHandler 注入 context["user"]

重点检查：

1. `context["user"]` 注入和相关注释/import 是否已删除。
2. 兼容保留的 AuthHandler 是否仅验证 Casdoor Token 和设置既有响应头，不再建立框架认证身份。
3. 测试是否使用真实可验签 RS256 Casdoor Token，确认请求确实到达下游后再断言 context 不含 user，避免因 401 提前返回而假绿。

## 非阻断 P2 复核

请确认以下处理是否诚实，不要自动升级为阻断，除非能给出当前可触发的安全或兼容证据：

- `Claims.GetToken` 仅标记 Deprecated，尚未删除，以保留公共 Go API 源码兼容。
- WebSocket Access 验证已强制用途；go-zero REST Access 中间件仍未额外强制 `token_use/auth_type`。
- Hook 返回普通 error 时仍映射通用 500，内部原因不会泄露；业务方可返回类型化公开错误。
- QueryLog/Statistics 当前未注册暴露，未来重新注册时才需要配置限流。
- 已补错误 RefreshSecret 的明确负向测试。

## 必跑命令

建议先创建干净 worktree：

```bash
git worktree add --detach /private/tmp/core-auth-review-0648d45 0648d45
cd /private/tmp/core-auth-review-0648d45
```

然后执行：

```bash
GOCACHE=/private/tmp/core-auth-review-cache \
  go test ./pkg/server/safe ./pkg/server/safe/casdoor ./pkg/server/config \
  ./pkg/server/router ./pkg/server/api ./pkg/server/api/public \
  ./pkg/server/ratelimit ./pkg/server/trans/rest \
  ./pkg/server/trans/websocket/melody -count=1

GOCACHE=/private/tmp/core-auth-review-cache \
  go test -race ./pkg/server/safe ./pkg/server/safe/casdoor \
  ./pkg/server/router ./pkg/server/api ./pkg/server/api/public \
  ./pkg/server/ratelimit ./pkg/server/trans/rest \
  ./pkg/server/trans/websocket/melody -count=1

GOCACHE=/private/tmp/core-auth-review-cache \
  go test -race ./examples/integration/01-simple-shop -count=1 -timeout=15m

GOCACHE=/private/tmp/core-auth-review-cache \
  go vet ./pkg/server/... ./examples/integration/...

./scripts/check-logging.sh
GOCACHE=/private/tmp/core-auth-review-cache ./scripts/test.sh release-contract
```

`httptest`、REST 生命周期和真实示例进程需要允许监听本地端口。验证后移除临时 worktree。

## 必须返回的反馈

1. Findings 按 P0、P1、P2 排序，每项提供文件/行号、触发场景、影响和修复建议。
2. 分别裁定原 P0-1、P1-1、P1-2 为 `CLOSED` 或 `OPEN`，并给出代码与测试证据。
3. 说明干净提交态是否成功编译，不能只报告脏工作区结果。
4. 评价新增测试是否能在修复前失败、修复后通过，是否存在弱断言或提前返回假绿。
5. 说明严格 Access Token 校验是否造成公共 API、HTTP/JSON、配置或 WebSocket 兼容性风险。
6. 列出实际执行命令、退出码和关键结果；区分代码失败、端口权限和范围外环境问题。
7. 对登记的 P2 给出“保持 P2 / 升级 P1”及理由。
8. 最终裁定只能是 `APPROVED` 或 `CHANGES_REQUIRED`。
9. 明确回答：是否允许关闭本计划并进入下一任务。

