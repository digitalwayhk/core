# Digitalway Core 后端开发参考

本参考以当前代码和发布契约为准。示例 01–06 依次覆盖最简服务、业务状态机、模型/Manage 继承、性能优化、Casdoor 身份生命周期和 Redis 多服务协同。创建新服务时按复杂度选择最近样例，不另造平行约定。

完整场景矩阵见 `docs/codex/FRAMEWORK_USAGE_GUIDE.md`。

## 标准样例目录

```text
examples/01-simple-shop/
├── contract/
│   └── service.go                # 无依赖服务名与跨服务基础契约
├── models/
│   ├── product.go                # 商品模型、名称哈希、字段和唯一性校验
│   ├── product_persistence.go    # 商品查询与名称唯一性操作
│   ├── order.go                  # 订单模型、价格快照和秒级业务哈希
│   ├── order_persistence.go      # 下单、本人查询、所有权查询和删除
│   └── data_action.go            # 模型层共享 IDataAction 持久化边界
├── api/
│   ├── dto/                      # 面向 HTTP、OpenAPI 和 WebSocket 的扁平 DTO
│   ├── manage/                   # 商品完整 CRUD、订单只读管理
│   ├── public/                   # 无需身份的商品查询
│   └── private/                  # 下单、本人订单、删除和用户 WebSocket
├── service.go                    # IService 路由组合根
└── main/main.go                  # WebServer 启动组合根

examples/integration/
├── helpers.go                    # 通用真实进程、HTTP、TestToken、WebSocket 能力
└── 01-simple-shop/
    ├── helpers_test.go           # 商城专属 Suite、DTO 和业务辅助方法
    ├── manage_test.go            # Manage command 集成测试
    ├── public_test.go            # Public API 集成测试
    └── private_test.go           # Private API 与 WebSocket 集成测试
```

进阶支付样例的关键目录：

```text
examples/02-shop-payment/
├── models/                       # 商品、订单、支付类型、支付流水和 IDataAction 事务边界
├── business/                     # 所有权、引用保护、金额计算和支付状态迁移
├── api/dto/                      # 商品、支付类型和统一订单 DTO
├── api/manage/                   # Manage hook、状态视图和受控命令
├── api/public/                   # 商品与启用支付类型查询
├── api/private/                  # 下单、支付、撤销、本人订单和 WebSocket
├── service.go
└── main/main.go

examples/integration/02-shop-payment/
├── helpers_test.go
├── manage_test.go
├── public_test.go
└── private_test.go
```

继承、性能与身份样例的关键目录：

```text
examples/03-shop-inheritance/
├── models/                       # ShopModel -> BaseDataModel/BusinessModel -> 具体模型
├── business/                     # 供应商/商品联合有效性、订单和支付规则
└── api/manage/                   # ShopManage -> BaseDataManage/BusinessManage -> 具体 Manage

examples/04-shop-performance/
├── api/public,api/private/       # RouterInfo 结果缓存与可信缓存键
├── api/manage/                   # EventBridge 主动失效
├── business/                     # 下单事实缓存与 SingleFlight
└── models/order_write_store.go   # Badger 可靠本地写、Group Commit、SQLite 同步

examples/05-shop-casdoor-rbac/
├── models/{common,basedata,transaction,identity,internal/store,schema}
├── business/{basedata,transaction,identity}
├── api/manage/{common,basedata,transaction,audit}
├── auth_hooks.go                 # 签发、请求、身份事件三 Hook
└── models.go/business.go/manage.go # 根包兼容门面

examples/06-shop-microservices/
├── contract,dto                  # 无反向依赖的跨服务契约
├── user-service                 # 买家 facade 与地址权威
├── supplier-service             # 供应商/商品权威与受限 Public API
├── order-service                # 订单/支付事实与 Outbox
└── main,deploy                  # 同进程调试和三进程部署
```

单元测试与实现同目录；跨子包继承/兼容契约测试留在根包；真实进程、HTTP、WebSocket 和 Casdoor 测试只放 `examples/integration/<service>`；固定样本放 `testdata/`。

示例 06 的每个服务也按示例 05 的模型目录拆分：`models/common` 放服务级基础模型、数据库名和 TraceID，`models/basedata` 放供应商、商品、支付类型、用户、地址等基础资料，`models/transaction` 放订单、支付、投影和 Outbox/Inbox 等业务事实，`models/internal/store` 统一 `IDataAction` 和事务互斥，`models/schema` 统一建表，根 `models` 只保留 `models.go` 兼容门面，不放具体模型或持久化实现。具体模型通过基础资料模型或业务事实模型继承服务级基础模型，自动获得 `GetLocalDBName/GetRemoteDBName` 和 `TraceID`；不要在每个具体模型上重复声明库名或 TraceID 字段。写路径从入口 `req.GetTraceId()` 传到 business，再写入业务事实、Outbox、Inbox 和投影；事件 Metadata 同步携带 TraceID，但 EventID 仍负责事件幂等。

示例 06 的 `api/manage` 目录也必须按示例 05 拆分：`api/manage/common` 放权限、owner 限域和全服务最基础 `ServiceManage[T]`，`api/manage/basedata` 放 `BaseDataManage[T]`、基础资料 Manage 与受控命令，`api/manage/transaction` 放 `TransactionManage[T]`、订单、支付、投影等业务 Manage，`api/manage/audit` 只在存在审计/身份事件时使用；根 `api/manage` 只保留 `manage.go` 兼容门面和路由注册入口。

