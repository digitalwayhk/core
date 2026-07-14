# 第二个商城支付示例设计

## 1. 目标

在 `examples/01-simple-shop` 的商品、订单、认证和 WebSocket 能力基础上，新增一个可独立运行的支付示例 `examples/02-shop-payment`。该示例重点演示：

- API、业务层、模型持久化层的明确分层；
- 支付类型后台管理与启用、禁用；
- 第三方支付结果滞后时的订单与支付流水状态机；
- 通过 Manage hook 扩展校验、引用保护和视图元数据；
- 通过自定义 Manage 命令安全推进支付状态；
- 使用真实 HTTP、TestToken、SQLite 和 WebSocket 的完整集成测试。

第二个示例是独立完整应用，不引用第一个示例的业务包。两个示例可以分别启动、阅读和测试。

## 2. 范围与非目标

本示例模拟“用户发起支付、管理员确认第三方结果”的流程，不连接真实支付网关，也不提供第三方回调接口。

本次不实现：

- 支付密钥、商户号或网关地址管理；
- 自动对账、分账、优惠、税费和多币种；
- EventBridge 驱动的最终一致性支付编排；
- 支付中订单的强制撤销；
- 独立退款申请表或复杂审批流。

EventBridge 只承载订单变化观察通知和 WebSocket 投递，不代替数据库事务。

## 3. 目录结构

```text
examples/02-shop-payment/
├── contract/
├── models/
│   ├── data_action.go
│   ├── data_action.go            # IDataAction 单例与事务边界
│   ├── product.go
│   ├── product_persistence.go
│   ├── order.go
│   ├── order_persistence.go
│   ├── paymenttype.go
│   ├── paymenttype_persistence.go
│   ├── paymentrecord.go
│   └── paymentrecord_persistence.go
├── business/
│   ├── product.go
│   ├── order.go
│   ├── payment.go
│   └── paymenttype.go
├── api/
│   ├── dto/
│   ├── public/
│   ├── private/
│   └── manage/
├── service.go
└── main/main.go

examples/integration/02-shop-payment/
├── shop_helpers_test.go
├── public_test.go
├── private_test.go
└── manage_test.go
```

依赖方向固定为：

```text
Public / Private / Manage API -> business -> models -> IDataAction
```

- API 负责参数绑定、认证身份、DTO 转换和提交成功后的通知。
- business 负责所有权、引用关系、状态迁移、金额计算和事务编排。
- models 负责实体规则、查询和持久化，不引用 business、API、DTO 或 Manage。
- Public/Private 不直接返回持久化模型。
- SQLite 只在模型持久化边界选择，不沿 Service、API、business 传递。

## 4. 数据模型

### 4.1 Product

沿用第一个示例：

- 商品名称唯一，`GetHash` 使用规范化名称；
- 价格必须大于零；
- 订单保存商品 ID、名称和单价快照；
- 商品被任何历史订单引用后不能删除，但仍允许修改当前名称和价格。

### 4.2 PaymentType

字段：

- `Code`：稳定、唯一的小写标识，参与 `GetHash`；
- `Name`：展示名称，唯一；
- `Enabled`：是否允许新支付选择；
- `Description`：公开说明。

约束：

- Public API 只返回启用项；
- 禁用不影响既有支付流水的确认、失败或退款；
- 被任意支付流水引用后不能删除；
- 被引用后禁止修改 `Code`，允许修改名称和描述；历史流水继续使用创建时快照；
- `Enabled` 不能通过普通 Edit 修改，只能通过启用、禁用命令变更。

### 4.3 Order

保留第一个示例的商品快照、数量和用户字段，并增加：

- `Status`：订单状态；
- `PaymentStatus`：当前支付状态；
- `PaymentID`：当前或最近一次支付流水 ID。

订单状态：

```text
正常 -> 撤销处理中 -> 已撤销
```

支付状态：

```text
未支付 -> 支付中 -> 已支付 -> 退款中 -> 已退款
                  └-> 支付失败 -> 可重新支付
```

订单哈希继续使用 `UserID + ProductID + UTC 创建时间秒`，保持每个用户每秒只能购买同一商品一次的示例契约。

### 4.4 PaymentRecord

字段：

- `OrderID`、`UserID`；
- `PaymentTypeID`；
- 支付类型 `Code`、`Name` 快照；
- `Amount`，只能由订单价格快照乘数量计算；
- `Attempt`，表示该订单第几次支付尝试；
- `Status`；
- `PaidAt`、`RefundedAt`。

