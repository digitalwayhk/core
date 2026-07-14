# 第三个商城继承示例设计

## 1. 文档状态

- 状态：设计已确认，等待书面规格复审
- 日期：2026-07-14
- 应用目录：`examples/03-shop-inheritance`
- 集成测试目录：`examples/integration/03-shop-inheritance`
- 服务名：`inheritanceshop`

## 2. 目标

第三个示例是在第二个商城支付示例能力上的独立完整应用，重点演示服务内模型继承和 Manage 继承的正确用法。示例必须能够单独启动、阅读和测试，不引用 `examples/01-simple-shop` 或 `examples/02-shop-payment` 的业务包。

示例保留商品、订单、支付类型、支付流水、Public/Private/Manage API、支付状态机和用户 WebSocket，并新增供应商能力。核心教学目标是：

1. 使用服务级基础模型承载所有模型共有的数据库能力。
2. 将基础资料模型和业务模型分层，并让具体模型只声明自身差异。
3. 通过 Manage 继承复用字段展示、启用禁用、状态显示和通用校验。
4. 将最终具体 owner 传入每层 Manage，确保框架 hook 分派到具体实现。
5. 在子级重载 hook 时显式调用父级 hook，避免覆盖后丢失通用规则。
6. 用真实进程集成测试证明继承能力可以经过 HTTP、认证、数据库和 WebSocket 完整运行。

本示例不以继承层数为目标。只保留能表达明确业务职责的层级，不复制 Futures 项目中的历史重复字段、过深构造链或单例 Manage 请求状态。

## 3. 范围与非目标

### 3.1 本次范围

- 供应商模型和只读商品子集合。
- 商品增加供应商引用。
- 订单增加供应商快照。
- 服务通用模型、基础资料模型、业务模型三层模型结构。
- 服务通用 Manage、基础资料 Manage、业务 Manage 三层管理结构。
- 商品、供应商、支付类型继承通用启用和禁用能力。
- 订单、支付流水继承通用业务状态显示能力。
- 完整 Public、Private、Manage 和 WebSocket 集成测试。

### 3.2 非目标

- 不实现供应商账号、结算、库存、采购或供应链流程。
- 不允许在供应商商品子表中直接新增、编辑或删除商品。
- 不自动级联修改供应商下商品的启用状态。
- 不连接真实支付网关或外部消息系统。
- 不增加第四个多层继承示例。
- 不修改框架生产代码；如发现框架能力缺口，必须停止扩展并单独评审。

## 4. 目录与依赖方向

```text
examples/03-shop-inheritance/
├── contract/
├── models/
│   ├── data_action.go
│   ├── shop_model.go
│   ├── base_data_model.go
│   ├── business_model.go
│   ├── product.go
│   ├── supplier.go
│   ├── payment_type.go
│   ├── order.go
│   ├── payment_record.go
│   └── *_persistence.go
├── business/
│   ├── product.go
│   ├── supplier.go
│   ├── payment_type.go
│   ├── order.go
│   └── payment.go
├── api/
│   ├── dto/
│   ├── manage/
│   ├── public/
│   └── private/
├── service.go
└── main/main.go

examples/integration/03-shop-inheritance/
├── shop_helpers_test.go
├── manage_test.go
├── public_test.go
└── private_test.go
```

依赖方向固定为：

```text
Public / Private / Manage API -> business -> models -> IDataAction
```

- `contract` 只定义稳定服务名等无依赖契约，不引用项目内其他包。
- API 负责请求、认证身份、DTO、Manage 视图和提交成功后的通知。
- business 负责状态、引用、所有权、事务和跨模型规则。
- models 负责模型契约、查询和持久化，不引用 API、DTO、Manage 或 WebSocket。
- Public/Private 只返回独立 DTO，不直接序列化持久化模型。
- SQLite 只在 models 的数据访问组合根选择，不沿调用链传递具体数据库类型。

## 5. 模型继承设计

### 5.1 继承结构

```text
entity.Model
└── ShopModel
    ├── BaseDataModel
    │   ├── Product
    │   ├── Supplier
    │   └── PaymentType
    └── BusinessModel
        ├── Order
        └── PaymentRecord
```

Go 嵌入不提供虚构造。每个具体模型的构造方法和 `NewModel()` 必须显式初始化从 `entity.Model` 到具体模型的完整指针链，不能留下 nil 嵌入字段。