多服务场景必须按服务建立独立 Manage 继承树：`common.ServiceManage[T]` 继承框架可选 `manage.HookedManageService[T]`，`basedata.BaseDataManage[T]` 和 `transaction.TransactionManage[T]` 继承本服务 `ServiceManage[T]`，每个具体 Manage 再继承本目录的基础资料或业务基座。具体 Manage 不直接嵌入 `manage.ManageService[T]`，也不重复实现服务级权限、owner 限域、禁用主体拦截、分页、审计或日志；这些横切逻辑必须在 `common.ServiceManage[T]` 或更靠近根部的抽象基座实现一次。具体 Manage 只暴露“业务目标对象是谁”和“业务动作怎么做”，否则复杂系统会在权限或日志调整时到处修改。自定义 Manage 命令不要引入命令专用 Hook 旁路；命令 `Do` 先调用 owner `DoBefore`，通过服务级权限/限域后再调用 business。

Manage 日志参考示例 05 的 `ShopManage.logManageResult`：统一使用 `logx.Infow("shop_manage_operation_failed", ...)` 和 `logx.Infow("shop_manage_operation_succeeded", ...)`，字段保持 `owner`、`phase`、`service`、`route`、`trace_id`、失败时 `code`。不要按服务名发明 `shop_user_manage_operation_*`、`shop_supplier_manage_operation_*` 等新事件，也不要记录 token、请求/响应 body、SQL 或对象 dump。

新增或重排文件默认按 struct 拆分：一个业务 struct 一个文件。多个模型、多个 Manage、多个 Router 或多个 DTO 不应聚在一个大文件里；只有紧密配套的小型测试桩或私有辅助结构可以与被测代码同文件。

普通 CRUD 和简单 API 以 `01-simple-shop` 为准；出现以下任一需求时，以 `02-shop-payment` 为参考：

- API 需要组合多个模型操作；
- 两个以上模型必须在同一事务内更新；
- 业务状态只能沿有限状态机推进；
- Manage 需要引用删除保护、字段冻结或启用/禁用命令；
- 后台命令成功后需要通知最终用户 WebSocket。

## 业务层与状态机

业务复杂度超过单模型查询或写入时，增加无请求状态的 `business` 包：

```text
API / Manage command -> business service -> models -> IDataAction
```

- API 只负责绑定参数、读取可信身份、转换 DTO 和提交后的观察通知。
- business 负责所有权、金额、引用关系、状态迁移和事务编排。
- models 负责实体规则、查询和持久化，不引用 API、DTO、Manage 或 business。
- `IDataAction` 仍只在 models 持久化边界选择，不能沿 Service -> API -> business 传递。

支付样例使用 `OrderStatus` 和 `PaymentStatus` 分离订单生命周期与资金阶段。支付失败重试创建新 `PaymentRecord` 并递增 `Attempt`，旧流水只读保留；后台确认支付、标记失败和确认退款都在事务内重新读取订单与流水，不能信任页面提交的旧状态。

Manage 扩展遵循以下顺序：

1. 通用 CRUD 继续使用 `ManageService[T]` 和 `ModelList`；
2. 复杂服务可使用 `manage.HookedManageService[T]` 作为可选辅助基类，把 `DoBefore/DoAfter/SearchBefore/SearchAfter` 分派到 `OnView/OnAdd/OnEdit/OnRemove/OnSearch` 等细粒度 Hook；
3. 服务级 `ShopManage` 或 `ServiceManage` 统一处理授权、日志、分页和查询约束；具体 Manage 只提供 owner column、写入目标 scope 或业务命令 Hook，不重复调用服务级鉴权函数；
4. `BaseDataManage` 与 `BusinessManage`/`TransactionManage` 实现模型类别规则，具体 Manage 只重写差异 Hook；需保留父级规则时必须显式先调父级。
5. 状态字段通过 `ViewFieldModel` 和 `ComBoxValue` 显示中文；
6. 状态迁移使用自定义 Router，并在 `ViewCommandModel` 中配置按钮；
7. 自定义 Router 的 `Do` 先调用 owner `DoBefore` 复用服务级权限和限域，再调用 business，不直接修改模型，也不另造 `CommandBefore` 一类命令专用 Hook。`ParseAfter/ValidationAfter` 不是常规业务分层点，只在框架解析阶段确有特殊需求时使用。

支付流水示例不注册通用 Add/Edit/Remove，只注册 View/Search 和确认支付、支付失败、确认退款命令。前端按钮只是能力提示，服务端必须再次校验当前状态。

Casdoor 双域和业务授权以 `examples/05-shop-casdoor-rbac` 为标准样例：`ShopService` 同时实现签发前 `IAuthHookProvider`、Router 前 `IAuthRequestHookProvider` 和撤销事实落地后 `ICasdoorEventHookProvider`。Auth 域只签发普通用户角色，Manage 域只签发管理员角色；角色由已验证 `AuthType` 派生，不接受请求字段或 Casdoor 自定义字符串直接决定。集成测试模板位于 `examples/integration/05-shop-casdoor-rbac`，使用本地 Fake Casdoor 真实经过域配置、OAuth callback、Refresh、REST、WebSocket 和 Webhook，不使用 TestToken 代替身份生命周期验证。

