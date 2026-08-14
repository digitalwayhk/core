# 废弃 API 登记

最早删除版本是下限，不是自动删除指令；删除前仍需 public-api 门禁、迁移说明和消费方验证。

| API | 替代入口 | 首次登记版本 | 最早删除版本 | Owner | 消费方 | 迁移证据 |
| --- | --- | --- | --- | --- | --- | --- |
| `ManageService.Req`、`SetReq`、`IRequestSet` | hook 的显式 `req` 参数、`GetDefaultItemsWithRequest` | v0.0.248 | v0.1.0 | service/manage | futures、框架 Manage 扩展 | `service/manage/request_isolation_test.go` |
| `MenuManage.GetDefaultItems` | `GetDefaultItemsWithRequest` | v0.0.248 | v0.1.0 | server/api/manage | futures、菜单扩展 | `pkg/server/api/manage/menumanage_request_test.go` |
| `types.SetCrossNodeForwarder`、`GetCrossNodeForwarder` | `Set/GetCrossNodeForwarderForService` | v0.0.248 | v0.1.0 | server/types | 多服务进程、跨节点通知扩展 | `pkg/server/types/crossnode_test.go` |
| `router.TestResult` 直接变量 | `SetTestResult`、`GetTestResult` | v0.0.248 | v0.1.0 | server/router | OpenAPI/路由测试扩展 | `pkg/server/router/servicecontext_registry_test.go` |
| `config.INITSERVER` 直接并发读写 | `IsServerInitializing` 及受同步初始化入口 | v0.0.248 | v0.1.0 | server/config | 框架启动扩展 | config/router 并发测试 |
| `PrefixedBadgerDB.SetSyncDB` | `UseWriteBehind(WriteBehindTarget)`（不要迁到同样已废弃的 `EnableWriteBehind`） | v0.0.248 | v0.1.0 | persistence/nosql | 下游本地写回扩展 | `pkg/persistence/database/nosql/sharedbadger_writebehind_test.go` |
| `PrefixedBadgerDB.EnableWriteBehind(*entity.ModelList[T])` | `UseWriteBehind(WriteBehindTarget)` | v0.0.250 | v0.1.0 | persistence/nosql | 下游本地写回扩展 | `pkg/persistence/database/nosql/sharedbadger_writebehind_target_test.go` |
| `public.Callback`、`public.Casdoor` Go 类型 | `public.CasdoorCallback`、`public.CasdoorConfig` | v0.0.249 | v0.1.0 | server/api/public | Casdoor 登录前端、认证扩展 | `pkg/server/api/public/casdoorcallback_test.go` |
| HTTP `/api/callback` | `/api/casdoor?service=<name>` 返回的 `background_callback_url`，路径为 `/api/casdoor/callback` 并可附带目标服务 | v0.0.249 | v0.1.0 | server/api/public | Casdoor 登录前端 | `examples/integration/casdoor-auth-lifecycle/rest_test.go` |
| `RouteCacheL1Config.Limit` | `RouteCacheL1Config.MaxEntries` | v0.0.249 | v0.1.0 | server/routecache | 路由缓存配置消费方 | `pkg/server/config/routecache_test.go` |
| 示例 07 `models.RemoveLocalOrder` | `PrefixedBadgerDB` 根据 write-behind ACK 与 `IsSyncAfterDelete` 自动清理 | v0.0.250 | v0.1.0 | examples/07-shop-order-scale | 订单水平扩展示例扩展 | `examples/07-shop-order-scale/order-service/business/order_syncer_test.go` |
| 示例 04 `StartOrderWriteStore`、`StopOrderWriteStore`、全局查询/指标门面 | `OrderWriteRuntime` + `ServiceContext.UseResource` | v0.0.250 | v0.1.0 | examples/04-shop-performance | 示例扩展与基准 | `examples/04-shop-performance/models/order_write_store_test.go` |
| 示例 07 `StartOrderWriteStore`、`StopOrderWriteStore`、`AddOrder`、`UseOrderWriteBehind`、`SyncLocalOrders`、本地查询别名 | `transaction.OrderWriteRuntime` + 注入式 `OrderWriteAccess` | v0.0.250 | v0.1.0 | examples/07-shop-order-scale | 订单水平扩展示例扩展 | `examples/07-shop-order-scale/order-service/models/transaction/order_write_store_test.go` |
| `utils.StopMemoryMonitor` | 由资源 owner 管理指标和内存策略 | v0.0.251 | v0.1.0 | utils | 旧反射工具调用方 | `pkg/utils/lifecycle_test.go` |
| `types.RouterStats`、`RouterStatsSnapshot`、`ServiceContext.GetAllRouterStats`/`GetPublicRouterStats`/`GetPrivateRouterStats`/`PrintRouterStats`、`router.StatsManager` | Runtime Aggregator + `POST /api/servermanage/runtimetopology` / `runtimeservice`（Prometheus 历史源） | v0.0.252 | v0.1.0 | server/router、server/types | futures、旧 Admin Statistics 前端 | 规格与 runtime 测试；生产路径 `enableStats=false`；Statistics 保持不注册 |
| 未注册的 `public.Statistics` 类型与旧 `/api/servermanage/statistics` 前端约定 | `RuntimeTopology` / `RuntimeService` | v0.0.252 | v0.1.0 | server/api/public、web/admin | Admin MonitorSystem 旧 mock | `web/admin/src/services/runtime.ts` |
| `safe.Claims.GetToken`、`safe.ValidateJWTToken` | `IssueTokenPair`；校验用 `ValidateAccessToken` 并显式传入期望认证类型与当前时间 | v0.0.253 | v0.1.0 | server/safe | 旧 Token 签发与校验调用方 | `pkg/server/safe/jwt_test.go`、`pkg/server/safe/tokenissuer_test.go` |
| `RouterInfo` 导出字段 `ID`、`Path`、`Auth`、`Method`、`ServiceName`、`PackPath`、`PathType`、`StructName`、`InstanceName`、`PoolSize`、`InternalCallers`、`TempStore` | 注册后使用对应 `GetXxx`；内部调用方白名单用 `router.WithInternalCallers`；`TempStore` 无替代，请求级状态不得写入 `RouterInfo` | v0.0.253 | v0.1.0 | server/types | 路由注册与 OpenAPI 扩展 | `pkg/server/types/routerinfo_lifecycle_test.go`、`pkg/server/types/internalcaller_test.go` |
| `casdoor.AuthMiddleware`、`AuthHandler`、`NewAuthHandler`、`TokenParse` | `ServiceContext` 注册的 REST 认证链；解析用 `TokenParseWithClient` 并显式传入认证域 Client（旧签名已固定 fail closed） | v0.0.253 | v0.1.0 | server/safe/casdoor | 旧 Casdoor 中间件调用方 | `pkg/server/safe/casdoor/authmiddleware_test.go` |
| `types.WebSocketNotificationSystem` | `ServiceContext` 独占的 `RouteWebSocketHub`，经 `RouterInfo` 兼容方法访问；框架不再创建或运行旧通知池 | v0.0.253 | v0.1.0 | server/types | 旧 WebSocket 通知调用方 | `pkg/server/types/route_websocket_hub_test.go`、`pkg/server/types/websocket_worker_lifecycle_test.go` |
| 整包 `pkg/fileserver` | `pkg/server/run` 中 FiberServer / WebServer 直接注册的内嵌 `dist/` 文件系统 | v0.0.253 | v0.1.0 | fileserver | 历史静态文件调用方 | 仓库内调用已清零（除自身外无 import 命中） |
| 整包 `pkg/dec` | `pkg/server/event`：进程内用 `sc.EventStream.Subscribe`，跨节点用 `sc.NewEventPublisher(subject).Publish` | v0.0.253 | v0.1.0 | dec | 历史进程内事件总线调用方 | `pkg/server/event/*_test.go`；仓库内调用已清零 |
| 整包 `pkg/localization` | 专用 i18n 库（如 `golang.org/x/text`）与 locale 感知格式化器 | v0.0.253 | v0.1.0 | localization | 历史 i18n 调用方 | 仓库内调用已清零 |

删除条件：仓库内调用清零；futures 等已登记消费方迁移；CHANGELOG Removed 段完整；新旧版本 smoke 证据可复现。

自定义内部 Socket 表面依据 `socket-to-grpc-v1` 的 MAJOR 变更批准直接删除。Logto、静态服务依赖和顶层遗留配置依据 `logto-legacy-service-config-removal-v1` 直接删除；两者均不进入长期 Deprecated 状态。