流水状态：

```text
支付中 -> 已支付 -> 退款中 -> 已退款
      └-> 支付失败
```

流水哈希使用 `OrderID + Attempt`。支付失败后的重试必须创建新流水，旧流水只读保留；`Order.PaymentID` 指向最新流水。

## 5. 业务层

业务层不保存请求级状态，提供以下服务：

```text
ProductService
- ValidateCreate
- ValidateUpdate
- EnsureRemovable

PaymentTypeService
- ListEnabled
- ValidateCreate
- ValidateUpdate
- EnsureRemovable
- Enable
- Disable

OrderService
- CreateOrder
- ListUserOrders
- DeleteUnpaidOrder
- RequestCancellation

PaymentService
- CreatePayment
- ConfirmPayment
- FailPayment
- ConfirmRefund
```

核心规则：

- API 传入的金额、用户快照和状态不可信；business 必须从认证身份和数据库事实计算。
- 未支付或支付失败订单允许物理删除。
- 支付中订单禁止删除和撤销，必须等待后台确认成功或失败。
- 已支付订单禁止删除，只能申请撤销。
- 申请撤销同时把订单和当前支付流水置为退款中。
- 确认退款同时把流水置为已退款、订单置为已撤销和已退款。
- 退款中、已退款和已撤销状态禁止重复操作。
- 同一订单同一时刻只能存在一条支付中或退款中的活动流水。
- 查找他人订单和操作他人订单统一返回“订单不存在或无权操作”。

business 返回模型结果和 `OrderChange{Action, Order}`，但不引用 API DTO、RouterInfo 或 WebSocket 类型。

## 6. 事务、并发和幂等

`models.RunInTransaction` 封装单例 `IDataAction` 的 `Transaction`、`Commit` 和 `Rollback`。模型层为事务生命周期提供并发保护，避免共享适配器上的事务状态互相覆盖。

以下操作必须在一个事务中完成：

- 创建支付流水并更新订单；
- 确认支付并更新订单；
- 标记支付失败并更新订单；
- 申请撤销并把订单、流水置为退款中；
- 确认退款并更新订单。

每个状态命令都必须在事务内重新读取订单和流水，不信任 Manage 页面或客户端提交的旧状态。

幂等规则：

- 已到达目标状态的重复确认返回当前结果，不重复写入；
- 不允许的跨状态调用返回明确业务错误；
- 同一订单并发发起支付只能成功一次；
- 支付失败重试递增 `Attempt` 并创建新流水；
- 事务失败或提交失败不发送通知。

## 7. API 设计

Public：

- `GetProducts`：无筛选返回全部商品，可按名称等可选条件筛选；
- `GetPaymentTypes`：无筛选返回全部启用支付类型，可按 `code/name` 筛选，禁用项永不返回。

Private：

- `AddOrder`：创建未支付订单；
- `GetOrders`：查询当前用户订单并提供 WebSocket 订阅；
- `DeleteOrder`：删除当前用户未支付或支付失败订单；
- `CreatePayment`：选择启用支付类型，为当前用户订单创建新流水；
- `CancelOrder`：为当前用户已支付订单申请撤销退款。

Private API 只从 `req.GetUser()` 获取身份，不接受客户端 `UserID`。每个接口实现 `GetResponse()`，返回独立 DTO 供 OpenAPI 使用。

统一订单 DTO 增加订单状态、支付状态、支付 ID 和 `Action`。WebSocket 动作：

```text
created
deleted
payment_pending
payment_failed
paid
refund_pending
cancelled
```

通知只投递给订单所属用户。事务提交后，API 或 Manage 命令将 business 返回的订单变化转换为 DTO，通过 `GetOrders` 已注册的 RouterInfo 发布。底层继续使用服务专属 EventBridge。通知是 best-effort 观察事件，发送失败不回滚业务事务。

## 8. Manage 扩展设计

### 8.1 ProductManage

暴露 View、Search、Add、Edit、Remove。

- `ParseAfter`：规范化商品名称；
- `ValidationAfter(Add/Edit)`：调用 business 校验名称唯一、名称非空、价格为正；
- `ValidationAfter(Remove)`：查询订单引用，只要存在历史订单就拒绝删除；
- `ViewFieldModel`：设置中文字段、名称搜索、价格精度和内部字段可见性。

### 8.2 PaymentTypeManage

