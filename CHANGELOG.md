# 变更日志

本文记录 `github.com/digitalwayhk/core` 的重要变更，格式遵循 Keep a Changelog，版本遵循 SemVer。

## [Unreleased]

### Added

- `types.SearchItem.SkipCount`：业务热路径可显式放弃完整结果总数，使 `IDataAction.Load` 直接执行有界查询，避免点查固定产生 `COUNT(*)` + `SELECT` 两次数据库往返；零值继续保留原分页总数语义。
- 可选分键可靠并发：`UseOutboxWithOptions`、`Subscription.KeyConcurrency` 与 `KeyedReliableMQProvider` 默认均保持并发度 1；显式启用后同 OrderingKey串行、不同 key有界并行，Redis仍保持单 active owner并分页处理 PEL。provider不支持或同 subject配置冲突时 fail closed；新增真 Redis conformance、Runtime低基数指标和 Bitzoom全局串行失败案例。
- 可选 `IHMACAuthProvider`：只有显式实现该接口的服务才为 Auth 用户域增加 HMAC 凭证备选；Bearer 保持默认且始终优先，Manage/ServerManage 不进入 HMAC 分支。REST 与 WebSocket 复用现有可信身份和 `OnAuthRequest` 授权链，Core 不保存 Secret/nonce，也不实现业务签名算法。
- 必过门禁 `required/web-dist-sync`（`scripts/check-web-dist-sync.sh`）：校验已提交的内嵌前端产物 `pkg/server/run/dist/build-info.json` 的 `frontend_commit` 与 `git ls-tree HEAD web/admin` 的子模块指针一致。此前只有 `scripts/test-build-web-admin.sh` 用合成 fixture 验证构建脚本行为，没有任何门禁看真实产物，`web/admin` 指针前进而 dist 未重建时服务会静默内嵌旧前端。校验只读、不联网、不需要 node/yarn，约 1 秒；配套契约测试 `scripts/test-check-web-dist-sync.sh` 与本地入口 `./scripts/test.sh web-dist-sync`。
- 管理后台中英文切换：请求头 `X-Locale`（`zh-CN` / `en-US`，缺省与无法识别一律回退 `zh-CN`）、解析包 `pkg/server/locale`、加性接口 `types.ILocaleTitle`，以及 `DirectoryModel` / `MenuModel` 的 `TitleEN` 列（框架自动补列，无需迁移脚本）。`getmenu` 按当前语言填写 `title`，`View.Do` 的页面标题、标准命令和 `ID`、`CreatedAt` 等框架公共字段也有了中英默认标题。未实现 `ILocaleTitle` 的服务行为不变。详见 `docs/ai/core-skill/manage.md` 与 `docs/ai/core-skill/openapi-and-frontend.md`。
- 声明式业务统计 `pkg/persistence/entity/stats`：`StatSpec`、Store、Dashboard、**StatsEngine**、`CompileClickHouse`；**服务级报表** `ReportDef`（与 Dashboard 分离，多菜单位于服务下）。示例 07：多报表 API + Admin `/report/:service/:code`（图表/表/钻取/跳转 Manage）。
- ServerManage AI 提供商运行时配置：`POST /api/servermanage/aiprovider`、`saveaiprovider`、`testaiprovider`，持久化 `etc/aiprovider.json`。
- PageAgent 同源 LLM 代理：`POST /api/servermanage/aillm/chat/completions`（OpenAI 兼容透传）；`view=runtime` 仅下发代理 baseURL，上游 API Key 不进入浏览器。
- 管理端集成 `page-agent`（可选自然语言 GUI Agent）；优先读服务端 AI 配置并经 `customFetch` 注入 Manage JWT，env 仅作 fallback。
- 消费方 AI skill 安装说明与脚本：`docs/codex/CONSUMER_AI_SKILL_SETUP.md`、`scripts/link-consumer-skill.sh`；README「AI 助手与 Skill」。
- skill / 场景指南登记 **Manage 动态分库** 标准管道：`IDBName` + Where 写回 `searchHook` + MySQL `config.Database=""`；明确禁止 `OnSearchBefore`+`stop=true` 旁路列表。详见 `docs/ai/core-skill/manage.md`。
- 多服务运行与请求聚合监控（Runtime Observability）：go-zero/Prometheus 历史源、Core 调用边与 gRPC 入站低基数指标、ServerManage `runtimetopology`/`runtimeservice`、示例 07 Prometheus scrape，以及 Admin 运行视图。详见 `docs/superpowers/specs/2026-07-27-service-runtime-graph-design.md`。
- `ServerConfig.RuntimeObservability` 查询端配置（`Mode=off|prometheus`）与能力矩阵登记。
- 配置到运行时闭集门禁、公共 API/OpenAPI/路由兼容基线和类型化公共错误契约。
- `PrefixedBadgerDB.UseWriteBehind` 绑定入口、显式损坏恢复策略和可识别的 `PendingSyncError`。
- `ReliableWriteStore` 统一封装本地持久写、批量提交、准入背压、服务/实例目录隔离，以及 Insert/Update/Delete 可靠操作。
- `WriteBehindTarget`、有界 `ForceSyncBatch` 和批次确认协议，支持业务自定义远端幂等汇合。
- Casdoor Auth/Manage 独立客户端、持久撤销权威、可靠 Webhook 控制事件，以及签发/请求/事件三类服务 Hook。
- Redis ClusterProvider、ServiceResolver、可靠 Redis Streams 控制订阅，以及演示三服务协同和两种部署方式的示例 06。
- 默认内部 gRPC 传输：复用 go-zero zrpc Client、标准 gRPC health、独立 ServiceContext 生命周期、TLS/mTLS/mesh 和协议级三进程验证。

