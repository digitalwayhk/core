# Casdoor 认证生命周期破坏性变更批准

- 变更 ID：`casdoor-auth-lifecycle-v1`
- Owner：`server/api/public`、`server/authstate`
- 批准日期：2026-07-16
- 目标版本：下一个 `Unreleased` 候选版本

## 批准范围

1. 删除旧 HTTP `/api/callback`，前端必须先调用 `/api/casdoor?type=auth|manage`，并使用返回的 `background_callback_url`（当前为 `/api/casdoor/callback`）。
2. `public.Callback` 和 `public.Casdoor` 只保留 Go 类型别名兼容，新代码使用 `CasdoorCallback` 和 `CasdoorConfig`。
3. Casdoor Access/Refresh Token 新增并强制校验 `auth_provider`、`provider_subject`、`auth_generation`；Auth 与 Manage 密钥和用途严格隔离。
4. 升级部署必须轮换 Auth/Manage 的 Access 与 Refresh 四个 Secret，旧 Token 全部失效，用户和管理员重新登录。

## 安全与回滚

- shared 撤销模式必须同时具备 Redis 权威和 MQ EventBridge 控制通道；任何一个不可用时认证面 fail closed。
- Webhook 使用独立 Secret，限制 64KiB，并禁止日志记录 Header、Payload、Claims 或 Token。
- 回滚到不识别新 Claims 的版本不受支持；需要回滚时先停止流量、恢复旧配置并再次轮换全部 Token Secret。

## 验证证据

- `examples/integration/casdoor-auth-lifecycle`
- `./scripts/test.sh security`
- `./scripts/test.sh integration-casdoor-auth`
- `./scripts/ci.sh required/contracts`