暴露 View、Search、Add、Edit、Remove，以及 `EnablePaymentType`、`DisablePaymentType` 自定义命令。

- `ParseAfter`：Code 去空白并转小写，Name 去首尾空白；
- `ValidationAfter(Add)`：校验 Code、Name 唯一；
- `ValidationAfter(Edit)`：已使用时禁止改 Code；禁止普通 Edit 改 Enabled；
- `ValidationAfter(Remove)`：存在支付流水引用时拒绝删除；
- `ViewFieldModel`：Enabled 显示“启用/禁用”，Code 可搜索，状态只读；
- `ViewCommandModel`：设置启用、禁用按钮的中文标题、单行选择、二次确认、图标和顺序。

### 8.3 OrderManage

只暴露 View 和 Search：

- 订单状态、支付状态通过 `ComBoxValue` 显示中文；
- 商品、用户和状态字段可搜索、排序；
- 不允许后台直接编辑订单。

### 8.4 PaymentRecordManage

只暴露 View、Search，以及 `ConfirmPayment`、`FailPayment`、`ConfirmRefund` 自定义命令。

- 支付流水不开放通用 Add、Edit、Remove；
- 金额、订单、用户和支付类型快照全部只读；
- 状态显示中文并支持筛选；
- `ViewCommandModel` 配置“确认支付”“支付失败”“确认退款”按钮；
- 命令调用 business，服务端重新校验状态；
- 状态变化成功后发送统一订单 WebSocket 通知。

按钮只描述客户端能力，不能代替服务端权限和状态校验。

## 9. 测试设计

### 9.1 单元测试

- 模型：哈希、唯一性、金额快照、查询条件和持久化方法；
- business：完整状态迁移、非法跳转、所有权、引用保护、重复操作和事务回滚；
- 并发：同一订单并发发起支付只能生成一条活动流水；
- Manage：状态 ComBox、自定义命令元数据、引用删除保护、Code 冻结和 Enabled 编辑保护；
- 通知：事务成功后发送，失败和回滚不发送；不同用户过滤正确。

### 9.2 集成测试

复用 `examples/integration/helpers.go`，启动真实示例进程，使用自动生成配置、临时数据目录、真实 SQLite、真实 HTTP、内建 TestToken 和真实 WebSocket。

保留三个总入口，每个 API 或命令作为独立子测试：

```text
TestPublicAPIs
- TestGetProducts
- TestGetProductsWithFilter
- TestGetEnabledPaymentTypes
- TestGetPaymentTypesWithFilter

TestPrivateAPIs
- TestAddOrder
- TestGetOwnOrders
- TestDeleteUnpaidOrder
- TestCreatePayment
- TestRejectDisabledPaymentType
- TestRejectSecondActivePayment
- TestRejectDeleteOrCancelWhilePaying
- TestRetryAfterPaymentFailure
- TestRejectDeletePaidOrder
- TestCancelPaidOrder
- TestOrderWebSocketEvents
- TestOrderWebSocketUserIsolation

TestManageAPIs
- 商品 View/Search/Add/Edit/Remove
- 拒绝删除已使用商品
- 支付类型 View/Search/Add/Edit/Remove
- 支付类型启用/禁用
- 拒绝删除已使用支付类型
- 拒绝修改已使用支付类型 Code
- 拒绝普通 Edit 修改 Enabled
- 支付流水 View/Search
- 确认支付
- 标记支付失败
- 确认退款
- 检查状态字段和自定义命令元数据
```

核心端到端链路：

```text
创建商品和支付类型
-> 用户下单
-> 发起支付
-> 后台标记失败
-> 用户重新支付
-> 后台确认成功
-> 用户申请撤销
-> 后台确认退款
-> 验证订单、两条支付流水和全部 WebSocket 事件
```

必须额外验证两个用户之间的查询、删除、支付和 WebSocket 订阅隔离。

## 10. 完成标准

- 第二个示例可独立构建、启动和关闭；
- Public、Private、Manage 和 WebSocket 功能均通过真实集成测试；
- 状态迁移和跨模型更新有事务及并发测试；
- 所有 Go 文件包含必要的中文业务注释并通过 `gofmt`；
- 定向单测、race、日志检查和第二个示例集成测试通过；
- `SKILL.md` 与 `core-backend-api.md` 登记第二个示例为业务层、支付状态机、Manage hook 和自定义命令的标准参考；
- 不提交运行时自动生成的配置或数据文件。