### 5.2 ShopModel

`ShopModel` 嵌入 `*entity.Model`，统一定义本服务模型使用的数据库名称、初始化约束和服务级公共能力。它不得保存请求、用户、trace、响应或事务等请求级状态。

### 5.3 BaseDataModel

基础资料公共字段：

| 字段 | 约束 |
| --- | --- |
| `Code` | 必填；去除首尾空白并转为小写后，在具体模型表内唯一；作为稳定业务标识 |
| `Name` | 必填；去除首尾空白后在具体模型表内唯一 |
| `Enabled` | 只通过 Enable/Disable 命令修改 |
| `Description` | 可选说明 |

`GetHash()` 使用规范化后的 `Code`。`Code` 与 `Name` 都必须在 `AddValid`、`UpdateValid` 和数据库唯一约束中保护，不能只依赖哈希碰撞。唯一性范围是各具体模型自己的表，不要求商品、供应商和支付类型之间互不重复。

基础资料新增时统一强制为禁用状态，必须通过 Enable 命令显式启用。客户端或 Manage Add 请求携带的 `Enabled=true` 不得绕过该规则。

`BaseDataModel` 提供供泛型 Manage 使用的公共访问方法，使通用启用、禁用和字段格式化不需要反射修改具体模型。

### 5.4 BusinessModel

`BusinessModel` 提供公共 `Status int`。基础层只负责存储和只读展示，具体模型负责：

- 定义自己的强类型状态常量；
- 校验允许的状态迁移；
- 将状态转换为中文显示；
- 禁止业务代码散落未命名的原始整数。

订单继续保留独立的 `PaymentStatus`，因为订单生命周期和支付生命周期不是同一个状态机。支付流水使用继承的 `Status` 表达支付状态。

### 5.5 Supplier

供应商除基础资料字段外，包含只读商品集合：

```go
Products []*Product `gorm:"foreignKey:SupplierID"`
```

- 商品集合只用于供应商 Manage 页面展示和查询。
- 商品新增、编辑和删除必须统一经过 `ProductManage`。
- 供应商存在任意商品引用时禁止删除。
- 禁用供应商不级联修改商品的 `Enabled`。
- 重新启用供应商后，其自身仍为启用状态的商品恢复公开可见和可下单。

### 5.6 Product

商品增加：

- `SupplierID`：必填供应商引用；
- `Price`：必须大于零。

新增、编辑、启用商品时，供应商必须存在且已启用。供应商后来被禁用时，商品自身状态不变，但有效性由“商品启用且供应商启用”共同决定。

### 5.7 PaymentType

支付类型继承基础资料字段。禁用后不能创建新支付；已有支付流水仍可继续确认、失败和退款。被支付流水引用后禁止删除，且禁止修改稳定 `Code`。

### 5.8 Order 与 PaymentRecord

订单保留示例 2 的用户、数量、商品和价格快照，并新增：

- `SupplierID`；
- `SupplierCode`；
- `SupplierName`。

下单后修改基础资料或改变其启用状态不得影响历史订单快照。订单继续使用继承的 `Status` 表达订单状态，并使用 `PaymentStatus` 表达支付状态。

支付流水继承 `BusinessModel.Status`，保留支付类型快照、金额、尝试次数和支付/退款时间。支付失败后的重试创建新流水，不覆盖历史记录。

## 6. Manage 继承设计

### 6.1 继承结构

```text
manage.ManageService[T]
└── ShopManage[T]
    ├── BaseDataManage[T]
    │   ├── ProductManage
    │   ├── SupplierManage
    │   └── PaymentTypeManage
    └── BusinessManage[T]
        ├── OrderManage
        └── PaymentRecordManage
```

每层构造函数必须接收并向父层传递最终具体 owner。不得把中间层自身错误地注册成 owner，否则具体 Manage 的 hook 不会执行。

Go 嵌入不是虚方法继承。具体 Manage 重载 `ValidationAfter`、`ViewFieldModel`、`ViewChildModel`、`ViewCommandModel` 等 hook 时，必须先显式调用直接父级的同名 hook，再追加具体规则。除非规格明确要求替换父级行为，否则不得跳过父级调用。

### 6.2 ShopManage

`ShopManage[T]` 统一处理：

- `ShopModel` 的框架公共字段显示；
- ID、创建时间、更新时间等公共列顺序和只读属性；
- `ModelList` 的标准构造；
- 最终 owner 的保存和 hook 分派。

