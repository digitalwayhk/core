# 示例

七个示例构成从简到繁的递进阶梯，每个都是可独立运行、独立测试的完整应用，而不是片段演示：

1. [01-simple-shop](./01-simple-shop)：模型、Manage CRUD、Public/Private API、JWT 鉴权、SQLite 持久化和订单 WebSocket 的最小闭环。
2. [02-shop-payment](./02-shop-payment)：API、business、models 分层，跨模型事务，支付结果滞后的状态机，Manage 自定义命令。
3. [03-shop-inheritance](./03-shop-inheritance)：供应商业务，模型继承与 Manage 继承，通用启停，只读子表。
4. [04-shop-performance](./04-shop-performance)：查询分层缓存、下单事实缓存、Group Commit 可靠写与写后同步。
5. [05-shop-casdoor-rbac](./05-shop-casdoor-rbac)：Casdoor 登录，Auth / Manage 双认证域隔离，权限矩阵。
6. [06-shop-microservices](./06-shop-microservices)：拆成 `shop-user`、`shop-supplier`、`shop-order` 三服务，Redis 发现，gRPC/mTLS，受限内部 Public，可靠事件，本地永久投影。
7. [07-shop-order-scale](./07-shop-order-scale)：订单服务多副本水平扩展，AutoMachineID，共享远程权威库，Outbox，服务报表。

后一级在前一级的业务契约上叠加能力，覆盖同一个商城业务域，因此可以直接对比「多一项能力要多写多少代码」。选择与当前阶段匹配的示例作为模板。

集成测试通用能力位于 `integration` 根目录。每个示例在 `integration/<示例名>` 下保留 Manage、Public、Private/WebSocket 的真实进程测试。

运行全部示例测试：

```bash
go test ./examples/... -count=1
```
