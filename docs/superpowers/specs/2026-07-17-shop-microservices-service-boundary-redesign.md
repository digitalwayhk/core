# 示例 06：多服务商城边界重构设计

## 1. 文档状态

- 状态：设计已确认，等待用户审阅书面规格
- 日期：2026-07-17
- 示例目录：`examples/06-shop-microservices`
- 集成测试目录：`examples/integration/06-shop-microservices`、`examples/integration/06-shop-microservices-three-process`
- 被修订设计：`docs/superpowers/specs/2026-07-16-shop-microservices-example-design.md`

本文重新定义示例 06 的 User、Supplier、Order 服务边界、路由类型、身份归属、可靠事件和内部调用安全。既有 Redis Discovery、ServiceResolver、gRPC/mTLS、Outbox/Inbox 和三进程部署能力继续保留；与本文冲突的旧服务职责、API 目录和业务流程以本文为准。

## 2. 目标

1. 让三个服务分别面向普通用户、供应商和平台管理员，目录与路由直接表达真实使用者。
2. 使用统一 Manage 与 Hook 完成本人/管理员数据隔离、禁用后的只读策略、字段保护和删除约束，不复制两套管理 API。
3. 删除 Supplier 的 Private API 和 `api/call`，跨服务调用直接使用目标服务真实注册的 Public Router。
4. 让 Order 成为不对外暴露的订单与支付事实服务；其 Public Router 只能由获准内部服务调用。
5. 使用可靠事件在 Supplier 建立永久只读订单投影，同时驱动 User 缓存失效与最终用户 WebSocket。
6. 分离认证 UID 与数字业务主键，禁止认证标识进入跨服务外键、DTO、缓存键和事件。
7. 把新增的内部调用方约束、三服务模式和验收契约并入 Core 文档与 `use-digitalway-core` 技能。

## 3. 非目标

- 不引入 Casdoor、Logto、Kafka、NATS 或新的数据库。
- 不让 User 或 Supplier 成为订单事实源。
- 不让 Supplier 同步调用 Order 查询订单或删除引用。
- 不为平台管理员和业务主体拆分重复的 Manage Router。
- 不让 WebSocket 承担服务间通信。
- 不通过信任请求中的 `SourceService` 字符串实现内部授权。
- 不将 Order HTTP 端口映射到宿主机或公网。

## 4. 已确认决策

| 项目 | 决策 |
| --- | --- |
| Supplier 路由 | Manage + Public，无 Private、无 `api/call` |
| User 路由 | Manage + Public facade + Private |
| Order 路由 | Manage + 内部限定 Public，无 Private |
| Manage 权限 | 一套 Router，通过 Search/Do/CRUD Hook 区分本人和管理员 |
| 新 Supplier/User | TestToken 首次登录幂等创建，默认启用 |
| 禁用主体 | 本人后台只读；管理员可恢复；业务写操作 fail closed |
| 业务主键 | User.ID、Supplier.ID 使用数字 ID；认证 UID 仅作 AuthUserID 映射 |
| 商品初始状态 | 新增默认下架，必须显式上架 |
| User 删除 | 永不物理删除，只能禁用 |
| Supplier 删除 | 仅管理员可删；有商品或订单引用时禁止 |
| Product 删除 | 所属 Supplier 或管理员可删；被订单使用后禁止 |
| Address 删除 | 本人或管理员可删；历史订单依赖地址快照 |
| Supplier 订单 | Order 可靠事件驱动永久只读本地投影 |
| 撤单 | Order 保留记录并进入 Cancelled，不物理删除 |
| 下单幂等 | 客户端提供 requestID，UserID + requestID 形成稳定幂等键 |
| 实时通知 | 只由 User Service 面向普通用户提供订单 WebSocket |
| Order 部署 | HTTP 不对外映射；Manage 通过内部管理网关/网络访问 |

## 5. 总体架构

```text
外部普通用户
   |
   |-- User Manage: 本人资料、地址
   |-- User Public: 供应商、商品、支付类型 facade
   `-- User Private: 下单、撤单、支付、本人订单、WebSocket
                         |
                         | internal gRPC
                         v
Supplier Public <---- Order Service Public
供应商/商品查询          下单、撤单、支付、订单、支付类型
      ^                     |
      |                     | reliable order events
      |                     |---> User: 缓存失效 + WebSocket
      |                     `---> Supplier: 永久只读订单投影
      |
User/Order 直接构造真实 Public Router
```