Manage 是 ServiceContext 内长期对象，不得保存当前请求、选中行、用户或临时命令参数。

### 6.3 BaseDataManage

`BaseDataManage[T]` 默认提供 View、Search、Add、Edit、Remove，以及通用 Enable、Disable 命令。

公共行为：

- 格式化 Code、Name、Description；
- `Enabled` 显示“启用/禁用”，普通编辑只读；
- Add 强制写入禁用状态，不信任请求中的 Enabled；
- Add/Edit 校验 Code、Name 必填和唯一；
- Enable/Disable 调用最终具体 owner 的状态变更 hook，再持久化；
- 重复启用或禁用保持幂等；
- 命令失败不修改数据库，也不发送观察事件。

### 6.4 BusinessManage

`BusinessManage[T]` 默认只提供 View 和 Search，并将 `Status` 设置为只读中文状态字段。具体业务 Manage 可以显式增加受控命令，但不能通过普通 Edit 绕过状态机。

### 6.5 具体 Manage

`ProductManage`：

- 继承完整 CRUD 和启用、禁用；
- 父级校验完成后，校验价格和供应商引用；
- 新增、编辑、启用时拒绝不存在或已禁用的供应商；
- 被订单引用后禁止删除。

`SupplierManage`：

- 继承完整 CRUD 和启用、禁用；
- `Products` 子集合只读，`IsAdd`、`IsEdit`、`IsRemove` 全部为 false；
- 仍允许通过子表执行查看、搜索和分页；
- 存在任意商品时禁止删除。

`PaymentTypeManage`：

- 继承完整 CRUD 和启用、禁用；
- 被支付流水引用后禁止删除和修改 Code。

`OrderManage`：

- 继承只读 View/Search；
- 格式化订单状态、支付状态、用户、商品和供应商快照；
- 不提供后台 Add/Edit/Remove。

`PaymentRecordManage`：

- 继承只读 View/Search；
- 保留 ConfirmPayment、FailPayment、ConfirmRefund 受控命令；
- 命令通过 business 事务推进状态，不能直接修改模型字段。

## 7. API 与业务规则

### 7.1 Public API

`GetSuppliers` 只返回启用供应商，支持可选 `id`、`code`、`name` 筛选。

`GetProducts` 只返回“商品启用且供应商启用”的商品，支持可选：

- `id`；
- `code`；
- `name`；
- `supplierID`；
- `supplierCode`。

参数全部为空时返回全部有效商品；多个参数按 AND 组合。DTO 包含商品 ID、Code、Name、Price，以及供应商 ID、Code、Name。

`GetPaymentTypes` 只返回启用支付类型，并支持可选 Code、Name 筛选。

### 7.2 Private API

保留示例 2 的能力：

- 新增订单；
- 查询本人订单并订阅 WebSocket；
- 删除本人未支付或支付失败订单；
- 为本人订单创建支付；
- 撤销已支付订单。

身份只从 `req.GetUser()` 获取。订单查询、删除、支付和撤销均不得接受或信任客户端 UserID。

下单时必须在业务层重新查询商品和供应商，并同时确认二者启用。订单保存商品、价格和供应商快照。商品不存在、商品禁用、供应商不存在或供应商禁用均返回稳定的公开业务错误。

支付状态机、退款规则、幂等规则和事务边界与示例 2 保持一致：跨模型写入使用克隆的 `IDataAction`，在同一事务中完成，只有提交成功后才发布观察事件。

### 7.3 WebSocket 与 EventBridge

WebSocket 只面向最终外部用户。`GetOrders` 的订阅实例按会话持久存在，直到退订或断开，不进入普通请求对象池。

订单 DTO 统一包含 `Action`，HTTP 查询时为空，WebSocket 推送时使用 `created`、`deleted`、`payment_pending`、`payment_failed`、`paid`、`refund_pending`、`cancelled` 等动作。通知只投递给订单所属用户。

通知通过服务专属 EventBridge 和现有 RouterInfo WebSocket 发布能力处理。通知属于 best-effort 观察事件：无订阅者时直接丢弃，发送失败不回滚已提交事务。

## 8. 错误、并发与兼容约束

