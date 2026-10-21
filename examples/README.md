# 示例

十一个示例构成能力阶梯，每个都是可独立运行、独立测试的完整应用，而不是片段演示：

1. [01-simple-shop](./01-simple-shop)：模型、Manage CRUD、Public/Private API、JWT 鉴权、SQLite 持久化和订单 WebSocket 的最小闭环。
2. [02-shop-payment](./02-shop-payment)：API、business、models 分层，跨模型事务，支付结果滞后的状态机，Manage 自定义命令。
3. [03-shop-inheritance](./03-shop-inheritance)：供应商业务，模型继承与 Manage 继承，通用启停，只读子表。
4. [04-shop-performance](./04-shop-performance)：查询分层缓存、下单事实缓存、Group Commit 可靠写与写后同步。
5. [05-shop-casdoor-rbac](./05-shop-casdoor-rbac)：Casdoor 登录，Auth / Manage 双认证域隔离，权限矩阵。
6. [06-shop-microservices](./06-shop-microservices)：拆成 `shop-user`、`shop-supplier`、`shop-order` 三服务，Redis 发现，gRPC/mTLS，受限内部 Public，可靠事件，本地永久投影。
7. [07-shop-order-scale](./07-shop-order-scale)：订单服务多副本水平扩展，AutoMachineID，共享远程权威库，Outbox，服务报表。
8. [08-admin-manage-ui](./08-admin-manage-ui)：Manage View 配置与 `web/admin` 管理界面全能力对照，覆盖外键、子表、字段控件、导入导出和自定义命令。

9. [09-admin-manage-rbac](./09-admin-manage-rbac)：复用 08 业务，演示 Core 管理员、角色绑定和命令权限，真实 HTTP 403。
10. [10-reliable-multi-instance](./10-reliable-multi-instance)：一个发布服务、两个同逻辑组消费副本，验证可靠事件跨实例分键消费、同 key 顺序/失败屏障及真实 HTTP 用户隔离。

11. [11-scoped-event-ownership](./11-scoped-event-ownership)：两个处理副本与两个Gateway，多主题持续归属、真实MySQL fence、Ready查询及WS原会话免重订恢复。

01–07 递进覆盖商城业务域；08–09 集中演示管理能力；10 集中演示单主题消息调度（无业务数据库或持久Inbox）；11增加应用事务fence与恢复。 按当前能力选择模板。

集成测试通用能力位于 `integration` 根目录。每个示例在 `integration/<示例名>` 下保留 Manage、Public、Private/WebSocket 的真实进程测试。

运行全部示例测试：

```bash
go test ./examples/... -count=1
```

第10例需显式提供独占 Redis，未配置时跳过外部验证：

```bash
CORE_TEST_REDIS_ADDR=<专属Redis地址> go test -p 1 -race ./examples/integration/10-reliable-multi-instance -count=1 -v
```
