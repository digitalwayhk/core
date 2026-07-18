# 示例 06：可信内部调用的多服务商城

本示例把商城拆成 `shop-user`、`shop-supplier`、`shop-order` 三个服务，演示统一 Manage Hook、受限 Public、买家 Private、Redis 发现、gRPC/mTLS、可靠事件、缓存主动失效和本地永久投影。身份由 TestToken 模拟，重点是服务边界，不是第三方登录。

## 三类使用者与服务边界

| 使用者 | 入口 | 可以做什么 |
| --- | --- | --- |
| 普通用户 | `shop-user` | 注册后维护本人资料和地址；查询供应商、商品、支付类型；下单、撤单、支付、查询本人订单 |
| 供应商用户 | `shop-supplier` | 注册后维护本人供应商资料和商品、上下架商品、查询本人订单及状态 |
| 平台管理员 | User/Supplier/Order 的 Manage | 管理用户、供应商、商品和支付类型；查询及驱动订单、支付状态 |

三个服务各自拥有事实：

| 服务 | 权威数据 | 对外边界 |
| --- | --- | --- |
| `shop-user` | `User`、`Address` | 面向普通用户的唯一业务入口；公开 facade、买家 Private、User/Address Manage、唯一订单 WebSocket |
| `shop-supplier` | `Supplier`、`Product`、本地 `SupplierOrder` 投影 | Supplier/Product/Order Manage；仅供内部服务调用的供应商和商品 Public；没有 Private |
| `shop-order` | `Order`、`PaymentType`、`PaymentRecord`、Outbox | 管理员 Manage；仅允许 `shop-user` 调用的五个 Public；没有 Private、WebSocket，部署时不暴露 HTTP 端口 |

跨服务业务 ID 都是数字。`AuthUserID` 只保存在 User/Supplier 本地，用于把登录身份映射为数字 `UserID`/`SupplierID`；DTO、事件和服务间调用不传播认证字符串。

## 路由矩阵

| 服务 | 路由类型 | 能力 | 调用者 |
| --- | --- | --- | --- |
| User | Manage | User 查看/查询/编辑/禁用；Address 完整 CRUD | 本人由 Hook 自动限域；管理员可跨用户管理 |
| User | Public facade | `GetSuppliers`、`GetProducts`、`GetPaymentTypes` | 外部普通用户；内部再调用事实服务 |
| User | Private | `AddOrder`、`GetOrders`、`CancelOrder`、`CreatePayment` | 已认证且已建立本地 User 的普通用户 |
| Supplier | Manage | Supplier 查看/查询/编辑/删除/禁用；Product CRUD/上下架；Order 只读 | 供应商由 Hook 自动限域；管理员可跨供应商管理 |
| Supplier | 受限 Public | `GetSuppliers`、`GetProducts` | 前者只允许 `shop-user`；后者允许 `shop-user`、`shop-order` |
| Order | Manage | PaymentType CRUD/启停；Order、PaymentRecord 查询及受控状态命令 | 仅平台管理员 |
| Order | 受限 Public | `CreateOrder`、`CancelOrder`、`CreatePayment`、`GetOrders`、`GetPaymentTypes` | 仅 `shop-user` |

Manage 不拆成“自管理 API”和“平台管理 API”。同一个 `ManageService` 通过 `SearchBefore`、`DoBefore` 等 Hook，根据可信身份自动添加 owner 条件、冻结归属字段并校验操作权限。供应商被禁用后仍可查看，但不能修改；只有管理员能禁用或重新启用供应商。商品可由所属供应商或管理员上下架。

已被订单引用的供应商和商品不能删除。删除 Hook 只查询 Supplier 服务本地、永久保存的 `SupplierOrder`，不在删除事务中同步调用 Order 服务。供应商订单 Manage 只有 View/Search，供应商只能看到自己的投影，管理员可以查看全部。

## 受限 Public 与可信调用方

Public 表示路由使用公共序列化契约，不等于允许互联网直接访问。内部专用 Public 必须声明调用方白名单：

```go
func (g *GetProducts) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(g,
		router.WithServiceName("shop-supplier"),
		router.WithPath("/api/shop-supplier/getproducts"),
		router.WithInternalCallers("shop-user", "shop-order"),
	)
}
```

校验发生在 `Parse`、`Validation`、`Do` 之前：

- 同进程调用的可信身份来自发起调用的 Source `ServiceContext`。
- 跨进程调用的 `SourceService` 只是声明；只有客户端证书的已验证 mTLS SAN 与该声明一致时，框架才注入可信身份。
- 普通 HTTP 请求没有内部服务身份，即使知道 URL 或伪造 Header 也会被拒绝。
- `insecure` 只用于 all-in-one 本地调试；远程受限路由必须使用 mTLS，或由独立实现提供等价且可验证的 mesh 身份。

调用方直接构造事实服务注册的 Public API，再使用 `req.CallService`；代码中不建立保存地址、连接或重试状态的 client，也不设置第二套 `api/call` 目录。

## 下单、撤单和支付