## 路由基础契约

### 无依赖服务契约

每个业务服务建立最底层 `contract` 包，供本服务各层和其他服务安全引用。该包不得导入其他包，不保存数据库模型、ServiceContext、RouterInfo、连接、请求或用户状态。

```go
package contract

const ServiceName = "shop"
```

`IService.ServiceName()` 返回这个唯一常量。服务名用于配置、ServiceContext 注册和内部服务本地/远程分流；不要在各 API 中重复字符串。

路由 Path 不放入 contract。任何 Router 都通过 `RouterInfo()` 提供 Path、Method、认证类型和服务归属。服务完成注册后，框架按路由类型身份返回 ServiceContext 持有并冻结的 RouterInfo；服务关闭时注销。相同 Router 类型如果同时归属多个 ServiceContext，无服务上下文的 `RouterInfo()` 调用会 fail closed，调用方必须从目标 ServiceContext 解析。

路由元数据必须在 `RouterInfo()` 构造表达式中通过 Option 一次声明，注册后只读：

```go
func (own *GetProducts) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(
		own,
		router.WithMethod(http.MethodGet),
	)
}
```

读取时使用 `GetPath()`、`GetMethod()`、`GetAuth()`、`GetServiceName()`、`GetPathType()` 等 Getter。当前导出的同名字段仅为旧消费方源码兼容保留，已废弃；新代码不得直接读写。后续破坏性版本会将这些冻结属性改为非导出字段，因此不要依赖字段赋值。Option 只在首次创建且尚未 Freeze 时执行；再次调用 `RouterInfo()` 返回已注册单例，不会重放 Option 或改写元数据。

内部调用可先判断目标服务是否位于当前进程：

```go
serviceName := contract.ServiceName
info := targetAPI.RouterInfo()
if target := router.GetContext(serviceName); target != nil {
	// 通过目标 ServiceContext 中的已注册 RouterInfo 走本地调用链。
} else {
	// 使用 serviceName、info.GetPath() 和服务发现结果走 Transport。
}
```

`GetContext(serviceName)==nil` 只表示目标服务不在当前进程，不表示远程节点一定存在。远程服务发现失败必须明确返回错误；不得缓存 ServiceContext 指针，也不得直接调用目标 API 的 `Do()` 绕过完整执行链。

所有普通 API 实现：

```go
type IRouter interface {
	Parse(req types.IRequest) error
	Validation(req types.IRequest) error
	Do(req types.IRequest) (interface{}, error)
	RouterInfo() *types.RouterInfo
}
```

职责：

- `Parse`：绑定 JSON/query，不执行业务副作用。
- `Validation`：校验身份、参数和调用前条件，不写数据库。
- `Do`：查询事实数据并执行业务副作用。
- `RouterInfo`：无自定义元数据时使用 `router.DefaultRouterInfo(own)`；需要覆盖 Method、Path、Auth、PathType、PoolSize 时使用 `router.DefaultRouterInfoWithOptions(own, options...)`。旧构造函数保留精确签名，保证函数值和既有消费方兼容。

路径：

```text
public/private: /api/{service}/{structLower}
manage:         /api/manage/{service}/{manageLower}/{operationLower}
server manage:  /api/servermanage/{structLower}
```

`api/public` 与 `api/private` 只决定认证策略，不进入 URL。private 身份只能来自：

```go
userID, userName := req.GetUser()
```

禁止从 body/query 的 UserID 推断认证身份，也不要把当前请求、用户、trace 或响应保存在 `RouterInfo`、ServiceContext 或其他共享对象中。

## 模型默认能力

### 选择 Model 或 BaseModel

普通业务记录使用 `entity.Model`：

```go
type Product struct {
	*entity.Model
	Name  string
	Price decimal.Decimal
}

func NewProduct() *Product {
	return &Product{Model: entity.NewModel()}
}

func (own *Product) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}
```

只有具有稳定唯一 `Code`、`Name` 和资料状态语义时才使用 `BaseModel`。`BaseModel.GetHash()` 基于 Code；没有 Code 的模型不要为了复用字段而误用 `BaseModel`。

嵌入指针必须在显式构造器和 `NewModel()` 中初始化。前者供业务代码使用，后者供 ModelList 反射创建实例。

### 哈希表达业务唯一性

`GetHash` 不是随机值，应表达真实的业务唯一约束：

- 商品以规范化后的名称生成哈希，因此名称不能重复。
- 订单以 `UserID + ProductID + CreatedAt(UTC 秒)` 生成哈希，因此同一用户同一商品每秒只能创建一次订单。
- 时间参与哈希时，保存值和哈希值必须使用同一精度，不能一个保留纳秒、一个截断到秒。

数据库唯一约束是并发下的最终防线，`AddValid`/`UpdateValid` 仍应提前返回清晰的公开业务错误：

```go
func (own *Product) AddValid() error {
	return own.validate(true)
}

func (own *Product) UpdateValid(_ interface{}) error {
	return own.validate(true)
}
```

校验应同时覆盖字段格式、数值范围和业务唯一性。公开错误使用框架的类型化公开错误，不直接暴露数据库错误文本。

### 模型持久化边界

Manage CRUD 使用 `entity.NewModelList[T](nil)`。public/private 不直接依赖 GORM、SQLite 或具体数据库类型，而是调用模型封装的方法，例如：

