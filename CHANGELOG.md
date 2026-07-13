# 变更日志

本文记录 `github.com/digitalwayhk/core` 的重要变更，格式遵循 Keep a Changelog，版本遵循 SemVer。

## [Unreleased]

### Added

- 配置到运行时闭集门禁、公共 API/OpenAPI/路由兼容基线和类型化公共错误契约。
- `PrefixedBadgerDB.EnableWriteBehind`、显式损坏恢复策略和可识别的 `PendingSyncError`。

### Changed

- 未分类 HTTP 错误改为 fail-closed 500；TypeError parse/validation/do 使用稳定状态映射。
- Badger 损坏恢复默认保留目录并启动失败；write-behind 要求持久写、冲突检测且禁止 pending TTL。

### Deprecated

- 进程级请求状态、CrossNode 转发和 TestResult 兼容入口，详见 `docs/codex/DEPRECATION_REGISTER.md`。
- `PrefixedBadgerDB.SetSyncDB`；新代码使用可返回绑定错误的 `EnableWriteBehind`。

### Removed

- 暂无。

### Fixed

- OpenAPI 零服务生成、配置静默接受、生命周期和并发关闭问题。
- write-behind 同 key 重复写入的 pending 计数漂移，以及损坏同步项被静默跳过的问题。

### Security

- 默认 REST 错误响应不再暴露内部 cause；代理、本地访问、Logto 和 CORS 使用 fail-closed 策略。
- 未同步 Badger 数据不再被损坏自动重建或 TTL 静默删除，关闭积压会返回错误。

[Unreleased]: https://github.com/digitalwayhk/core/compare/v0.0.247...HEAD