路由矩阵：

| 服务 | Manage | Public | Private |
| --- | --- | --- | --- |
| User | User、Address | Supplier/Product/PaymentType facade | AddOrder、CancelOrder、CreatePayment、GetOrders |
| Supplier | Supplier、Product、只读 SupplierOrder | GetSuppliers、GetProducts | 无 |
| Order | PaymentType、只读 Order/PaymentRecord、受控状态命令 | 仅内部调用的订单与支付 API | 无 |

## 6. 目录与依赖

```text
examples/06-shop-microservices/
├── contract/                    # 服务名、事件名、版本和稳定错误
├── dto/
│   ├── user/
│   ├── supplier/
│   ├── order/
│   └── event/
├── user-service/
│   ├── models/
│   ├── api/manage/
│   ├── api/public/
│   └── api/private/
├── supplier-service/
│   ├── models/
│   ├── business/
│   ├── api/manage/
│   └── api/public/
├── order-service/
│   ├── models/
│   ├── business/
│   ├── api/manage/
│   └── api/public/
├── runtime/                     # 通用 Outbox/订阅辅助
├── main/{all-in-one,user,supplier,order}/
└── deploy/
```

依赖规则：

- `contract` 无业务和框架依赖。
- DTO 不引用 Model、business、RouterInfo、ServiceContext 或数据库。
- API 依赖 business/models；business 依赖 models；models 不反向引用 API。
- 服务间只共享 contract 与 DTO，不跨服务 import Model 或 business。
- 调用方直接构造目标服务真实 Public Router；不得建立 `api/call`、静态地址 client 或第二套序列化类型。
- Supplier Service 不 import Order Service 的 API、business 或 models。

## 7. 身份与业务主键

User：

```text
Token UID -> User.AuthUserID -> User.ID
                               |-- Address.UserID
                               |-- Order.UserID
                               `-- Order event UserID
```

Supplier：

```text
Token UID -> Supplier.AuthUserID -> Supplier.ID
                                       |-- Product.SupplierID
                                       |-- SupplierOrder.SupplierID
                                       `-- Supplier/order event SupplierID
```

规则：

- AuthUserID 唯一，只用于认证映射，不出现在 Public DTO 或跨服务事件。
- 当前身份只从 `req.GetUser()`/claims 获取。
- Manage Hook 先映射数字业务 ID，再添加 Search 条件或校验目标 owner。
- Body、Query、WebSocket 和 Manage Model 中的 UserID/SupplierID 不能覆盖可信归属。
- 平台管理员使用固定服务端身份 `platform-admin`，不创建 User 或 Supplier 业务主体。

## 8. Core 内部调用方约束

### 8.1 RouterInfo 能力

新增冻结的 RouterInfo 元数据和 Option，语义类似：

```go
router.WithInternalCallers(contract.UserServiceName)
```

要求：

- 注册前声明，Freeze 后只读。
- 提供返回防变更副本的 Getter。
- 未配置时保持现有 Public/Private/Manage 行为，不改变既有 Router。
- 配置后只允许列入白名单的可信服务调用；普通 HTTP 请求没有可信内部身份，必须拒绝。
- 路由/OpenAPI/兼容快照必须记录该约束，避免安全属性静默漂移。

### 8.2 可信来源

框架不得只信任 Payload.SourceService。

- 同进程：来源来自发起调用的真实 Source ServiceContext。
- 跨进程：mTLS 客户端证书服务身份必须与 Payload.SourceService 一致。
- HTTP：不存在内部调用方身份。
- all-in-one insecure：只接受已知同进程 ServiceContext，不把远程 insecure 请求视为可信服务。
- 错误证书、伪造 SourceService、空来源和未列入白名单的服务均在 Parse/Validation/Do 前拒绝。

为避免破坏稳定 `IRequest`，可信调用方读取优先使用可选接口或路由执行上下文；授权由框架统一执行，不要求每个 API 重复 Validation。

### 8.3 服务调用白名单

以下 Order Public Router 只允许 `shop-user`：

- CreateOrder
- CancelOrder
- CreatePayment
- GetOrders
- GetPaymentTypes

Supplier Public 同样是内部能力，不允许外部 HTTP 直调：

- GetSuppliers 只允许 `shop-user`。
- GetProducts 允许 `shop-user` 和 `shop-order`；前者用于普通用户查询，后者用于下单时获取可信商品与供应商快照。

外部普通用户只能通过 User Public facade 查询 Supplier、Product 和 PaymentType。