```go
product, err := models.NewProduct().FindByID(productID)
orders, err := models.NewOrder().QueryByUser(userID)
order, err := models.NewOrder().FindOwned(orderID, userID)
err = order.Delete()
```

模型方法内部依赖 `types.IDataAction`。数据库实现只在模型持久化边界选择，不沿 Service -> API -> Model 逐层传递。标准样例在 `models/data_action.go` 中延迟创建并共享 IDataAction：

```go
var (
	dataActionOnce sync.Once
	dataAction     persistencetypes.IDataAction
)

func getDataAction() persistencetypes.IDataAction {
	dataActionOnce.Do(func() {
		dataAction = entity.GetGlobalSqliteInstance(NewProduct().GetLocalDBName())
	})
	return dataAction
}
```

这里共享的是无请求状态的数据访问能力。模型实例、当前用户、查询条件和响应不得放入该单例。

SQLite 默认 mmap 预算为 256MiB/实例，可通过 `Sqlite.MmapSize` 覆盖；负值关闭。不得恢复机器级 30GB 默认。

## DTO 与响应契约

public/private API 返回独立 `api/dto` 类型，不直接序列化持久化模型。原因包括：

- 持久化模型可能具有很深的嵌入关系和内部字段。
- 对外字段、名称和时间格式需要稳定，不应随数据库模型重构漂移。
- HTTP、OpenAPI 与 WebSocket 可以复用同一份公开结构。

标准样例使用：

- `dto.ProductResponse`：只暴露 ID、名称和价格。
- `dto.OrderResponse`：暴露订单快照、数量、用户和创建时间。
- `OrderResponse.Action`：HTTP 响应为空；WebSocket 事件复制 DTO 后设置 `created` 或 `deleted`。

普通 API 实现 `IRouterResponse`，让 OpenAPI 在不执行路由的情况下获得响应结构：

```go
func (own *GetProducts) GetResponse() interface{} {
	return []*dto.ProductResponse{}
}
```

DTO 转换集中放在 `api/dto`，不要放入通用集成测试 helpers，也不要让测试 DTO 进入生产包。

## Manage API

### 完整 CRUD

```go
type ProductManage struct {
	*manage.ManageService[models.Product]
}

func NewProductManage() *ProductManage {
	own := &ProductManage{}
	own.ManageService = manage.NewManageService[models.Product](own)
	return own
}
```

必须把真实 owner 传给 `NewManageService`，否则 `ViewModel`、Parse/Validation/Do 和 Search hooks 不会落到自定义类型。

商品管理注册 `view/search/add/edit/remove`，通过模型的 `AddValid`/`UpdateValid` 校验名称、价格和唯一性。自定义操作以值嵌入 `manage.Operation[T]`，不要嵌入指针。

### 只读管理

订单管理只注册 `view/search`，不注册 `add/edit/remove`。只读不是依赖 handler 内拒绝写入，而是根本不把写 command 暴露为路由。集成测试应断言未注册 command 返回 404。

`ModelList` 只用于 Manage 和框架管理能力。public/private 使用面向业务语义的模型方法，不直接暴露通用列表操作。

## Public API

Public API 无需身份，但仍执行参数解析、校验、类型化错误和 DTO 转换。

`GetProducts` 展示标准可选筛选模式：

- `id` 为空时不按 ID 限制；有值时精确匹配。
- `name` 为空时不按名称限制；有值时模糊匹配。
- 两者同时存在时组合筛选。
- 条件全部为空时返回全部可下单商品。
- 非法 ID 返回稳定的公开校验错误。

不要为了 public 查询复用 Manage 的列表请求/响应结构；它们面向不同调用方和兼容性契约。

## Private API

### 创建订单

下单只接收 `productID` 和 `quantity`。UserID 从 `req.GetUser()` 获取，商品名称和价格从数据库中的当前商品读取。订单保存商品 ID、名称和价格快照，因此商品后来改名或改价不会改变历史订单。

标准顺序：

1. `Parse` 绑定商品 ID 与数量。
2. `Validation` 验证可信身份、商品 ID 和正数数量。
3. `Do` 查询商品事实数据。
4. 创建订单并设置框架 ID、UserID、商品快照和秒级 CreatedAt。
5. 持久化成功后才发布 `created` WebSocket 通知。
6. HTTP 返回不带 `action` 的订单 DTO。

### 查询本人订单

`GetOrders` 不接受 UserID 参数，只按可信身份调用 `QueryByUser`。响应使用订单 DTO，不能返回其他用户订单。

### 删除本人订单

删除先以 `ID + UserID` 查询所有权，再物理删除。不存在与不属于当前用户返回同一公开错误，避免泄露其他用户订单是否存在。持久化成功后发布 `deleted` 通知。

## RouterInfo 缓存与高性能写

API 只通过 `info.UseCache(ttl)` 声明启用结果缓存。未配置 `RouteCache` 时默认使用 local L1；Badger L2 和 shared Redis L3 才需显式配置。

- Public 缓存键覆盖所有筛选维度；Private 键中的 UserID 只取自 Token 解析后的认证上下文。
- L1/L2/L3 命中统一返回 `json.RawMessage`。L1 `MaxBytes=0` 按进程/容器有效内存 2% 解析为 16–256 MiB 共享预算；`MaxEntries=0` 自动解析；超过 `MaxValueBytes` 的响应正常返回但不进入任何缓存层。
- 商品、供应商、支付类型、订单状态变更后，通过 ServiceContext 专属 EventBridge 执行主动失效；TTL 只是兜底。
- 同键冷加载使用 RouteCache/`syncx.SingleFlight`，不在 API 自建锁和队列。