### Changed

- **菜单同步刷新展示标题**：`syncOneMenu` 过去在权限集合未变时直接返回，存量菜单的标题永远停留在首次落库的值。现在权限比较与展示标题比较分开判断，权限未变但代码里的中英标题变了也会写库；`Sort`、`Icon`、`Description` 仍是用户字段，不被生成结果覆盖。目录同步遵循同一规则。
- **标准命令默认标题按语言生成**：`RouterToCommand` 签名不变，但默认语言下 `add`、`edit`、`remove`、`submit`、`release` 的 `Title` 从 `Add`、`Edit` 等英文类型名变为中文；`Command` 与 `Name` 仍是稳定键，消费方可继续用 `ViewCommandModel` 覆盖。需要显式指定语言时使用新增的 `RouterToLocaleCommand`。
- **UpdateMenu 报表菜单**：不再把 `reports.List` / `reports.View` 扫成 List/View 两行；改为按 `ReportDef` **一个报表一行**（Name=code、Title=菜单名、Url=`/report/{service}/{code}`），并清理历史 API 伪菜单。

- skill 澄清 Manage/`ModelList`、public/private/`IDataAction` 与 04/07 高吞吐写三层数据访问；动态分库与「写死只能 SQLite」脱钩。
- 未分类 HTTP 错误改为 fail-closed 500；TypeError parse/validation/do 使用稳定状态映射。
- Badger 损坏恢复默认保留目录并启动失败；write-behind 要求持久写、冲突检测且禁止 pending TTL。
- 示例 04/07 的订单热路径改用框架 `ReliableWriteStore`；示例 07 每个水平副本使用独立 Badger 路径，并通过共享 MySQL 批量汇合订单与 Outbox。
- Casdoor 回调迁移到 `/api/casdoor/callback`；Access/Refresh Token 强制携带认证提供方、外部 Subject 和撤销世代。
- 路由缓存默认使用 local L1，只有调用 `UseCache` 的 API 会写入；L1 按进程有效内存自动解析共享字节预算，并在所有层统一返回 `json.RawMessage`。
- BREAKING: 内部同步调用默认改为 gRPC，节点发现发布 `GRPCPort`；HTTP 只允许显式发送前 fallback，发送开始后不跨协议重试。迁移说明：`docs/codex/GRPC_TRANSPORT_MIGRATION.md`。
- BREAKING: OpenAPI 改为匿名 `/api/openapi` 外部视图和使用 `ServerManageAuth` 的 `/api/internal/openapi` 内部视图；移除 `/api/servermanage/openapi`。迁移说明：`docs/codex/BREAKING_CHANGE_APPROVAL.md` 的 `openapi-audience-split-v1`。
- BREAKING: REST 认证移除 Logto，仅保留框架 JWT 与 Casdoor 身份生命周期；同步服务发现统一使用 `ServiceResolver`，异步事件统一使用 EventBridge。