- 业务错误使用类型化公开错误，不向响应暴露 SQL、内部错误或对象内容。
- 唯一性同时由业务预检和数据库约束保护；并发冲突必须转换为稳定业务错误。
- 状态命令在事务内重新读取数据，不信任 Manage 页面提交的旧状态。
- 禁用供应商和商品不得级联改写历史订单或支付流水。
- 示例不增加新的公共框架 API、配置字段、路由规则或 JSON 契约。
- 如实现必须修改 `pkg/`、`service/manage` 或其他框架生产代码，应停止示例开发，单独说明缺口、兼容风险和测试范围，经确认后再处理。

## 9. 测试设计

### 9.1 模型契约测试

- 每个具体模型的完整嵌入链均已初始化。
- Code、Name 必填且在各自具体表内唯一。
- `GetHash()` 使用规范化 Code。
- 业务状态具有强类型转换和稳定中文显示。
- 供应商商品关系使用 `SupplierID`，且订单供应商快照不可变。

### 9.2 Manage 继承测试

- 最终具体 owner 能收到父级触发的 hook。
- 子级 hook 显式调用父级后，通用规则和具体规则都执行。
- Product、Supplier、PaymentType 自动继承 Enable/Disable。
- Product 的重载规则拒绝已禁用供应商。
- Supplier.Products 的新增、编辑、删除权限均关闭。
- BusinessManage 默认只读，PaymentRecordManage 只通过受控命令推进状态。
- Manage 实例不保存请求级可变状态，race 测试无数据竞争。

### 9.3 业务测试

- 禁用供应商不改变商品 Enabled，但 Public 查询和下单均拒绝该商品。
- 重新启用供应商后，原本启用的商品恢复公开可见和可下单。
- 商品创建、编辑、启用时拒绝无效供应商。
- 供应商有商品时不能删除。
- 订单供应商、商品和价格快照不随基础资料修改而变化。
- 禁用支付类型阻止新支付，但不阻止既有流水继续确认、失败和退款。
- 覆盖支付失败、重试、确认、退款、幂等和事务隔离。

### 9.4 集成测试

集成测试复用 `examples/integration/helpers.go`，启动真实服务进程，使用框架自动生成配置、系统临时数据目录、真实 HTTP、内建 TestToken、SQLite 和真实 WebSocket。

必须保留三个整组入口：

- `TestManageAPIs`；
- `TestPublicAPIs`；
- `TestPrivateAPIs`。

每个 API 和 Manage command 分成独立测试方法或子测试，再由三个整组入口统一调用。覆盖：

- Manage 的所有 CRUD、继承得到的启用/禁用命令和支付命令；
- 供应商商品只读子表的查询及写权限关闭；
- Public 可选筛选和商品、供应商联合有效性；
- Private 身份隔离、订单所有权、供应商快照和完整支付流程；
- WebSocket 只向当前用户推送订单变化；
- 进程退出、数据库关闭和临时目录清理。

## 10. 完成标准

实现完成后必须通过：

```bash
go test -race ./examples/03-shop-inheritance/... -count=1
go test -race ./examples/integration/03-shop-inheritance -count=1 -timeout=15m
go vet ./examples/03-shop-inheritance/... ./examples/integration/03-shop-inheritance
./scripts/check-logging.sh
```

同时满足：

1. 所有导出类型、方法和关键继承 hook 都有清晰中文注释。
2. 不提交运行时生成的配置、数据库或日志文件。
3. 不引用其他示例的业务包。
4. 不把 DTO 放入集成测试通用 helpers。
5. 不修改框架生产代码；若确有框架缺口，必须拆为独立任务。
6. 实现完成后生成只读外部审查提示词，要求审查继承分派、父级 hook、状态机、事务、权限、race 和集成测试真实性。

## 11. 已选方案与舍弃方案

采用“单个完整示例 + 两条清晰继承支线”：

- 模型按服务公共层、基础资料层、业务层组织；
- Manage 按服务公共层、基础资料层、业务层组织；
- 具体类型只覆盖差异，并显式保留父级行为。

不采用以下方案：

- 复制示例 2 后只增加 Supplier：无法演示继承的复用价值。
- 为每个模型和 Manage 建立更多中间层：层数增加但职责不增加，阅读成本过高。
- 让供应商子表直接编辑商品：会绕过 ProductManage 的供应商、引用和订单规则。
- 禁用供应商时级联禁用商品：会丢失商品自身状态，重新启用时无法恢复原业务意图。