`PrefixedBadgerDB` write-behind 与 RouterInfo L2 是两种不同能力：L2 可重建；write-behind pending 在远端数据库确认前是业务事实。高 TPS 路径必须等 Badger `SyncWrites` 成功后才返回，后台同步 SQLite 成功后再删除本地副本。基准必须与对照示例同机、同口径、多轮运行，同时报告 QPS/TPS、p50/p95/p99、错误率、pending 收敛和磁盘上限。

详细运行时契约见 `docs/codex/ROUTERINFO_RUNTIME_GUIDE.md`，容量契约见 `docs/codex/PERFORMANCE_SLO_BASELINE.md`。

## WebSocket 最终用户订阅

WebSocket 只面向最终外部用户。内部服务之间不使用 WebSocket，内部请求使用 TransportSelector，内部事件使用每个 ServiceContext 所属的 EventBridge。

订阅使用真实路由路径：

```text
/api/shop/getorders
```

private WebSocket 路由至少需要以下职责：

- `IWebSocketUserIdentity`：`SetUserID` 接收 WebSocket 登录会话解析出的可信身份，`GetUserID` 返回已绑定身份。private 路由使用 WebSocket 时必须实现。
- `IRouterHashKey`：按 UserID 生成稳定 hash，将不同用户放入不同订阅组。
- `IWebSocketRouterNotice`：校验消息 DTO，并只向消息所属用户投递。

框架为每次订阅直接创建并持有独立路由实例，直到退订或连接关闭；WebSocket 订阅实例不进入普通请求对象池。路由没有额外启动/停止资源时，不要为了形式实现空的 `IWebSocketRouter` 生命周期回调。

通知应复用 HTTP DTO：

```json
{
  "action": "created",
  "id": "123",
  "productID": 1,
  "productName": "示例商品",
  "unitPrice": "39.8",
  "quantity": 2,
  "userID": "user-a",
  "createdAt": "2026-07-14T10:00:00Z"
}
```

不要再包一层 `order`，也不要把 action 写回 HTTP DTO 原对象。通知过滤失败、类型不匹配或用户不匹配时直接不投递。

跨节点模式要求 ClusterProvider 和 CrossNodeNoticeBroker 已由 ServiceContext 启动。forwarder 按服务名隔离；IPv6 地址通过 `net.JoinHostPort`；非 2xx 转发视为错误。

worker 生命周期由通知系统持有；队列满、filter timeout、panic 和 shutdown timeout 是 error，worker 启停是 debug。不得记录消息体。

## Service 与启动组合根

业务 Service 只组装路由：

```go
type ShopService struct{}

func (*ShopService) ServiceName() string {
	return contract.ServiceName
}

func (*ShopService) Routers() []types.IRouter {
	routers := make([]types.IRouter, 0, 11)
	routers = append(routers, manage.NewProductManage().Routers()...)
	routers = append(routers, manage.NewOrderManage().Routers()...)
	routers = append(routers,
		&public.GetProducts{},
		&private.AddOrder{},
		&private.GetOrders{},
		&private.DeleteOrder{},
	)
	return routers
}

func (*ShopService) SubscribeRouters() []*types.ObserveArgs { return nil }
```

`SubscribeRouters` 是旧 Router 生命周期观察订阅兼容入口，不是外部用户 WebSocket 订阅，也不是新业务事件订阅入口。没有内部观察者时返回 nil；新业务统一在 `Start()` 中使用 `sc.SubscribeEvent(...)`。

main 只负责创建 WebServer、注册 Service 和 ServerOption，然后启动。单服务运行配置由框架首次运行生成，示例和集成测试不提交临时运行配置。示例 06 为了让同进程和三进程使用完全相同的 Redis 契约，由 `bootstrap.ServiceConfig` 在组合根显式构造配置，仍不提交运行后 JSON。

```go
server := run.NewWebServer()
server.AddIService(&simpleshop.ShopService{}, &types.ServerOption{
	IsCors:     true,
	OriginCors: []string{"http://localhost:8000"},
})
server.Start()
```

CORS fail closed：`IsCors=true` 必须显式 origin；`*` 只能由调用方主动选择。

## 多服务调用与事件

以 `examples/06-shop-microservices` 为标准模板：

