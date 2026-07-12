# 变更日志

本文记录 `github.com/digitalwayhk/core` 的重要变更，格式遵循 Keep a Changelog，版本遵循 SemVer。

## [Unreleased]

### Added

- 配置到运行时闭集门禁、公共 API/OpenAPI/路由兼容基线和类型化公共错误契约。

### Changed

- 未分类 HTTP 错误改为 fail-closed 500；TypeError parse/validation/do 使用稳定状态映射。

### Deprecated

- 进程级请求状态、CrossNode 转发和 TestResult 兼容入口，详见 `docs/codex/DEPRECATION_REGISTER.md`。

### Removed

- 暂无。

### Fixed

- OpenAPI 零服务生成、配置静默接受、生命周期和并发关闭问题。

### Security

- 默认 REST 错误响应不再暴露内部 cause；代理、本地访问、Logto 和 CORS 使用 fail-closed 策略。

[Unreleased]: https://github.com/digitalwayhk/core/compare/v0.0.247...HEAD