## 9. Supplier Service

### 9.1 模型

`Supplier`：数字 ID、AuthUserID、Code、Name、Description、Enabled。AuthUserID、Code、Name 唯一并规范化。

`Product`：数字 ID、SupplierID、Code、Name、Price、Enabled。SupplierID 创建后不可通过普通 Edit 改变；新增默认下架。

`SupplierOrder`：OrderID 唯一、OrderRevision、SupplierID、ProductID、商品成交快照、数量、总额、PaymentStatus、OrderStatus、收件人、电话、地区、详细地址、CreatedAt、UpdatedAt。记录永久保留，只允许更高 Revision 更新。

`Outbox`：Supplier/Product 变化与业务事实同事务写入。

`Inbox`：订单事件幂等事实；Inbox 检查、SupplierOrder upsert 和 Inbox 插入必须处于同一事务。

### 9.2 TestToken 注册

- 非管理员 Manage TestToken 首次签发时按 AuthUserID 幂等创建 Supplier。
- 新 Supplier 默认启用，可立即维护资料、创建商品和上架。
- 创建失败拒绝 Token，不能产生有身份但无业务主体的状态。

### 9.3 SupplierManage

- 普通 Supplier Search/View/Edit 只能访问本人；管理员不加 owner 限制。
- 普通 Supplier 可以编辑本人公开资料，不能修改 AuthUserID、数字 ID 或 Enabled。
- Enabled 只能由管理员通过启用/禁用命令修改，普通 Edit 不能绕过。
- 禁用 Supplier 本人后台只读，不能新增、编辑、删除或上下架商品。
- 普通 Supplier 不能删除自己。
- 管理员只能删除没有 Product 且没有 SupplierOrder 引用的 Supplier。

### 9.4 ProductManage

- 普通 Supplier Search 自动按本人 Supplier.ID 过滤；管理员查看全部。
- Add 时普通 Supplier 的 SupplierID 由服务端注入；管理员可以选择目标 Supplier。
- View/Edit/Remove 和上下架命令均校验 owner。
- Enabled 不能通过普通 Edit 修改。
- 所属 Supplier 与管理员都可以上下架 Product。
- Supplier 被禁用时 Product 不可重新上架，且不会出现在 Public 查询。
- 未使用 Product 可由 owner 或管理员删除；存在 SupplierOrder 引用后只能下架。

### 9.5 OrderManage

- 只注册 View/Search，不提供 Add/Edit/Remove 或状态命令。
- 普通 Supplier 只能查看本人 SupplierID 的投影；管理员查看全部。
- 展示商品成交快照、数量、金额、支付状态、订单状态、收件人、电话、地区、详细地址和时间。

### 9.6 内部 Public API 与缓存

GetSuppliers：

- `GET /api/shop-supplier/getsuppliers`
- 支持 id、code、name。
- 只返回 Enabled Supplier。
- DTO 不包含 AuthUserID。

GetProducts：

- `GET /api/shop-supplier/getproducts`
- 支持 id、code、name、supplierID。
- 只返回 Product.Enabled 且 Supplier.Enabled 的结果。
- DTO 包含 ProductID、Code、Name、Price、SupplierID、SupplierCode、SupplierName。

两个 Router 使用 30 秒缓存，CacheKey 包含全部规范化条件。Supplier/Product 事务提交成功后，Manage After Hook 立即失效本服务 Public 缓存；Outbox 可靠发布 SupplierChanged/ProductChanged，供 User facade 失效。TTL 只作兜底。两个 Router 都声明内部调用方白名单，HTTP 直调必须拒绝。

## 10. User Service

### 10.1 模型与注册

`User`：数字 ID、AuthUserID、Name、Enabled。User 永不物理删除。

`Address`：数字 ID、UserID、Recipient、Phone、Region、Detail。Address 可物理删除，历史订单依赖地址快照。

`Inbox`：Supplier/Product/PaymentType/Order 控制事件幂等消费。

非管理员 TestToken 首次签发时幂等创建 User，默认启用。普通 Auth Token 用于 Private；同一 AuthUserID 的 Manage Token 用于本人后台。平台管理员不创建 User 主体。

### 10.2 UserManage

- 普通 User Search/View/Edit 只能访问本人；管理员查看和维护全部。
- User 不注册 Remove。
- Enabled 只能由管理员通过启用/禁用命令修改。
- 禁用 User 本人后台只读，可查看资料、地址和历史订单，但不能发起任何业务写操作。

