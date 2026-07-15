# 变更日志

本文记录 `github.com/digitalwayhk/core` 的重要变更，格式遵循 Keep a Changelog，版本遵循 SemVer。

## [Unreleased]

### Added

- 配置到运行时闭集门禁、公共 API/OpenAPI/路由兼容基线和类型化公共错误契约。
- `PrefixedBadgerDB.EnableWriteBehind`、显式损坏恢复策略和可识别的 `PendingSyncError`。
- Casdoor Auth/Manage 独立客户端、持久撤销权威、可靠 Webhook 控制事件，以及签发/请求/事件三类服务 Hook。

### Changed

- 未分类 HTTP 错误改为 fail-closed 500；TypeError parse/validation/do 使用稳定状态映射。
- Badger 损坏恢复默认保留目录并启动失败；write-behind 要求持久写、冲突检测且禁止 pending TTL。
- Casdoor 回调迁移到 `/api/casdoor/callback`；Access/Refresh Token 强制携带认证提供方、外部 Subject 和撤销世代。

### Deprecated

- 进程级请求状态、CrossNode 转发和 TestResult 兼容入口，详见 `docs/codex/DEPRECATION_REGISTER.md`。
- `PrefixedBadgerDB.SetSyncDB`；新代码使用可返回绑定错误的 `EnableWriteBehind`。
- `public.Callback`、`public.Casdoor` 类型别名；新代码使用 `CasdoorCallback`、`CasdoorConfig`。

### Removed

- 旧 Casdoor 回调路由 `/api/callback`；前端从 `/api/casdoor` 响应读取新回调地址。

### Fixed

- OpenAPI 零服务生成、配置静默接受、生命周期和并发关闭问题。
- write-behind 同 key 重复写入或待同步 Set 后软删除的 pending 计数漂移，以及损坏同步项被静默跳过的问题。
- 注销后的已验证 Casdoor 用户可以重新登录，新 Token 使用当前世代，旧 Token 继续失效。

### Security

- 默认 REST 错误响应不再暴露内部 cause；代理、本地访问、Logto 和 CORS 使用 fail-closed 策略。
- 未同步 Badger 数据不再被损坏自动重建或 TTL 静默删除，关闭积压会返回错误。
- Casdoor Webhook 使用独立 Secret、请求上限、域绑定和幂等持久化；REST/WebSocket 每次认证均校验撤销权威，内部 JWT 失败日志不再转储 Authorization Header。

[Unreleased]: https://github.com/digitalwayhk/core/compare/v0.0.247...HEAD
