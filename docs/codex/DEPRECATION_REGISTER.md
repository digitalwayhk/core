# 废弃 API 登记

最早删除版本是下限，不是自动删除指令；删除前仍需 public-api 门禁、迁移说明和消费方验证。

| API | 替代入口 | 首次登记版本 | 最早删除版本 | Owner | 消费方 | 迁移证据 |
| --- | --- | --- | --- | --- | --- | --- |
| `ManageService.Req`、`SetReq`、`IRequestSet` | hook 的显式 `req` 参数、`GetDefaultItemsWithRequest` | v0.0.248 | v0.1.0 | service/manage | futures、框架 Manage 扩展 | `service/manage/request_isolation_test.go` |
| `MenuManage.GetDefaultItems` | `GetDefaultItemsWithRequest` | v0.0.248 | v0.1.0 | server/api/manage | futures、菜单扩展 | `pkg/server/api/manage/menumanage_request_test.go` |
| `types.SetCrossNodeForwarder`、`GetCrossNodeForwarder` | `Set/GetCrossNodeForwarderForService` | v0.0.248 | v0.1.0 | server/types | 多服务进程、跨节点通知扩展 | `pkg/server/types/crossnode_test.go` |
| `router.TestResult` 直接变量 | `SetTestResult`、`GetTestResult` | v0.0.248 | v0.1.0 | server/router | OpenAPI/路由测试扩展 | `pkg/server/router/servicecontext_registry_test.go` |
| `config.INITSERVER` 直接并发读写 | `IsServerInitializing` 及受同步初始化入口 | v0.0.248 | v0.1.0 | server/config | 框架启动扩展 | config/router 并发测试 |
| `PrefixedBadgerDB.SetSyncDB` | `EnableWriteBehind` | v0.0.248 | v0.1.0 | persistence/nosql | 下游本地写回扩展 | `pkg/persistence/database/nosql/sharedbadger_writebehind_test.go` |
| `public.Callback`、`public.Casdoor` Go 类型 | `public.CasdoorCallback`、`public.CasdoorConfig` | v0.0.249 | v0.1.0 | server/api/public | Casdoor 登录前端、认证扩展 | `pkg/server/api/public/casdoorcallback_test.go` |
| HTTP `/api/callback` | `/api/casdoor` 返回的 `background_callback_url`，当前为 `/api/casdoor/callback` | v0.0.249 | v0.1.0 | server/api/public | Casdoor 登录前端 | `examples/integration/casdoor-auth-lifecycle/rest_test.go` |
| `RouteCacheL1Config.Limit` | `RouteCacheL1Config.MaxEntries` | v0.0.249 | v0.1.0 | server/routecache | 路由缓存配置消费方 | `pkg/server/config/routecache_test.go` |
| `ServerConfig.AttachServices`、`SetAttachService`、动态设置服务地址 API | `ClusterProvider` + `ServiceResolver` | v0.0.250 | v0.1.0 | server/router、server/config | 多服务调用方、旧静态部署 | `pkg/server/router/serviceresolver_test.go` |
| 示例 07 `models.RemoveLocalOrder` | `PrefixedBadgerDB` 根据 write-behind ACK 与 `IsSyncAfterDelete` 自动清理 | v0.0.250 | v0.1.0 | examples/07-shop-order-scale | 订单水平扩展示例扩展 | `examples/07-shop-order-scale/order-service/business/order_syncer_test.go` |
| 示例 04 `StartOrderWriteStore`、`StopOrderWriteStore`、全局查询/指标门面 | `OrderWriteRuntime` + `ServiceContext.UseResource` | v0.0.250 | v0.1.0 | examples/04-shop-performance | 示例扩展与基准 | `examples/04-shop-performance/models/order_write_store_test.go` |
| 示例 07 `StartOrderWriteStore`、`StopOrderWriteStore`、`AddOrder`、`UseOrderWriteBehind`、`SyncLocalOrders`、本地查询别名 | `transaction.OrderWriteRuntime` + 注入式 `OrderWriteAccess` | v0.0.250 | v0.1.0 | examples/07-shop-order-scale | 订单水平扩展示例扩展 | `examples/07-shop-order-scale/order-service/models/transaction/order_write_store_test.go` |
| `utils.StopMemoryMonitor` | 由资源 owner 管理指标和内存策略 | v0.0.251 | v0.1.0 | utils | 旧反射工具调用方 | `pkg/utils/lifecycle_test.go` |

删除条件：仓库内调用清零；futures 等已登记消费方迁移；CHANGELOG Removed 段完整；新旧版本 smoke 证据可复现。

自定义内部 Socket 表面不在本表登记长期废弃：它依据 `socket-to-grpc-v1` 的 MAJOR 变更批准直接删除，迁移见 `GRPC_TRANSPORT_MIGRATION.md`。这不改变本表其他条目的保留窗口。