### 10.3 AddressManage

- 普通 User Search/View/Edit/Remove 只能操作本人地址；管理员管理全部。
- Add 时 UserID 由可信身份映射注入。
- 禁用 User 不能新增、修改或删除 Address。
- Address 删除不影响历史订单地址快照。

### 10.4 Public facade

- GetSuppliers -> Supplier Public GetSuppliers
- GetProducts -> Supplier Public GetProducts
- GetPaymentTypes -> Order internal Public GetPaymentTypes

三个 Router 返回独立 DTO。Supplier/Product facade 使用 30 秒缓存并包含全部查询条件；PaymentType 使用 30 秒缓存。User 消费 SupplierChanged、ProductChanged、PaymentTypeChanged 后按 EventID 幂等失效缓存。

### 10.5 Private API

AddOrder：

- 参数为 requestID、productID、quantity、addressID。
- 从 Token 映射数字 User.ID，校验 User 启用且 Address 属于本人。
- 使用 `UserID + requestID` 形成稳定幂等键。
- 把可信 UserID、AddressSnapshot、ProductID、Quantity 和幂等键传给 Order CreateOrder。
- ProductSnapshot 由 Order 调 Supplier GetProducts 获取，User 不重复保存或信任客户端价格。

CancelOrder：校验 User 启用，调用 Order CancelOrder；Order 再校验数字 UserID 与订单归属。

CreatePayment：校验 User 启用，调用 Order CreatePayment；Order 校验所有权、订单状态和支付类型。

GetOrders：调用 Order GetOrders，只传可信数字 UserID；使用数字 UserID 摘要作为 10 秒缓存键，并作为本人订单 WebSocket 订阅 Router。

### 10.6 WebSocket

Order 事件到达后，User 在 Inbox 幂等边界中失效该用户 GetOrders 缓存，并只向数字 UserID 匹配的会话推送。WebSocket 不接受客户端 UserID，不保存离线通知，不承担服务间通信。禁用 User 可以继续接收已有订单状态通知。

## 11. Order Service

### 11.1 模型

`Order`：数字 UserID、SupplierID、ProductID、幂等键、OrderRevision、商品/供应商/价格快照、数量、总额、地址快照、PaymentStatus、OrderStatus。

`PaymentType`：Code、Name、Enabled；新增默认禁用。

`PaymentRecord`：OrderID、PaymentTypeID、Attempt/PaymentID、Amount、Status。业务哈希必须区分同一订单的不同支付尝试。

`Outbox`：Order、Payment、PaymentType 事实与事件同事务。

Order 当前没有业务事件消费需求时不创建空 Inbox 能力。

### 11.2 CreateOrder

1. 接收可信数字 UserID、稳定幂等键、ProductID、Quantity 和 AddressSnapshot。
2. 调用 Supplier GetProducts 以 ProductID 精确获取当前可售 Product/Supplier 快照。
3. 计算总额并冻结快照。
4. 在同一事务写 Order 与 Outbox，OrderRevision 从 1 开始。
5. 幂等键具有唯一约束；并发冲突后重新读取原 Order。
6. 相同幂等键、相同请求指纹返回原 Order；商品、数量或地址不同则返回稳定的幂等键复用错误。

### 11.3 CancelOrder、Payment 与查询

- CancelOrder 校验数字 UserID 所有权，保留 Order 并进入 Cancelled。
- 重复 Cancel 幂等返回当前 Order。
- 已支付订单撤单按支付状态机进入退款流程，不物理删除。
- CreatePayment 只允许有效订单和启用 PaymentType；每次尝试创建新 PaymentRecord。
- 同一订单同一时刻只允许一个有效 Processing 流水。
- GetOrders 按可信数字 UserID 查询，可选 OrderID。
- GetPaymentTypes 只返回启用项并使用 30 秒缓存；PaymentType 提交后立即本地失效并发布 PaymentTypeChanged。

### 11.4 Manage

所有 Order Manage 只允许平台管理员。

- PaymentTypeManage：CRUD、启用/禁用；Enabled 不允许普通 Edit。被 PaymentRecord 使用后不能删除或修改 Code，只能禁用。
- OrderManage：只读 View/Search，并提供受控取消/退款命令，不开放通用 Add/Edit/Remove。
- PaymentRecordManage：只读 View/Search，并提供确认支付、支付失败、确认退款命令。
- 每个状态命令都在事务内重新读取 Order/PaymentRecord 并验证当前状态，不能信任页面旧值。

