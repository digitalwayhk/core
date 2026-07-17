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
- 远程同步调用使用 gRPC；三进程配置启用应用层 mTLS，不经最终用户 WebSocket。
- 写请求只发送一次；User 生成 IdempotencyKey，Order 用唯一约束返回同一订单。
- 无健康节点或 Redis 不可用时 fail closed，不回退 `AttachServices`。

## 可靠事件

1. 业务事实与 Outbox 在同一 SQLite 事务中写入。
2. Outbox worker 只在 `ServiceEventBridge.Publish` 成功后标记已发布。
3. Redis Streams 消费组使用逻辑服务名：同服务多实例竞争，不同服务各收一份。
4. `ControlHandler` 成功后才 ACK；失败消息留在 pending，由同组存活消费者超时认领。
5. 消费方以 EventID 写 Inbox，重复投递不重复执行缓存失效或 WebSocket 通知。
6. User/Supplier 的外部控制主题必须全部订阅成功；任一主题失败会撤销本轮已建立订阅、记录 `service_external_control_subscribe_failed` 并终止服务，禁止部分启用。

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
go run ./examples/06-shop-microservices/main/all-in-one -p 18080 -grpc 38080 -view 0
```

`all-in-one` 使用 local provider 和 `insecure` gRPC，Redis 仅承载 EventBridge。它只用于本地断点和快速阅读，不提供故障隔离、独立扩容或进程级资源隔离。显式 `-grpc 38080` 为框架 `server` 与三个业务 ServiceContext 分配 `38080..38083`，避免与常见的本机 `18080` 端口冲突。

三进程部署演示：

```bash
docker compose -f examples/06-shop-microservices/deploy/docker-compose.yml up --build
```

Compose 只映射 User `18081` 和 Supplier `18082` HTTP 端口。Order HTTP 和所有 gRPC 端口只在 Docker 网络中可见。Redis 发现使用 `core:discovery:*`，事件使用 `core:event:*`。启动前必须通过部署密钥系统向 `deploy/certs` 注入 CA 和三个服务身份；Compose 仅以只读方式挂载该目录，仓库不包含证书或私钥。

端口由框架按“命令行 `-p` 基准端口 + DataCenterID - 1”解析，示例固定映射如下，部署时必须成组修改，不能只改 Compose 的 `ports`：

| 进程 | `-p` 基准端口 | DataCenterID | 实际业务 HTTP | 内部 gRPC | 宿主机暴露 |
| --- | ---: | ---: | ---: | ---: | --- |
| User | 18080 | 2 | 18081 | 28081 | 18081 |
| Supplier | 18081 | 2 | 18082 | 28082 | 18082 |
| Order | 18082 | 2 | 18083 | 28083 | 不暴露 |

同进程入口为了让三个 ServiceContext 的 MachineID 空间保持独立，使用 DataCenterID `2/3/4`，业务 HTTP 仍固定为 `18081/18082/18083`。生产配置应从统一配置源生成这些参数，避免手工分别维护命令、DataCenterID 和端口映射。

## gRPC 生产安全

- 应用层 mTLS：`Transport.GRPC.Security.Mode=mtls`，由密钥系统注入 CA、服务证书和私钥。Compose 展示的是这种模式。
- 服务身份：`Transport.GRPC.Security.ServerName={service}` 使客户端按每次调用的逻辑目标服务名校验证书；三进程证书分别必须包含 `shop-user`、`shop-supplier`、`shop-order` SAN，不得用共享 `localhost` 身份代替服务身份。
- 服务网格：`Transport.GRPC.Security.Mode=mesh`，应用不读取证书文件，mTLS 身份、加密和证书轮换由 sidecar 与网格控制面负责。
- 即使 gRPC 只暴露在私网，`insecure` 也不是生产安全方案；它只用于 all-in-one 本地调试。

三进程配置的 `Transport.Internal` 固定为 `grpc`，`Transport.Fallback` 为空。本机管理路由 `/api/servermanage/transportstats` 返回请求所属 ServiceContext 的结构化传输计数；默认未授权请求仅允许 loopback，RFC1918 私网地址也会被拒绝。集成测试比较 UAT 前后快照，不解析日志。

## 验收

```bash
# 模型、业务、路由和 Outbox/Inbox
go test -race ./examples/06-shop-microservices/... -count=1

# 同进程真实 HTTP/WebSocket/Redis
SHOP_REDIS_ADDR=127.0.0.1:6379 \
go test -race ./examples/integration/06-shop-microservices -count=1 -timeout=15m

# 最终用户活动 UAT：商品快照、订单归属、支付与供应商视图
SHOP_REDIS_ADDR=127.0.0.1:6379 \
go test -race ./examples/integration/06-shop-microservices \
  -run TestUATBuyerOrderLifecycle -count=1 -timeout=15m

# 三个独立 race 进程、Redis 发现、mTLS gRPC 和真实传输计数
SHOP_REDIS_ADDR=127.0.0.1:6379 \
go test -race ./examples/integration/06-shop-microservices-three-process -count=1 -timeout=15m
```

集成测试不写 `AttachServices`，因此绿灯可以直接证明新 Resolver 链路已生效。

UAT 聚焦用户活动产生的业务事实，不替代 API 矩阵测试：它会在下单后修改商品价格，确认订单仍保留下单价格快照；随后验证支付确认和已支付订单撤销能通过 EventBridge 主动失效缓存，并在买家与供应商视图中收敛到同一状态，同时确保其他用户看不到该订单。
