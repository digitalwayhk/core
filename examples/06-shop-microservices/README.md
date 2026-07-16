# 示例 06：Redis 多服务商城

本示例把单体商城拆分为 User、Supplier、Order 三个独立服务，用一个 Redis 同时提供服务发现和 Redis Streams EventBridge。身份使用框架 TestToken，不引入 Casdoor，便于聚焦服务边界、调用和协同。

## 服务边界

| 服务 | 权威数据 | 外部职责 |
| --- | --- | --- |
| `shop-user` | 用户、本人地址 | 买家唯一入口，组装商品、订单和支付 facade |
| `shop-supplier` | 供应商、商品 | 供应商管理本人商品，查询本人商品订单 |
| `shop-order` | 订单、支付类型、支付流水、Outbox | 保存下单快照，执行幂等下单和支付状态机 |

User/Supplier 不复制订单权威副本；查询始终同步调用 Order Service。Order 下单时同步调用 Supplier Service，并冻结商品、供应商、价格和收货地址快照。

## 目录与依赖

```text
contract                 # 无业务依赖的服务名、事件名和稳定错误
dto/{user,supplier,order,event}
                         # 跨服务共用 JSON 契约，不引用 Model
user-service             # API -> models，facade 不保存订单副本
supplier-service         # API -> business -> models
order-service            # API -> business -> models，事务内同写 Outbox
runtime                  # 无业务模型的通用 Outbox worker
main/{all-in-one,user,supplier,order}
deploy                   # 三进程 Docker Compose
```

`supplier-service/api/call` 中的类型是 Supplier Service 真实注册的目标 API，只为解决 Go 包依赖分层，不是另一套 client。它不保存地址、连接、重试或序列化逻辑。调用方直接构造该 API，`req.CallService` 根据 `router.WithServiceName` 声明的稳定服务名调用 ServiceResolver。

## 同步调用

```go
response, err := req.CallService(&orderapi.CreateOrder{
    ProductID: productID,
    Quantity: quantity,
    IdempotencyKey: key,
    Address: snapshot,
})
```

- Resolver 先查同进程 ServiceContext，未命中再查 Redis ClusterProvider。
- 远程调用使用受控私网 socket，不经最终用户 WebSocket。
- 写请求只发送一次；User 生成 IdempotencyKey，Order 用唯一约束返回同一订单。
- 无健康节点或 Redis 不可用时 fail closed，不回退 `AttachServices`。

## 可靠事件

1. 业务事实与 Outbox 在同一 SQLite 事务中写入。
2. Outbox worker 只在 `ServiceEventBridge.Publish` 成功后标记已发布。
3. Redis Streams 消费组使用逻辑服务名：同服务多实例竞争，不同服务各收一份。
4. `ControlHandler` 成功后才 ACK；失败消息留在 pending，由同组存活消费者超时认领。
5. 消费方以 EventID 写 Inbox，重复投递不重复执行缓存失效或 WebSocket 通知。

WebSocket 只面向最终买家和供应商。User 按 Token UID 过滤，Supplier 按 Token 映射的 SupplierID 过滤；未在线用户不积压观察通知。

## 缓存与本地存储

- User 的商品 facade 使用 30 秒路由缓存，缓存键包含全部查询条件；`ProductChanged` 和 `SupplierChanged` 都通过 EventBridge 立即清理全部商品查询缓存。
- User 和 Supplier 的订单查询使用 10 秒路由缓存，缓存键只来自 Token 解析后的可信身份；`OrderChanged` 和 `PaymentChanged` 到达后清理订单缓存并通知对应 WebSocket 会话。
- 没有可靠失效事件的支付类型查询不启用缓存，避免为了命中率引入不可解释的陈旧窗口。
- 每个进程在创建 WebServer 前初始化自己拥有的 SQLite 表。初始化后保存一个稳定的 SQLite 克隆模板，请求、事务与 Outbox worker 均从模板创建独立适配器，避免与 Manage 使用的全局适配器共享事务状态或可变路径字段。

这些策略只优化 facade 读路径，不改变 User/Supplier 同步读取事实服务的权威边界；Redis 也不保存业务权威模型。

## 启动

先启动 Redis：

```bash
docker compose -f docker-compose.integration.yml up -d redis
```

同进程调试：

```bash
SHOP_REDIS_ADDR=127.0.0.1:6379 \
go run ./examples/06-shop-microservices/main/all-in-one -p 18080 -view 0
```

`all-in-one` 只用于本地断点和快速阅读，不提供故障隔离、独立扩容或进程级资源隔离。

三进程部署演示：

```bash
docker compose -f examples/06-shop-microservices/deploy/docker-compose.yml up --build
```

Compose 只映射 User `18081` 和 Supplier `18082` HTTP 端口。Order HTTP 和所有 socket 只在 Docker 私网中可见。Redis 发现使用 `core:discovery:*`，事件使用 `core:event:*`。

## 验收

```bash
# 模型、业务、路由和 Outbox/Inbox
go test -race ./examples/06-shop-microservices/... -count=1

# 同进程真实 HTTP/WebSocket/Redis
SHOP_REDIS_ADDR=127.0.0.1:6379 \
go test ./examples/integration/06-shop-microservices -count=1 -timeout=15m

# 三个独立 race 进程、Redis 发现和远程 socket
SHOP_REDIS_ADDR=127.0.0.1:6379 \
go test ./examples/integration/06-shop-microservices-three-process -count=1 -timeout=15m
```

集成测试不写 `AttachServices`，因此绿灯可以直接证明新 Resolver 链路已生效。