### 11.5 事件

每次 Order/Payment 状态事务同时递增 OrderRevision 并插入完整订单快照 Outbox。User 消费用于缓存/WebSocket；Supplier 消费用于永久 SupplierOrder 投影。事件允许包含履约地址，但日志、指标和错误响应禁止记录 payload。

## 12. 可靠事件与缓存拓扑

| 事件 | 发布者 | 消费者 | 作用 |
| --- | --- | --- | --- |
| SupplierChanged | Supplier | User | 失效供应商与商品 facade 缓存 |
| ProductChanged | Supplier | User | 失效商品 facade 缓存 |
| PaymentTypeChanged | Order | User | 失效支付类型 facade 缓存 |
| OrderCreated | Order | User、Supplier | User 缓存/WebSocket；Supplier 创建投影 |
| OrderStatusChanged | Order | User、Supplier | 更新展示和投影 |
| PaymentChanged | Order | User、Supplier | 更新支付状态和投影 |

订单事件字段：EventID、SchemaVersion、OrderRevision、OrderID、UserID、SupplierID、ProductID、商品与价格快照、Quantity、TotalAmount、PaymentStatus、OrderStatus、AddressSnapshot、CreatedAt、UpdatedAt。

可靠性规则：

- 业务事实与 Outbox 同事务。
- Outbox worker 只在外部 Publish 成功后标记完成。
- User 与 Supplier 使用不同逻辑消费组，各收到一份订单事件。
- Handler 成功后 ACK；失败保留 pending 并由存活消费者 reclaim。
- Inbox EventID 重复视为成功；旧 OrderRevision 视为已处理，不能回滚投影。
- 未知 SchemaVersion、缺少关键 ID 或非法状态返回错误，不 ACK。
- 必需订阅全有或全无；任一订阅失败时撤销本轮订阅并阻止服务提供流量。
- 缓存只在业务事务提交成功或事件消费成功后失效。

## 13. 错误与日志

- 身份不能映射业务主体：稳定 Forbidden。
- 普通主体访问他人记录：Search 不泄漏记录；View/Do 返回安全 NotFound/Forbidden。
- 禁用主体写入：稳定返回“已禁用，只允许查看”。
- 删除已使用 Supplier/Product：稳定业务错误并提示禁用/下架。
- 服务发现、内部身份、目标路由或 mTLS 校验失败：fail closed，不回退 AttachServices 或外部 HTTP。
- 内部事件错误不得把地址、电话、Token、Claims 或完整订单写入日志。
- 日志使用稳定事件名和 service、caller_service、target_service、route、event_type、event_id、aggregate_id、trace_id、error 字段。

## 14. 部署

### 14.1 all-in-one

- 仅用于本地调试和快速集成。
- 三个 ServiceContext 使用独立数字空间和 gRPC listener。
- 内部调用方身份来自真实 Source ServiceContext。
- Redis 继续承载 EventBridge；发现可使用 local provider。
- insecure 仅限已知同进程调用，不作为远程可信身份。

### 14.2 三进程

- Redis 承载 Discovery 与 Streams，命名空间隔离。
- 内部同步调用固定 gRPC，无 HTTP fallback。
- mTLS 证书 SAN 使用稳定服务名，客户端身份与 SourceService 必须匹配。
- User HTTP 可以按示例需要映射。
- Supplier HTTP 只为供应商 Manage 入口映射；其 Public Router 仍由内部调用方白名单保护。
- Order HTTP 不设置宿主机 `ports`；Manage 只经内部管理网关或受控管理网络访问。
- 所有 gRPC 端口只在服务网络开放。

## 15. 测试与验收

### 15.1 Core

- RouterInfo internal callers Option、Freeze、Getter 防变更副本与多服务注册隔离。
- 同进程正确 caller 通过，错误 caller/空 caller 被拒绝。
- 跨进程正确 mTLS caller 通过；伪造 SourceService、错误证书 SAN、无证书和 HTTP 直调被拒绝。
- internal callers 路由属性进入路由/API 兼容快照。
- 未配置 internal callers 的现有 Router 行为不变。
- race 覆盖注册、解析、请求转换和关闭。

### 15.2 Supplier