- 稳定服务名和事件名放根 `contract`；跨服务 JSON 结构放根 `dto`，不共享 Model。
- 调用方直接构造目标服务已注册的 Public API，不建保存地址的 client，也不复制 `api/call` 路由。如果 Go 目录名与 `IService.ServiceName()` 不同，目标 API 必须在 Freeze 前同时声明 `router.WithServiceName(contract.XxxServiceName)` 和稳定 `WithPath`。
- 内部专用 Public 用 `router.WithInternalCallers(...)` 声明允许服务；冻结后通过 `GetInternalCallers()` 读取，兼容快照/OpenAPI 记录 `x-internal-callers`。
- `req.CallService` 先查同进程 ServiceContext，再查 ClusterProvider 健康快照。新链路不读 `AttachServices`；无节点时 fail closed。
- 同进程调用方身份来自源 ServiceContext。同步跨进程调用默认使用 gRPC；服务端只在已验证客户端证书 SAN 等于载荷 `SourceService` 时注入可信身份。HTTP、Header、请求字段、无证书和 SAN 不匹配都不能建立内部身份，并在 Parse 前拒绝。
- 客户端按 endpoint 复用 go-zero `zrpc.Client`；Core Resolver 仍是唯一节点发现权威，不启用 zrpc 自带发现。
- 同进程模式只供调试；部署演示必须以独立进程、独立 SQLite 和 mTLS gRPC 再验收一次，并断言 HTTP 调用计数为零。
- HTTP 仅可作为显式发送前 fallback；gRPC 开始发送后不得跨协议重试。内部异步事件使用 EventBridge，WebSocket 只面向最终用户。
- Redis 发现和 EventBridge 使用不同 Prefix。业务服务只声明 `sc.UseOutbox(models.OutboxStore{})` 启用本服务可靠发布；`OutboxStore` 只实现 `LoadPending(ctx, limit)` 和 `MarkPublished(ctx, message)`，不关心当前服务名、消费者或 MQ。当前服务名由 `ServiceContext` 写入事件 Source，Subject/EventType/Payload/TraceID 来自 Outbox 记录。
- 业务服务只用 `sc.SubscribeEvent(event.Subscription{Subject, EventType, Reliable, Handler})` 订阅内部事件，不直接注册 `SubscribeControl` 和 `SubscribeExternalControl` 两套订阅。`Subject` 决定外部通道，`EventType` 是可选过滤条件；`EventType` 为空表示订阅该 Subject 下全部事件类型。`Reliable=true` 时 Handler 返回 error 会阻止当前逻辑服务消费组 ACK。
- 控制事件的 Handler 返回 error，成功后才 ACK；失败留 pending 并允许同组 reclaim。多个服务订阅同一 Subject 时按逻辑服务消费组独立 ACK；同一服务内多个可靠 Handler 全成功才 ACK。
- 生产写路径必须同事务写业务事实和 Outbox；消费方以 EventID 写 Inbox 或等价幂等事实。发布方只负责发布事实，不知道也不等待消费者处理完成。
- User 下单必须提供业务 `requestID`；事实服务用 `{UserID}:{requestID}` 唯一约束和请求指纹收敛并发重试。
- Supplier 使用统一 Manage Hook 同时处理本人和管理员权限；Order 可靠事件按 `OrderID` 幂等写本地永久 `SupplierOrder`，删除 Hook 只查询该投影，禁止同步查询远端判断能否删除。
- WebSocket 仅把本服务已消费的订单摘要推送给当前最终用户，不承担服务间传输和离线积压。

典型声明：

```go
func (g *GetProducts) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(g,
		router.WithServiceName(contract.SupplierServiceName),
		router.WithPath("/api/"+contract.SupplierServiceName+"/getproducts"),
		router.WithInternalCallers(contract.UserServiceName, contract.OrderServiceName),
	)
}
```

负向测试必须覆盖普通 HTTP、缺少可信身份、错误服务、伪造 `SourceService`、无客户端证书和 SAN 不匹配，并断言 `Parse/Validation/Do` 均未执行。兼容性变更同时运行 `go test ./internal/compat`、`./scripts/test.sh api-compat` 和 `./scripts/test.sh release-contract`。

验收必须同时运行 `examples/integration/06-shop-microservices` 和 `examples/integration/06-shop-microservices-three-process`。

反向代理必须配置 `ServerConfig.TrustedProxies` 的 IP/CIDR。默认空表示忽略 XFF/X-Real-IP；本地/private peer 携带 forwarding header 且没有信任策略时 fail closed。

## 标准集成测试模板

集成测试是平台服务的标准能力，不是可选示例。为新服务创建集成测试时，必须优先复用以下两层模板。

### 公共测试能力

`examples/integration/helpers.go` 负责与业务无关的能力：

- `StartProcess(ProcessOptions)`：编译并启动真实服务进程。
- 为服务分配隔离端口和系统临时目录。
- 捕获服务日志并在失败时输出。
- `RequestJSON`：通过真实 HTTP 调用路由并解析统一响应信封。
- `TokenFor`：调用框架内建 `/api/servermanage/testtoken` 获取普通用户或管理员令牌。
- `WriteWebSocket`、`ReadWebSocket`、`StreamWebSocket`：使用真实 WebSocket 协议测试订阅和事件。
- `Stop`：关闭进程并清理临时目录。

不要在每个服务里重新实现进程管理、端口分配、TestToken、HTTP 信封或 WebSocket 通信。只有当验收目标本身是 Casdoor 登录、刷新、撤销或 Webhook 时，才以 `examples/integration/05-shop-casdoor-rbac` 为模板，在服务专属 Suite 中用 Fake Casdoor callback 覆盖 `TokenFor`；该测试不得回退到 TestToken。

### 服务专属 Suite

以 `examples/integration/01-simple-shop/helpers_test.go` 为模板：

```go
type serviceSuite struct {
	*integration.Suite
}

func startServiceSuite() (*serviceSuite, error) {
	base, err := integration.StartProcess(integration.ProcessOptions{
		BuildPackage: "./path/to/service/main",
		BinaryName:   "service-name",
		TempPrefix:   "core-service-name-",
		ServiceCount: 2,
		ServiceIndex: 1,
	})
	if err != nil {
		return nil, err
	}
	// 等待本服务真实业务路由可用；失败时 Stop。
	return &serviceSuite{Suite: base}, nil
}
```

