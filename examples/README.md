# 示例

本目录只保留一个完整示例：[01-simple-shop](./01-simple-shop)。它通过商品与订单业务演示：

- `entity.Model`、Manage `ModelList` 与模型封装的 SQLite 持久化方法；
- Manage 商品 CRUD 与只读订单管理；
- public 商品筛选与 private 用户订单；
- 框架内建 TestToken；
- 面向最终用户的 WebSocket 登录、订阅和隔离推送；
- 真实 HTTP、WebSocket 和 SQLite 集成测试。

集成测试通用能力位于 `integration` 根目录，商城的 Manage、Public、Private/WebSocket 测试位于 `integration/01-simple-shop`。

运行全部示例测试：

```bash
go test ./examples/... -count=1
```