- TestToken 注册、数字 ID 映射和默认启用。
- Supplier/Product/Order Manage 的本人、禁用主体、管理员 Hook 矩阵。
- Product 新增默认下架；普通 Edit 不能改变 Enabled/SupplierID。
- Supplier/Product 使用后删除失败。
- Inbox 重投、乱序 Revision、事务失败和 ACK 语义。
- GetSuppliers/GetProducts 筛选、DTO、缓存键和主动失效。
- 路由清单不存在 Private 和 `api/call`。

### 15.3 User

- TestToken 注册、User 永不 Remove、Address owner 注入。
- 普通 User、禁用 User、管理员的 Manage 权限矩阵。
- Public facade 调用真实 Supplier/Order Public Router。
- Private API 拒绝伪造 UserID、禁用 User 写入和他人 Address。
- requestID 重试返回同一 Order；不同 payload 复用 key 被拒绝。
- GetOrders 缓存键只使用可信数字 UserID。
- WebSocket 只向匹配 UserID 会话推送。

### 15.4 Order

- CreateOrder 商品/供应商/地址/价格快照。
- 唯一幂等键的并发冲突收敛与请求指纹校验。
- Cancel 保留 Order 并进入 Cancelled。
- PaymentType 默认禁用、引用删除保护和 Code 冻结。
- PaymentRecord 多次尝试哈希与单 Processing 约束。
- 受控支付/失败/退款状态机事务。
- OrderRevision 单调递增，事实与 Outbox 原子。
- 无 Private Router、无 WebSocket。

### 15.5 真实集成

同进程：

- 真实 HTTP、TestToken、Manage、Public、Private、Redis Streams 和 WebSocket。
- User -> Order -> Supplier 的真实 Router/DTO 链。
- Supplier/User owner 隔离、管理员权限和禁用只读。
- 缓存主动失效、SupplierOrder 投影和删除保护。

三进程：

- Redis Discovery、三个独立 race 进程和 mTLS gRPC。
- Supplier/Order Public 的 HTTP、错误 caller 和错误证书负向验收。
- User -> Order -> Supplier 传输计数证明 gRPC 被实际使用，HTTP fallback 为零。
- Outbox pending/retry、Inbox 幂等、订单投影和 WebSocket 最终收敛。
- Order HTTP 无宿主机映射。

总门禁：定向测试、完整示例 race、go vet、日志守卫、路由/API/配置/发布兼容检查和 Compose config 全部通过。

## 16. 文档与能力并入

实现完成并验收后同步：

- `examples/06-shop-microservices/README.md`
- `examples/README.md` 的 01-06 能力索引
- `docs/codex/FRAMEWORK_USAGE_GUIDE.md`
- `docs/codex/ROUTERINFO_RUNTIME_GUIDE.md`
- `docs/codex/API_COMPATIBILITY_SURFACE.md`
- `docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`（仅当配置表面变化）
- `docs/codex/DEPRECATION_REGISTER.md`、CHANGELOG 和发布契约（按实际公共变化）
- `.codex/skills/use-digitalway-core/SKILL.md`
- `.codex/skills/use-digitalway-core/references/core-backend-api.md`

技能必须把示例 06 更新为：面向主体的 Manage/Public/Private 边界、统一 Manage Hook、数字业务身份、真实 Public Router 跨服务调用、内部调用方白名单、可靠订单投影、User WebSocket、Redis Discovery、gRPC/mTLS 和两种集成测试。

## 17. 完成定义

- Supplier 无 Private、无 `api/call`；User/Order 直接调用其真实 Public Router。
- User 是普通用户唯一外部业务入口；Supplier 是供应商后台和资料权威；Order 是内部订单/支付事实及管理服务。
- Supplier Public 只有可信 `shop-user`/`shop-order` 可以调用；Order Public 只有可信 `shop-user` 可以调用；HTTP 和伪造内部来源均被拒绝。
- User/Supplier 的认证 UID 与数字业务 ID 完全分离。
- 所有 Manage owner、管理员、禁用只读和字段保护规则由统一 Hook 实现。
- SupplierOrder 永久只读投影同时支撑后台查询和 Supplier/Product 删除保护。
- User 下单使用客户端 requestID，端到端重试不会重复创建订单。
- Order 撤单保留历史，支付状态机只能通过 business 和受控 Manage 命令推进。
- Supplier/Product/PaymentType/Order 事件形成完整缓存失效、投影和 WebSocket 闭环。
- 同进程与三进程真实测试、race、vet、日志和兼容门禁全部通过。
- README、现行 docs/codex 和 `use-digitalway-core` 能力说明与最终实现一致。
