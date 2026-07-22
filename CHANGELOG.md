# 变更日志

本文记录 `github.com/digitalwayhk/core` 的重要变更，格式遵循 Keep a Changelog，版本遵循 SemVer。

## [Unreleased]

### Added

- 配置到运行时闭集门禁、公共 API/OpenAPI/路由兼容基线和类型化公共错误契约。
- `PrefixedBadgerDB.EnableWriteBehind`、显式损坏恢复策略和可识别的 `PendingSyncError`。
- `ReliableWriteStore` 统一封装本地持久写、批量提交、准入背压、服务/实例目录隔离，以及 Insert/Update/Delete 可靠操作。
- `WriteBehindTarget`、有界 `ForceSyncBatch` 和批次确认协议，支持业务自定义远端幂等汇合。
- Casdoor Auth/Manage 独立客户端、持久撤销权威、可靠 Webhook 控制事件，以及签发/请求/事件三类服务 Hook。
- Redis ClusterProvider、ServiceResolver、可靠 Redis Streams 控制订阅，以及演示三服务协同和两种部署方式的示例 06。
- 默认内部 gRPC 传输：复用 go-zero zrpc Client、标准 gRPC health、独立 ServiceContext 生命周期、TLS/mTLS/mesh 和协议级三进程验证。

### Changed

- 未分类 HTTP 错误改为 fail-closed 500；TypeError parse/validation/do 使用稳定状态映射。
- Badger 损坏恢复默认保留目录并启动失败；write-behind 要求持久写、冲突检测且禁止 pending TTL。
- 示例 04/07 的订单热路径改用框架 `ReliableWriteStore`；示例 07 每个水平副本使用独立 Badger 路径，并通过共享 MySQL 批量汇合订单与 Outbox。
- Casdoor 回调迁移到 `/api/casdoor/callback`；Access/Refresh Token 强制携带认证提供方、外部 Subject 和撤销世代。
- 路由缓存默认使用 local L1，只有调用 `UseCache` 的 API 会写入；L1 按进程有效内存自动解析共享字节预算，并在所有层统一返回 `json.RawMessage`。
- BREAKING: 内部同步调用默认改为 gRPC，节点发现发布 `GRPCPort`；HTTP 只允许显式发送前 fallback，发送开始后不跨协议重试。迁移说明：`docs/codex/GRPC_TRANSPORT_MIGRATION.md`。

### Deprecated

- 进程级请求状态、CrossNode 转发和 TestResult 兼容入口，详见 `docs/codex/DEPRECATION_REGISTER.md`。
- `PrefixedBadgerDB.SetSyncDB`；新代码使用可返回绑定错误的 `EnableWriteBehind`。
- `public.Callback`、`public.Casdoor` 类型别名；新代码使用 `CasdoorCallback`、`CasdoorConfig`。
- `RouteCacheL1Config.Limit`；新配置使用 `MaxEntries`，并可通过 `MaxValueBytes` 和 `MaxBytes` 限制序列化缓存数据量。

### Removed

- 旧 Casdoor 回调路由 `/api/callback`；前端从 `/api/casdoor` 响应读取新回调地址。
- 自定义内部 Socket 的两个实现包、`-socket` 参数、Socket 配置/发现/payload 字段、旧 `GRPCTransportConfig.Enable` 及相关公开 Go API。WebSocket 与 Unix socket 不受影响。
- 未使用的实验性 `utils.Publisher`。进程内事件改用 `pkg/server/event.Stream`，服务事件改用 `ServiceContext` 管理的 EventBridge。

### Fixed

- OpenAPI 零服务生成、配置静默接受、生命周期和并发关闭问题。
- write-behind 同 key 重复写入或待同步 Set 后软删除的 pending 计数漂移，以及损坏同步项被静默跳过的问题。
- write-behind 二次绑定静默成功、手动同步忽略批次上限、部分远端成功确认丢失，以及订单查询被过期本地 pending 覆盖的问题。
- 注销后的已验证 Casdoor 用户可以重新登录，新 Token 使用当前世代，旧 Token 继续失效。

### Security

- 默认 REST 错误响应不再暴露内部 cause；代理、本地访问、Logto 和 CORS 使用 fail-closed 策略。
- 未同步 Badger 数据不再被损坏自动重建或 TTL 静默删除，关闭积压会返回错误。
- Casdoor Webhook 使用独立 Secret、请求上限、域绑定和幂等持久化；REST/WebSocket 每次认证均校验撤销权威，内部 JWT 失败日志不再转储 Authorization Header。
- 跨主机 gRPC 默认要求 mTLS；`mesh` 仅适用于已有双向身份校验的服务网格，生产禁止 `insecure`。

[Unreleased]: https://github.com/digitalwayhk/core/compare/v0.0.247...HEAD