买家调用 `AddOrder` 时必须提供 `requestID`。User 服务把它规范化为 `{UserID}:{requestID}` 后传给 Order；Order 保存请求指纹并用唯一约束收敛并发重试：相同请求返回同一订单，不同请求内容复用同一 key 会失败。

Order 创建时同步读取已启用商品和供应商，随后在同一事务中保存：

- 数字 User/Supplier/Product/Address ID；
- 供应商、商品、单价、数量和收货地址快照；
- 初始订单状态与 `OrderRevision`；
- 完整快照 `OrderCreated` Outbox 事件。

撤单不删除订单事实。未支付订单进入取消状态；已支付订单进入退款流程。支付尝试使用稳定字符串 `PaymentID`，重复处理中请求不会创建第二条流水。订单、支付变更分别发布 `OrderStatusChanged`、`PaymentChanged`；支付类型变更发布 `PaymentTypeChanged`。

## 可靠事件与永久投影

1. 业务事实和 Outbox 在同一 SQLite 事务中提交。
2. Worker 只在 `ServiceEventBridge.Publish` 成功后标记事件已发布。
3. Redis Streams 以逻辑服务名作为消费组：同服务实例竞争，不同服务各消费一份。
4. Handler 成功后才 ACK；失败消息留在 pending，供存活消费者认领。
5. User/Supplier 先以 `EventID` 写 Inbox，再执行副作用，重复投递不会重复处理。
6. Supplier 按 `OrderID` 幂等 upsert `SupplierOrder`，并保留最新 `OrderRevision`；该永久投影同时服务供应商订单查询和删除保护。
7. User 消费订单事件后，只失效对应数字 `UserID` 的订单缓存，并通知该用户的 WebSocket。

## 缓存

| 读路径 | TTL | 主动失效 |
| --- | ---: | --- |
| Supplier 内部供应商/商品 Public | 30 秒 | Supplier/Product 事务提交后本地失效 |
| User 供应商/商品 facade | 30 秒 | `SupplierChanged`、`ProductChanged` |
| Order 支付类型 Public | 30 秒 | PaymentType 事务提交后本地失效 |
| User 支付类型 facade | 30 秒 | `PaymentTypeChanged` |
| User 本人订单 Private | 10 秒 | 对应用户的 Order/Payment 事件 |

缓存键包含全部查询条件；认证读路径的身份只能来自 Token 映射后的数字 ID。缓存只是可重建副本，Redis 不保存业务权威模型。

## 目录

```text
contract                         # 稳定服务名、事件名和错误
dto/{user,supplier,order,event}  # 跨服务 JSON 契约，不引用持久化 Model
user-service                     # User/Address Manage、外部 facade、买家 Private
supplier-service                 # Supplier/Product/Order Manage、内部 Public、永久投影
order-service                    # 内部 Public、管理员 Manage、订单/支付事实与 Outbox
main/{all-in-one,user,supplier,order}
deploy                           # 三进程 Docker Compose 与证书挂载说明
```

## 运行模式

先启动 Redis：

```bash
docker compose -f docker-compose.integration.yml up -d redis
```

同进程调试：

```bash
SHOP_REDIS_ADDR=127.0.0.1:6379 \
go run ./examples/06-shop-microservices/main/all-in-one -p 18080 -grpc 38080 -view 0
```

all-in-one 使用本地 Resolver 和 `insecure` gRPC，只用于开发。可信调用方仍由 Source `ServiceContext` 注入，不允许用 HTTP 假冒。

三进程部署：

```bash
docker compose -f examples/06-shop-microservices/deploy/docker-compose.yml up --build
```

| 进程 | 实际业务 HTTP | 内部 gRPC | 宿主机暴露 |
| --- | ---: | ---: | --- |
| User | 18081 | 28081 | 18081 |
| Supplier | 18082 | 28082 | 18082 |
| Order | 18083 | 28083 | 不暴露 |

Compose 通过 Redis 发现服务，并把 `deploy/certs` 只读挂载到 `/run/secrets/shop-grpc`。三个证书必须由同一 CA 签发，允许客户端和服务端认证，并分别包含 `shop-user`、`shop-supplier`、`shop-order` DNS SAN。仓库不提交证书或私钥。

## 验收

```bash
# 模型、业务、路由、Outbox/Inbox
go test -race ./examples/06-shop-microservices/... -count=1

# 同进程真实 HTTP、Manage、Private、WebSocket 与服务间调用
go test -race ./examples/integration/06-shop-microservices -count=1 -timeout=15m

# 三进程 Redis 发现、mTLS、gRPC 传输计数及错误证书拒绝
go test -race ./examples/integration/06-shop-microservices-three-process -count=1 -timeout=15m

# 部署结构；输出中 order 不得有 ports
docker compose -f examples/06-shop-microservices/deploy/docker-compose.yml config
```

同进程和三进程测试都经过真实 `ServiceContext`/`ServiceResolver`。三进程测试还断言 User→Order、Order→Supplier 的 gRPC 计数增长、HTTP 计数为零，并拒绝错误 PKI。
