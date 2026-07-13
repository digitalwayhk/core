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

删除条件：仓库内调用清零；futures 等已登记消费方迁移；CHANGELOG Removed 段完整；新旧版本 smoke 证据可复现。