### Deprecated

- 进程级请求状态、CrossNode 转发和 TestResult 兼容入口，详见 `docs/codex/DEPRECATION_REGISTER.md`。
- 旧 `RouterStats` / `GetAllRouterStats` / 未注册 `Statistics` 统计链路；生产路径不再产生统计，替代为 Runtime Aggregator + Prometheus。删除须进入批准的破坏性版本。
- `PrefixedBadgerDB.SetSyncDB` 与 `EnableWriteBehind`；前者无绑定错误，后者仅为 `ModelList`/`IDataAction` 兼容层，新代码统一使用 `UseWriteBehind(WriteBehindTarget)`。
- `public.Callback`、`public.Casdoor` 类型别名；新代码使用 `CasdoorCallback`、`CasdoorConfig`。
- `RouteCacheL1Config.Limit`；新配置使用 `MaxEntries`，并可通过 `MaxValueBytes` 和 `MaxBytes` 限制序列化缓存数据量。
- 补登此前已在代码标记 Deprecated 但漏登记的表面：`safe.Claims.GetToken` / `ValidateJWTToken`、`RouterInfo` 兼容导出字段、`casdoor` 旧中间件签名、`types.WebSocketNotificationSystem`，以及整包 `pkg/fileserver`、`pkg/dec`、`pkg/localization`。替代入口与迁移证据见 `docs/codex/DEPRECATION_REGISTER.md`。

### Removed

- 旧 Casdoor 回调路由 `/api/callback`；前端从 `/api/casdoor` 响应读取新回调地址。
- 自定义内部 Socket 的两个实现包、`-socket` 参数、Socket 配置/发现/payload 字段、旧 `GRPCTransportConfig.Enable` 及相关公开 Go API。WebSocket 与 Unix socket 不受影响。
- 未使用的实验性 `utils.Publisher`。进程内事件改用 `pkg/server/event.Stream`，服务事件改用 `ServiceContext` 管理的 EventBridge。
- 重复的 `public.OpenAPI` 生成器及 `/api/servermanage/openapi`；内部调用方迁移到 `/api/internal/openapi`。
- Logto 配置、中间件、身份常量及专用 JWT/JWKS 依赖。
- `Service.AttachService`、`SubscribeRouters`、Router Observe/Notify 类型与系统路由，以及动态设置服务地址 API。
- 顶层配置 `RunIp`、`ParentServerIP`、`AttachServices`、`Debug`、`CustomerDataList`；旧 JSON 在读取时幂等删除这些键。
- WebSocket `call` 事件及其 `melody.Call` 常量、`handleCall` 实现和专用请求解析；该事件从未被文档、前端或示例使用，`/ws` 只保留 `sub`、`unsub`、`get`，未识别事件统一返回「不支持的事件类型」。需要调用业务接口的客户端使用 HTTP 路由，需要持续推送的使用 `sub`。

### Fixed

- `SearchItem.SkipCount` 在带 `IScopes` 模型上先应用 Scope 再解析排序字段，避免 CamelCase 字段在 MySQL 中未映射成 snake_case 列名。
- MySQL 活动事务已经绑定连接后不再对基础 `*sql.DB` 额外执行 `Ping`，避免并发事务数等于 `MaxOpenConns` 时所有事务等待下一条连接而无法 Commit/Rollback；事务连接错误继续由 SQL、Commit 或 Rollback 原样返回。
- HMAC OpenAPI 扩展的 `available_inputs` 补回 `signature`，与运行时实际抽取的凭证字段保持一致。
- OpenAPI 零服务生成、配置静默接受、生命周期和并发关闭问题。
- write-behind 同 key 重复写入或待同步 Set 后软删除的 pending 计数漂移，以及损坏同步项被静默跳过的问题。
- write-behind 二次绑定静默成功、手动同步忽略批次上限、部分远端成功确认丢失，以及订单查询被过期本地 pending 覆盖的问题。
- 注销后的已验证 Casdoor 用户可以重新登录，新 Token 使用当前世代，旧 Token 继续失效。
- `NewSessionSubscriptions` 丢弃 `sr` 参数导致会话在首次订阅前持有空 `ServiceRouter`，全新会话发送订阅以外的事件会解引用空指针；`sr` 现在在构造时赋值，随之删除多余的 `setServiceRouter`。
- 恢复 `handleMessage` 中被注释掉的 recover，并为 `handleGet` 补上与 `handleSubscribe`/`handleUnsubscribe` 一致的会话空值检查；WebSocket 读循环中的恐慌不再逃逸，客户端收到「服务器内部错误」而连接保持可用。