服务专属目录只保存：

- 当前服务的测试 DTO。
- 业务路由辅助方法。
- 就绪探测。
- 业务 WebSocket 登录/订阅和事件解析。

业务 DTO 放在对应服务集成测试目录，不放入 `examples/integration/helpers.go`。

### TestMain 生命周期

一个服务测试目录启动一个真实进程，供三类测试共用：

```go
func TestMain(m *testing.M) {
	created, err := startServiceSuite()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	suite = created
	code := m.Run()
	if code != 0 {
		suite.PrintLog()
	}
	suite.Stop()
	os.Exit(code)
}
```

测试必须等待认证和真实业务路由可用，不能只等待端口监听。框架应在临时目录首次启动时自动生成配置；测试可验证必要配置存在，但不向源码目录写运行配置。

### 文件和测试分组

```text
manage_test.go
public_test.go
private_test.go
```

每个 API 或 Manage command 使用独立测试函数，整组入口保留并调用全部子测试：

```go
func TestPrivateAPIs(t *testing.T) {
	t.Run("AddOrder", testAddOrderAPI)
	t.Run("GetOrders", testGetOrdersAPI)
	t.Run("DeleteOrder", testDeleteOrderAPI)
	t.Run("GetOrdersWebSocket", testGetOrdersWebSocketAPI)
}
```

Manage 按 command 拆分，而不是把 view/search/add/edit/remove 堆在一个测试函数中。Public/Private 按 API 拆分。这样既能单独定位失败，也能一次运行整类能力。

示例 06 三进程 UAT 还必须按业务角色拆文件：`buyer_uat_test.go` 放普通用户注册模拟、资料/地址维护、下单、支付、本人订单查询和其他用户隔离；`supplier_uat_test.go` 放供应商注册模拟、商品维护/上架、本供应商订单投影查询和其他供应商隔离；`admin_uat_test.go` 放平台管理员支付类型配置和全量订单查询。完整三角色流程测试只负责启动三个真实进程并组合这些角色步骤；共享查找、进程启动、Redis consumer group 等跨角色辅助可以放独立 helper 文件。不要把三种角色的 API 调用、断言和异常用例全部堆在一个 UAT 大文件中。

### 最低验收范围

Manage：

- 管理员鉴权。
- view/search 元数据与列表。
- add/edit/remove 的成功和业务校验。
- 只读 Manage 的写 command 未注册。

Public：

- 空筛选、单条件和组合条件。
- 非法参数公开错误。
- DTO 不泄露持久化字段。

Private：

- 未认证请求被拒绝。
- UserID 来自令牌，不接受客户端伪造。
- 资源所有权和跨用户隔离。
- 业务事实快照、唯一性和删除语义。
- HTTP DTO 不包含仅供事件使用的 action。

WebSocket：

- 匿名订阅被拒绝。
- 登录后按真实 RouterInfo 路径订阅。
- 创建和删除事件结构正确。
- 事件只投递给当前用户，其他用户无消息。
- 连接和读取具有明确超时，测试结束关闭连接。

### 标准命令

```bash
go test ./examples/integration/01-simple-shop -count=1
go test ./examples/integration/01-simple-shop -count=10
go test -race ./examples/integration/01-simple-shop -count=1
```

新服务将路径替换为自身集成测试目录。涉及并发、身份隔离或 WebSocket 时必须运行 race；需要稳定性证据时运行多次，不用无断言 sleep 或 retry 掩盖失败。

## PrefixedBadgerDB

- 纯缓存默认损坏策略为 `CorruptionPolicyFail`；只有确认数据可从远端完整重建时才显式使用 `CorruptionPolicyResetCache`。
- 可靠写回使用 `EnableWriteBehind`，配置必须满足 `SyncWrites=true`、`DetectConflicts=true`、`CorruptionPolicyFail`。
- `DefaultSharedConfig` 默认 `SyncWrites=false`，面向共享缓存；write-behind 必须显式启用持久写并通过 `EnableWriteBehind` 校验。
- `SetSyncDB` 已废弃，仅保留编译兼容；其绑定错误会在后续写入和关闭时返回。
- 待同步记录禁止 TTL。`Close` 返回 `PendingSyncError` 表示本地仍是临时事实源，不能把目录当缓存删除。
- 语义为 at-least-once，远端操作必须幂等。同 key 写入会合并状态，不适用于资金流水或审计事件；不可合并事件使用唯一事件 ID 的 JetStream/outbox。

## Cluster、Transport、MQ 与事件

- Local cluster：`Stable`。
- etcd/Consul：`Conditional`，需要显式配置和外部依赖。
- 内部同步传输默认 gRPC，HTTP 只作为显式备用；自定义 Socket 已删除，迁移见 `docs/codex/GRPC_TRANSPORT_MIGRATION.md`。
- gRPC Client 复用 zrpc，Server 因 go-zero v1.10.2 无法独立停止单 listener 而保留薄 grpc-go 生命周期适配；跨主机生产使用 mTLS，已有双向身份的服务网格使用 mesh。
- QUIC 和 MQ transport：`Unsupported`，配置校验拒绝。
- MQ/EventBridge：Redis Streams、NATS JetStream 为 `Conditional`。
- JetStream 可靠数据库写路径先阅读 `docs/codex/NATS_JETSTREAM_WRITE_PATH_GUIDE.md`；当前 Provider 已有 publish ACK、消息 ID 去重和显式 ACK，但重试、死信、pull consumer 与生产 stream 参数尚未实现。
- Kafka/RabbitMQ/RocketMQ：无内建 Provider；应用可在 `MQProvider` 后注册自定义 `ProviderFactory`。

