# AuthHook 复审 P2 修复最终审查提示词

请对 AuthHook 首轮修复复审中登记的三个 P2 进行**只读最终审查**。不要修改代码、文档、Git 索引或提交历史。

## 范围

```bash
cd /Users/vincent/Documents/存档文稿/MyCode/digitalway.hk/core-codex

# P2 生产代码与测试
git diff dc5dd3e..619a99b

# 从上次复审目标到本次台账目标
git diff 0648d45..17f5550
```

- 上次复审目标：`0648d45`
- P2 修复提交：`619a99b`
- 台账目标：`17f5550`
- 实施计划：`docs/superpowers/plans/2026-07-15-auth-hook-public-rate-limit.md`
- 当前主工作区有范围外脏文件；必须使用 detached `17f5550` 或 `git archive` 验证。

## 必查 P2

### 1. WebSocket Casdoor 回归测试真实性

- 测试是否生成 RSA 证书并用公钥初始化 Casdoor SDK。
- Token 是否使用 `casdoorsdk.Claims` 且包含非空 `User.Id`，确保旧实现可成功解析并登录。
- 是否有可信证据证明同一测试在旧提交 `03b6456` 因 Logon 返回 nil 而失败，在新提交通过。
- 测试是否仍明确断言 Logon 返回 error 且 `subscriptions.req == nil`。

### 2. ServerRouterInfo 精确函数类型兼容

- `ServerRouterInfo(item interface{})` 是否恢复原精确签名，旧调用和函数值赋值均可编译。
- `ServerRouterInfoWithOptions` 是否新增为独立 API，并保持默认 servermanage PathResolver。
- 所有需要 Method/限流 Option 的系统 Public 路由是否迁移到新函数，是否有遗漏导致编译失败或限流丢失。
- 测试是否包含编译期函数类型契约，并验证真实 TestToken 的路径和 Method。
- 公共 API 变化是否为恢复旧接口 + 新增接口，而不是再次替换接口。

### 3. Access Token 验证 API 语义

- `ValidateAccessToken` 是否显式接收 secret、expectedAuthType、now。
- 是否严格验证 HS256、`token_use=access`、uid、认证类型、iat、exp。
- 是否支持 auth/manage/servermanage，并拒绝认证类型混用。
- WebSocket 是否直接调用新 API 并显式传入 `AuthTypeUser`。
- `ValidateJWTToken` 是否仅作为 Deprecated 兼容包装保留，没有删除公共符号。
- 是否出现 Access/Refresh 验证语义分叉、错误的时间判断或敏感错误泄露。

## 必跑命令

```bash
git worktree add --detach /private/tmp/core-auth-p2-final-review 17f5550
cd /private/tmp/core-auth-p2-final-review

GOCACHE=/private/tmp/core-auth-p2-final-cache \
  go test ./pkg/server/safe ./pkg/server/safe/casdoor ./pkg/server/config \
  ./pkg/server/router ./pkg/server/api ./pkg/server/api/public \
  ./pkg/server/ratelimit ./pkg/server/trans/rest \
  ./pkg/server/trans/websocket/melody -count=1

GOCACHE=/private/tmp/core-auth-p2-final-cache \
  go test -race ./pkg/server/safe ./pkg/server/safe/casdoor \
  ./pkg/server/router ./pkg/server/api ./pkg/server/api/public \
  ./pkg/server/ratelimit ./pkg/server/trans/rest \
  ./pkg/server/trans/websocket/melody -count=1

GOCACHE=/private/tmp/core-auth-p2-final-cache \
  go test -race ./examples/integration/01-simple-shop -count=1 -timeout=15m

GOCACHE=/private/tmp/core-auth-p2-final-cache \
  go vet ./pkg/server/... ./examples/integration/...

./scripts/check-logging.sh
GOCACHE=/private/tmp/core-auth-p2-final-cache ./scripts/test.sh release-contract
```

需要允许 `httptest`、REST 生命周期和真实示例进程监听本地端口。验证后清理临时 worktree。

## 必须返回

1. Findings 按 P0、P1、P2 排序，附文件/行号、触发场景、影响和修复建议。
2. 三个 P2 分别裁定 `CLOSED` 或 `OPEN`，并给出代码和测试证据。
3. 明确评价强化后的 Casdoor 测试是否在旧实现上真实失败，而非提前 401 或无效 Token 假绿。
4. 明确评价 `ServerRouterInfo` 的普通调用、函数值赋值和 Option 调用兼容性。
5. 明确评价 auth/manage/servermanage Access Token 的用途与类型隔离。
6. 列出所有实际命令、退出码和关键结果，必须包含干净提交态证据。
7. 最终裁定只能是 `APPROVED` 或 `CHANGES_REQUIRED`。
8. 明确回答：是否允许关闭 AuthHook 整项计划并进入下一任务。