### Security

- 升级 `google.golang.org/grpc` 至 v1.83.1，修复 CVE-2026-84304 / GHSA-vp52-pcj8-j9qc：远程客户端可通过大量极小 HTTP/2 DATA frame 放大接收缓冲区内存开销，导致 gRPC Server 堆内存耗尽和拒绝服务。同步采用该版本要求的 OpenTelemetry v1.44.0 与 Google Genproto 版本。
- 默认 REST 错误响应不再暴露内部 cause；代理、本地访问、JWT/Casdoor 和 CORS 使用 fail-closed 策略。
- 匿名 OpenAPI 不再暴露内部专用路由和 `x-internal-callers`；内部文档使用独立 ServerManage 认证域并禁止缓存。
- 未同步 Badger 数据不再被损坏自动重建或 TTL 静默删除，关闭积压会返回错误。
- Casdoor Webhook 使用独立 Secret、请求上限、域绑定和幂等持久化；REST/WebSocket 每次认证均校验撤销权威，内部 JWT 失败日志不再转储 Authorization Header。
- 跨主机 gRPC 默认要求 mTLS；`mesh` 仅适用于已有双向身份校验的服务网格，生产禁止 `insecure`。
- 修复 `/ws` 的认证绕过：WebSocket `call` 事件直接用客户端给定的 channel 查询 public/private/manage 三张路由表并执行 `ExecDo`，跳过整条 HTTP 中间件链（JWT、限流、访问日志、指标）。未认证会话可借此读取 Manage 列表数据，并以空 UID 执行 Private 路由。该事件已删除，`/ws` 上唯一执行路由代码的事件是 `sub`，它对 `Auth=true` 与 Private 路由在解析请求前强制校验会话身份。回归测试：`examples/integration/01-simple-shop/websocket_auth_boundary_test.go`。
- `/ws` 的 `sub` 事件改为按路由所属认证域验签，与 REST 侧 `resolveRouteAuthPolicy` 同构：Manage 路由用 `ManageAuth`、ServerManage 路由用 `ServerManageAuth`，其余仍用 `Auth`。此前 `authorizeAuthenticatedSubscription` 把密钥与 AuthType 都写死为用户域，而 `routeRequiresWebSocketAuth` 只判断 `Auth || PrivateType`、Manage 路由又显式 `WithAuth(true)`，因此普通用户 Token 能通过 Manage 与 ServerManage 路由的验签；跨域订阅当时未真正建立，靠的是验签之后几道与认证无关的护栏（Manage 路由未实现 `IWebSocketUserIdentity`、`RouteWebSocketHub` 的服务归属校验），隔离不由认证层保证。回归测试：`pkg/server/trans/websocket/melody/auth_boundary_test.go` 的 `TestAuthenticatedSubscriptionSelectsAuthDomainPerRoute`（含两域密钥被配成同一个时仍须按 AuthType 拒绝），以及 `examples/integration/01-simple-shop/websocket_auth_boundary_test.go` 的 `TestWebSocketSubscribeEnforcesAuthDomain`。
- 升级 `github.com/getkin/kin-openapi` 至 v0.144.0、`google.golang.org/grpc` 至 v1.82.1，处理三条依赖公告。本仓库只使用 kin-openapi 的 `openapi3` 与 `openapi3gen`，未使用 `openapi3filter` 和 `ValidationHandler`，因此 GHSA-r277-6w6q-xmqw（认证 fail-open，CVSS 9.1）与 GHSA-jpcw-4wr7-c3vq（请求校验空指针）在当前代码中不可达；GHSA-hrxh-6v49-42gf 中真正相关的是 HTTP/2 Rapid Reset 缓解绕过导致的拒绝服务，其 xDS RBAC 部分不适用（未使用 xDS）。

[Unreleased]: https://github.com/digitalwayhk/core/compare/v0.0.247...HEAD