go-zero `core/queue` 只用于进程内队列，不能替代 Broker。

## Casdoor 认证生命周期

- 前端先调用 `/api/casdoor?type=auth|manage` 获取对应 Casdoor 域配置和 `background_callback_url`；回调固定为 `/api/casdoor/callback`，不要再调用已删除的 `/api/callback`。
- Auth 与 Manage 分别配置 Casdoor YAML、Client、Access/Refresh Secret 和 Webhook Secret，任何 Secret 都不得复用。框架通过 ServiceContext 持有 DomainClient，不使用 Casdoor 全局 SDK。
- `casdoor.NewAuthHandler` 仅为公共 API 兼容保留并固定 fail closed，不得自行挂载。`TokenParseWithClient` 仅能解析原始 Casdoor JWT，解析成功不等于授权成功；生产请求必须经过 ServiceContext 注册的 Access Token、认证域、撤销世代和业务 Hook 完整链路。
- Callback 在线读取 Casdoor 用户并验证 Owner、Subject、`IsForbidden`、`IsDeleted`，随后以撤销权威当前世代签发 Access/Refresh。被 logout 的用户只有再次通过在线 Callback 才能解除 blocked，旧 Token 仍因世代落后而失效。
- `/api/refresh` 不访问 OAuth，但必须验证 Refresh 用途、AuthType、Provider、Subject、Generation 和当前在线用户状态；Auth Token 不能访问 Manage，Manage Token 不能访问 Private。
- `/api/casdoor/webhook?type=auth|manage` 使用对应域独立 Bearer Secret。Webhook 是控制面，不记录 Header/Payload，成功仅表示撤销事实已持久化且控制事件被 EventBridge 接受。
- 服务可选实现 `IAuthHookProvider`（签名前）、`IAuthRequestHookProvider`（验签及撤销校验后、Router 前）和 `ICasdoorEventHookProvider`（撤销事实提交后的异步业务通知）。只有类型化 `PublicError` 可向前端公开安全业务消息，普通错误统一 500 脱敏。
- local 模式使用 Badger，适合单实例。shared 模式使用 Redis 权威且必须启用 MQ `event-stream`；Redis/EventBridge 故障时认证面 fail closed，Public REST 保持可用。
- WebSocket 登录与每次认证订阅都重新验证 Access Token 和撤销权威；更高世代、blocked 事件或共享权威不可用会关闭旧 Casdoor 连接。

验证命令：

```bash
./scripts/test.sh security
CORE_TEST_REDIS_ADDR=127.0.0.1:6379 ./scripts/test.sh integration-casdoor-auth
```

## 日志与错误

- `logx.Infow`：生命周期、切换、成功降级。
- `logx.Debugw`：重试、路由注册、worker 和高频细节。
- `logx.Errorw`：最终失败、数据风险、panic、关闭失败。
- `logx.Sloww`：测量超阈值。

请求/跨服务失败携带 `trace_id`、service、route/target、operation 和 error。错误由拥有重试、降级、响应或终止决策的边界记录一次。

禁止记录凭据、token、cookie、TOTP、完整 payload/body/response、DSN、SQL、参数和对象 dump。

## 测试与发布

```bash
./scripts/test.sh quick
./scripts/test.sh security
./scripts/test.sh config-contract
./scripts/test.sh persistence-unit
./scripts/test.sh performance-contract
./scripts/test.sh release-contract
```

外部依赖默认 skip：

```bash
./scripts/test.sh integration-external-docker
./scripts/test.sh integration-persistence
```

发布前不得自动创建 tag。开发消费方可临时引用分支或精确 commit：

```bash
go get github.com/digitalwayhk/core@codex/optimize-code-cleanup
go get github.com/digitalwayhk/core@<commit>
```

分支会移动并解析为伪版本；生产必须使用已发布 tag 或精确 commit。执行 `release-contract`，并遵循 `docs/RELEASE_POLICY.md` 与废弃登记。

## 常见错误

- URL 加入 `/public`、`/private`。
- private API 使用客户端提交的 UserID。
- `NewModel()` 未初始化嵌入指针。
- 无稳定 Code 的模型使用 BaseModel。
- `GetHash` 与业务唯一性、保存时间精度不一致。
- ManageService 传入内嵌实例而非真实 owner。
- public/private 直接返回持久化模型，或复用 Manage 列表 DTO。
- WebSocket 把外部用户订阅与内部 EventBridge 混为一谈。
- private WebSocket 未实现可信身份注入和用户级通知过滤。
- 绕过 ModelList/模型持久化边界/ServiceContext，或自行实现成熟基础设施能力。
- 集成测试重新实现公共 Suite，只测 handler，或依赖开发机已有配置和数据库。
- 仅因配置字段存在就声明能力稳定。
- 单元测试隐式依赖 Docker/本机数据库。
