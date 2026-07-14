# 示例

本目录提供三个可以独立运行和测试的完整示例：

1. [01-simple-shop](./01-simple-shop)：模型、Manage CRUD、Public/Private API、认证和订单 WebSocket 的最小闭环。
2. [02-shop-payment](./02-shop-payment)：API、business、models 分层，跨模型事务、支付状态机和 Manage 自定义命令。
3. [03-shop-inheritance](./03-shop-inheritance)：供应商业务、模型继承、Manage 继承、通用启停和只读子表。

集成测试通用能力位于 `integration` 根目录。每个示例在 `integration/<示例名>` 下保留 Manage、Public、Private/WebSocket 的真实进程测试。

运行全部示例测试：

```bash
go test ./examples/... -count=1
```
