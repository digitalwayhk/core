# 示例 07：订单服务水平扩展

本示例演示商城订单量增长后，`shop-order` 通过多实例水平扩展、本地可靠写、异步同步远程 order 权威库和标准 EventBridge 事件保持吞吐与一致性。

最终数据库按业务域拆分，不按技术实例拆分。多个 order 实例共享同一个远程 order 权威库；每个实例拥有自己的本地 pending store，用于可靠接收、崩溃恢复、批量同步和故障重试。

## 目标

- 复用 06 的 `shop-user`、`shop-supplier`、`shop-order` 三服务边界。
- 重点扩展 `shop-order`：同一逻辑服务启动多个副本，普通用户下单可被路由到任意副本。
- `shop-order` 的管理配置只有一份权威数据，例如支付类型、订单规则和最小下单数量。
- 下单事实先写入当前副本本地可靠 pending store，再异步同步到共享远程 order 权威库。
- 所有业务事实、pending、Outbox、Inbox 和投影记录 `TraceID`、`ServiceName`、`ServiceInstanceID`，便于跨服务追踪。
- 示例必须验证 `AutoMachineID=true`，扩容后自动分配唯一 MachineID，不依赖人工配置固定编号。

## 服务边界

| 服务 | 对外能力 | 内部能力 | 水平扩展要求 |
| --- | --- | --- | --- |
| `shop-user` | 普通用户注册、资料、地址、下单入口、订单查询、WebSocket 订单订阅 | 调用 supplier/order 的受限 Public API | 入口服务可扩展，但缓存只放在入口 facade |
| `shop-supplier` | 供应商后台资料、产品上下架、供应商订单查询 | 提供供应商和商品受限 Public API，消费订单事件生成本地引用 | 保持供应商域权威库独立 |
| `shop-order` | 不暴露外部端口，只面向管理员 Manage | 提供下单、撤单、支付、支付类型、订单查询等受限 Public API | 多副本共享远程 order 权威库，每个副本独立本地 pending |

内部专用 Public API 必须使用 `WithInternalCallers`，调用方身份只能来自可信 ServiceContext 或 mTLS SAN，不能相信 HTTP Header 或请求体自报。

## 数据与一致性

```text
buyer request
  -> shop-user private facade
  -> shop-order public API（resolver 选择任意 order 副本）
  -> order 副本本地 pending store 持久成功后返回
  -> order 副本异步批量同步共享远程 order 权威库
  -> order 权威事实写入 Outbox
  -> EventBridge 投递 OrderCreated/OrderStatusChanged/PaymentChanged
  -> user/supplier 消费事件更新本地投影和缓存失效
```

本地 pending 是可靠写入队列，不是缓存。服务重启、扩容、缩容或同步失败时，pending 必须可恢复、可重试、可观测。

## 管理配置同步

`shop-order` 管理 API 只维护一份共享业务配置，包括：

- 支付类型。
- 订单规则。
- 最小下单数量。
- 是否允许撤单或支付的状态规则。

任意 order 副本处理下单时，都必须读取同一份规则权威数据。配置变更后通过标准 EventBridge 发布控制事件，入口 facade 只在 `shop-user` 缓存并主动失效。

## 自动水平扩展约束

- 示例必须启用 `AutoMachineID=true`，不能为 order 副本硬编码 `MachineID=1/2/3`。
- 框架需要通过当前 ClusterProvider 申请 MachineID lease，并在 Snowflake 初始化前完成绑定。
- 每个副本必须拥有唯一 `ServiceInstanceID`，并记录到业务事实、pending、Outbox、Inbox、同步状态和诊断日志。
- Docker 扩容时不固定暴露多个 order 业务端口；副本通过配置的发现机制注册，调用方只通过 ServiceResolver 选择实例。
- 注册发现机制不应绑定 Redis；Redis、局域网发现或其他中间件由配置决定，示例只验证框架抽象可用。

## 缓存规则

缓存只放在面向外部流量的入口服务 facade：

- `shop-user` 查询供应商和产品可缓存，并通过 Supplier/Product 事件主动失效。
- `shop-user` 查询支付类型可缓存，并通过 PaymentType 事件主动失效。
- `shop-user` 查询本人订单可短 TTL 缓存，并通过订单/支付事件按 UserID 失效。
- `shop-supplier` 和 `shop-order` 作为内部权威服务，其受限 Public API 不重复 `UseCache`。

## 测试要求

- 单进程集成测试验证 07 的业务语义、规则配置、pending 同步和事件投递。
- 多进程 UAT 必须按角色拆分：买家、供应商、管理员分别拥有可单独运行的闭环测试。
- 多 order 副本 UAT 必须验证自动 MachineID、唯一 ServiceInstanceID、本地 pending 目录隔离、共享远程权威库和 resolver 多节点调用。
- 有 WebSocket 的买家角色必须覆盖真实订阅、订单事件投递和其他买家隔离。
- 07 完成后增加基准性能测试，对比 06 与 07 在订单写入吞吐、延迟分位数和失败恢复上的差异。

## 质量门禁

- 禁止新增 `api/call`。
- 禁止内部权威 Public API 重复入口 facade 缓存。
- 禁止发布方关心消费者；事件发布只通过 `sc.UseOutbox(models.OutboxStore{})`，订阅只通过 `sc.SubscribeEvent(...)`。
- 禁止固定水平副本 MachineID；必须使用 `AutoMachineID=true` 并测试自动分配。
- 禁止把最终业务库按技术副本拆分；只允许按业务域拆库。
