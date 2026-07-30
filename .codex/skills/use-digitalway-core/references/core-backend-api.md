# Digitalway Core 后端开发参考

本参考以当前代码和发布契约为准。示例 01–07 依次覆盖最简服务、业务状态机、模型/Manage 继承、性能优化、Casdoor 身份生命周期、Redis 多服务协同与订单水平扩展；框架侧多服务运行图见 ServerManage Runtime API。创建新服务时按复杂度选择最近样例，不另造平行约定。

完整场景矩阵见 `docs/codex/FRAMEWORK_USAGE_GUIDE.md`。消费方安装本 skill 见 `docs/codex/CONSUMER_AI_SKILL_SETUP.md`。

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
├── models/order_write_store.go   # ReliableWriteStore 适配、UseWriteBehind
├── models/order_write_runtime.go # 实例级 OrderWriteRuntime，禁止全局 store
└── service.go                    # UseResource 注册 store，Stop 时 Unbind

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

examples/07-shop-order-scale/
├── contract,dto,bootstrap        # 稳定服务名、错误、配置组装
├── user-service                  # 入口 facade；幂等边界见 README
├── supplier-service              # 商品权威与订单投影
├── order-service                 # 多副本接单、OrderWriteRuntime、OrderRule、Outbox
├── main/{all-in-one,user,order,supplier}
├── deploy/                       # Docker、Prometheus scrape 示例
└── README.md                     # AutoMachineID、共享 MySQL、06/07 对比

examples/integration/07-shop-order-scale/
examples/integration/07-shop-order-scale-multi-process/
```

单元测试与实现同目录；跨子包继承/兼容契约测试留在根包；真实进程、HTTP、WebSocket 和 Casdoor 测试只放 `examples/integration/<service>`；固定样本放 `testdata/`。

示例 06 的每个服务也按示例 05 的模型目录拆分：`models/common` 放服务级基础模型、数据库名和 TraceID，`models/basedata` 放供应商、商品、支付类型、用户、地址等基础资料，`models/transaction` 放订单、支付、投影和 Outbox/Inbox 等业务事实，`models/internal/store` 统一 `IDataAction` 和事务互斥，`models/schema` 统一建表，根 `models` 只保留 `models.go` 兼容门面，不放具体模型或持久化实现。具体模型通过基础资料模型或业务事实模型继承服务级基础模型，自动获得 `GetLocalDBName/GetRemoteDBName` 和 `TraceID`；不要在每个具体模型上重复声明库名或 TraceID 字段。写路径从入口 `req.GetTraceId()` 传到 business，再写入业务事实、Outbox、Inbox 和投影；事件 Metadata 同步携带 TraceID，但 EventID 仍负责事件幂等。

示例 06 的 `api/manage` 目录也必须按示例 05 拆分：`api/manage/common` 放权限、owner 限域和全服务最基础 `ServiceManage[T]`，`api/manage/basedata` 放 `BaseDataManage[T]`、基础资料 Manage 与受控命令，`api/manage/transaction` 放 `TransactionManage[T]`、订单、支付、投影等业务 Manage，`api/manage/audit` 只在存在审计/身份事件时使用；根 `api/manage` 只保留 `manage.go` 兼容门面和路由注册入口。

示例和服务代码必须先让人读得懂再追求复用：每个 Go 文件开头用中文文件级注释说明该文件提供的能力、所属边界和主要读者；每个 public 类型、函数、方法、变量必须有中文注释；private 逻辑在涉及权限、事务、事件、缓存、幂等、跨服务调用或测试编排时也要补充意图说明。单元测试和 `examples/integration` 集成测试同样适用；测试文件的文件级注释必须写清验证的业务闭环、角色、边界和异常权限场景，避免系统复杂后只能靠逐行读代码理解测试目的。

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

### 模型持久化边界与双路径访问

框架支持多种数据库类型（SQLite、MySQL、PostgreSQL 等）。**SQLite 只是零配置的默认/开发选项**：本地开发与单机测试最简单，无需额外配置即可作为本地库，也可临时当作“远程”权威库；**生产与多进程共享权威库应按 MySQL 等网络库选型**，不是“只能 SQLite”。

推荐在服务**最基础 model 层**（如 `models/common`、`models/data_action.go`、`models/internal/store`）集中定义明确的 `IDataAction` 获取方法，例如 `LocalDataAction()` / `RemoteDataAction()` / `ManageDataAction()`。后续切换库类型时**只改这些方法**，Manage 与 public/private 调用点保持不变。这里共享的是无请求状态的数据访问能力；模型实例、当前用户、查询条件和响应不得放入单例。

**Manage API 与 public/private 使用数据库的方式不同，不可混用：**

| 路径 | 访问方式 | 适用 |
| --- | --- | --- |
| Manage | `ModelList` + 标准 Search/View/Add… | 管理后台；框架筛选/排序/分页；管理人员配置与查询 |
| public/private | models 业务方法（内部 `IDataAction`）+ 可选 business | **所有**业务读写默认模式（见 01）；API 不直接 `NewModelList` |
| public/private 高吞吐写 | 04/07 专用 store：本地可靠写 + `UseWriteBehind` → 远程权威库 | 下单/支付等需水平扩展或极高 TPS 时再升级，不是简单业务的必选项 |

两者可以**共用同一 model 结构体**，通过基础 model 上的 DataAction 取连接。库类型（SQLite/MySQL 等）由 DataAction 决定，与「是否 ModelList」正交。

public/private 默认示例（01：语义方法 + `IDataAction`，不是 ModelList）：

```go
product, err := models.NewProduct().FindByID(productID) // 内部 getDataAction().Load(...)
orders, err := models.NewOrder().QueryByUser(userID)
order, err := models.NewOrder().FindOwned(orderID, userID)
err = order.Delete() // 内部 getDataAction().Delete(...)
```

集中 DataAction 示例：

```go
var (
	dataActionOnce sync.Once
	dataAction     persistencetypes.IDataAction
)

// LocalDataAction / RemoteDataAction：切换 SQLite→MySQL 只改此处实现。
func LocalDataAction() persistencetypes.IDataAction {
	dataActionOnce.Do(func() {
		dataAction = entity.GetGlobalSqliteInstance(NewProduct().GetLocalDBName())
		// 生产示例：dataAction = entity.GetGlobalMysqlInstance(...)
	})
	return dataAction
}
```

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

### 指定 Manage 数据源（服务级 `GetList`）

Manage **应当**使用 `ModelList`，以获得默认筛选、排序、分页等标准能力（适合管理人员配置系统，不追求业务级吞吐）。

- **未重写 `GetList()`** 时：框架默认使用进程运行目录下 `db/` 中的**本地 SQLite**（按模型库名）。
- **生产或共享权威库**：在 models 提供 `ManageDataAction()` / `RemoteDataAction()`，并在本服务最基础的 `common.ServiceManage[T]` **统一重写** `GetList()`，使本服务全部 Manage 连同一数据源。
- 不要在具体 Manage 的 `OnSearchBefore` 手写列表并 `stop=true`，否则会绕过 `SearchItem`/`LoadList`，破坏前端筛选、排序、分页、关联与 `SearchAfter`。

```go
// models 包只暴露 IDataAction，不让 api/manage 直接依赖具体驱动。
func ManageDataAction() persistencetypes.IDataAction {
	return RemoteDataAction() // 开发可为 SQLite；生产常为共享 MySQL
}

// common.ServiceManage 为本服务全部 Manage 统一选择数据库。
func (*ServiceManage[T]) GetList() interface{} {
	return entity.NewModelList[T](models.ManageDataAction())
}
```

具体 `OrderManage`、`ProductManage` 等继续使用框架标准 `Search`，只在确有业务语义时实现 Hook。仅当**单个**模型需要特殊库时，才可在该 Manage 重写 `GetList()`，且仍必须返回绑定目标 `IDataAction` 的 `ModelList`。

### 只读管理

订单管理只注册 `view/search`，不注册 `add/edit/remove`。只读不是依赖 handler 内拒绝写入，而是根本不把写 command 暴露为路由。集成测试应断言未注册 command 返回 404。

**再次强调：** `ModelList` 是 Manage 路径的正确默认；public/private 默认用 models 业务方法 + `IDataAction`；仅高吞吐写再上 04/07 专用 store。共用 model 结构，不共用 Manage 列表访问方式。

### Manage 动态分库（IDBName + Where 写回 + 空 Database MySQL）

这是 Core **正统**的 Manage 分库路径：保留 `ModelList` 筛选/排序/分页生命周期，用模型上的 `IDBName` 按请求条件路由到不同库。**不要**用 `OnSearchBefore` + `stop=true` 自研列表（那是历史旁路）。

#### 标准链路（与实现一致）

```text
Manage Search.Do
  → SearchItem.ToSearchItem()
  → item.Model = list.NewItem()          // 空模型，须 NewModel 初始化嵌入指针
  → list.LoadList(item)
       → searchHook:
            IModelSearchHook.SearchWhere(WhereList)  // entity.Model 默认原样返回
            SetPropertyValue(Model, column, value) // Where 写回模型字段（列名大小写不敏感）
       → GetDBAdapter → ada.Load
            MySQL.init/Load → resolveDBName(item.Model):
              1) config.Database 非空 → 固定库（动态路由关闭）
              2) model.GetRemoteDBName() / GetLocalDBName() → 动态库（每次重算，不固化）
              3) m.Name 兜底
```

证据：

- `types.IDBName`：`pkg/persistence/types/interface.go`
- `resolveDBName`：`pkg/persistence/database/oltp/mysql.go`（注释明确多交易对与「不固化」）
- `searchHook`：`pkg/persistence/entity/modellist.go`
- Manage Search：`service/manage/search.go`（`stop=true` 才跳过 `LoadList`）
- 字段驱动多库：`sharedbadger_test.go` 多远程 DB 路由（`SearchItem.Model` + `GetRemoteDBName`）

#### 推荐目标形态（分库服务）

```go
// 服务基础 model（嵌入 *entity.Model 或本服务基座）
func (m *ServiceBaseModel) GetRemoteDBName() string {
	if m.MarketCode == "" {
		return "" // 或固定 UNBOUND 哨兵；禁止返回真实默认业务库
	}
	return "bitzoom_positions_" + m.MarketCode // 按服务域命名
}

// models：可路由 MySQL —— Database 必须为空
func ManageRoutableMySQL() persistencetypes.IDataAction {
	return oltp.NewMySQL(&oltp.Config{
		Host: host, Port: port, User: user, Password: pass,
		Database: "", // 关键：非空则永远固定库
	})
}

// ServiceManage.GetList
func (*ServiceManage[T]) GetList() interface{} {
	return entity.NewModelList[T](models.ManageRoutableMySQL())
}

// OnSearchBefore：只校验 / 补齐 Where，不 stop
func (own *PositionManage) OnSearchBefore(op *manage.Search[Position], req types.IRequest) (interface{}, error, bool) {
	if !whereHas(op.SearchItem, "MarketCode") {
		return nil, errMarketCodeRequired, true // 仅 fail-closed 时可 stop；不要在此查库拼列表
	}
	// 可选：校验 market 在目录中活跃
	return nil, nil, false
}
```

前端 Search 请求的 `WhereList` **必须**带分库键（如 `marketCode` / `MarketCode`）。`entity.Model.SearchWhere` 原样返回 Where；`SetPropertyValue` 按字段名 **大小写不敏感** 写回。

#### 硬条件（易踩坑）

| 条件 | 说明 |
| --- | --- |
| `config.Database` 必须为空 | 非空时 `resolveDBName` 永远用固定库，`GetRemoteDBName` 不参与 |
| Adapter 可动态切库 | 同一 host 的 MySQL 实例 + 空 Database；不要 `NewMySQL` 时写死 `bitzoom_trades_BTCUSDT`，也不要用无模型切库语义的全局 SQLite `store.Get()` 冒充分库路由 |
| Where 字段能写到 hook 字段 | 分库键在基础 model 上；指针嵌入须 `NewModel()`；写失败时 `SetPropertyValue` 可能静默不 set，须靠 `GetRemoteDBName` 空键 fail-closed 兜底 |
| 缺键 fail-closed | `MarketCode` 空 → 返回空名/错误，禁止默认真库或扫错库 |
| 单次 Search 单库 | 动态名是「一次查询一个 DB」；跨市场全扫需产品层多次 Search 或非本路径方案 |
| View / 按 ID | 仅 `Id` 无 market 时库名仍解析不了；View 条件须带 market，或约定别的入口 |

#### 与「旁路」对照

| 做法 | 评价 |
| --- | --- |
| `IDBName` + 空 Database MySQL + 标准 `LoadList` | **推荐**：保留筛选/分页/关联与 SearchAfter |
| `OnSearchBefore` + `stop=true` + per-market 手写 store | **历史适配**：绕过标准管道；长期应迁回上一行 |
| Manage `GetList` 绑固定 `bitzoom_trades` 控制面库 | 仅控制面/兼容；分库业务数据应走可路由 adapter + `GetRemoteDBName` |

#### public/private 分库写路径

业务高吞吐写仍可按市场分库，但是 **models/business/专用 store** 路径（或 04/07 write-behind 目标绑定分库），**不是** Manage `ModelList` 动态路由。两者可共用「按 MarketCode 拼库名」规则，但 API 访问方式仍分离。

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

`PrefixedBadgerDB` / `ReliableWriteStore` 的 write-behind 与 RouterInfo L2 是两种不同能力：L2 可重建；write-behind pending 在远端权威库确认前是业务事实。高 TPS 路径必须等**本地可靠写成功**后才向调用方确认，再异步同步远程；远端 ACK 后才删除 pending。

标准业务热路径（示例 04/07，**不是** Manage/`ModelList`）：

1. public/private → business → 实例级 `OrderWriteRuntime`（或等价注入），不使用包级全局 store registry。
2. `Start` 中创建 store，调用 `UseWriteBehind(WriteBehindTarget)` 绑定**远程权威库**汇合目标；`ServiceContext.UseResource` 管理关闭。
3. 远程权威库类型由 models 的 DataAction/`WriteBehindTarget` 决定：开发可用 SQLite；多进程/Docker 应用共享 MySQL 等网络库。04 可用 `ModelListWriteBehindTarget` 作示例目标适配；07 订单权威库应用真正共享 remote。
4. `EnableWriteBehind(ModelList)` / `SetSyncDB` 仅为兼容层；`StartOrderWriteStore`/`StopOrderWriteStore` 已废弃。

基准必须与对照示例同机、同口径、多轮运行，同时报告 QPS/TPS、p50/p95/p99、错误率、pending 收敛和磁盘上限。

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

```

`IService` 只声明稳定服务名和路由。内部异步事件统一在 `Start()` 中使用 `sc.SubscribeEvent(...)`；外部用户通知使用 WebSocket 运行时。

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
- 内部专用 Public 用 `router.WithInternalCallers(...)` 声明允许服务；冻结后通过 `GetInternalCallers()` 读取。匿名 `/api/openapi` 过滤这些路由且不输出白名单；兼容快照和使用 `ServerManageAuth` 的 `/api/internal/openapi` 才记录 `x-internal-callers`。
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

## 订单水平扩展（示例 07）

以 `examples/07-shop-order-scale` 为标准模板，在 06 的多服务边界之上演示可扩容 order 副本：

- `AutoMachineID=true`：MachineID 由 ClusterProvider lease 分配，不得为可扩容副本硬编码固定 MachineID。
- 每副本本地 pending / Outbox / Inbox / 投影目录隔离；最终订单权威库是**共享**远程库（Docker/多进程下为 MySQL 等），不是每进程 SQLite remote。
- 下单热路径：public/private → business → `OrderWriteRuntime` → 本地可靠写 → `UseWriteBehind` 同步远程权威库；Manage 继续用 `ModelList` 做后台视图/配置（服务级 `GetList` 绑定同一权威库 DataAction 亦可），但不得替代业务写路径。
- `OrderRule` 等可配置规则走 Manage + 可靠事件同步到副本本地缓存；下单校验读本地规则快照，不在热路径同步打远程权威库。
- 多实例诊断字段记录 `TraceID`、`ServiceName`、`ServiceInstanceID`；`ServiceInstanceIP` 仅诊断。
- 幂等边界必须写进 README：只扩展 order 时 user 入口幂等策略的限制；远程幂等探测在 MySQL 不可达时 fail-closed 或明确文档化降级风险。
- 部署侧提供 Prometheus scrape 配置（如 `deploy/prometheus*.yml`），标签至少稳定暴露 `service` 与 `service_instance_id`，供 Runtime Aggregator 查询。
- 集成测试：`examples/integration/07-shop-order-scale` 与 `07-shop-order-scale-multi-process`；多副本 UAT 应采样 discovery 确认 `MachineID`/`ServiceInstanceID` 唯一。

## 多服务运行图（Runtime API）

运维与 Admin 监控使用框架 Runtime 链路，**不要**恢复旧 `RouterStats` 或未注册的 `/api/servermanage/statistics`。

| 入口 | 路径 | 认证 | 用途 |
| --- | --- | --- | --- |
| 全局拓扑 | `POST /api/servermanage/runtimetopology` | ServerManageAuth | 逻辑服务节点、同步/异步边、窗口聚合 |
| 单服务详情 | `POST /api/servermanage/runtimeservice` | ServerManageAuth | 路由请求聚合、实例分布、组件指标 |

契约要点：

- 请求体/查询支持 `window`：仅 `15s`、`5m`、`1h`；`runtimeservice` 另需 `service`。
- **ClusterProvider** 提供服务实例与地址；**Prometheus** 提供 rate/error/histogram 历史；RouterInfo 提供稳定路由元数据；Pending/Outbox/EventBridge 等通过本进程 Collector 暴露后由 Prom 查询。
- Runtime Aggregator 只部署在 ServerManage 可达边界：业务副本暴露 scrape 指标，**禁止** Aggregator 在 API 请求中直连各实例 `/metrics` 或 Provider。
- 指标诚实状态：`ok` / `partial` / `stale` / `unavailable` / `no_traffic` / `not_collected`。缺失时数值为 `null` + `state`，不得把未采集写成 0。
- 同步边：跨服务 gRPC/内部调用；异步边：Outbox 发布与订阅索引汇合及低基数 gauge。全局图画逻辑服务，组件进入服务内部视图。
- 标签低基数：禁止 userId、orderId、TraceID、原始 URL、SQL 等作为指标标签。
- 兼容级别见 `docs/codex/API_COMPATIBILITY_SURFACE.md`（Experimental → 趋向 Stable）与 `docs/codex/DEPRECATION_REGISTER.md`。
- Web Admin 页面为 `MonitorSystem`，前端经 ServerManage 调 Runtime API，浏览器不直连 Prometheus。

实现位置：`pkg/server/runtime`、`pkg/server/api/public/runtimetopology.go`、`pkg/server/observability`；验收可参考示例 07 的 scrape 配置与 multi-process 集成测试。

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

只要是多服务业务，就必须提供真实多进程 UAT。单服务测试、同进程测试或单包 handler 测试不能证明跨服务发现、内部调用、事件投递、缓存失效和角色权限边界真的可用；任一业务角色都可能通过跨服务调用链才能确认完整能力。

多服务 UAT 必须按业务角色或调用方拆文件，每个角色文件保存本角色全部功能闭环和异常权限断言，并提供一个可单独 `go test -run` 的角色闭环测试。任何角色或服务只要实现 WebSocket 接口，该角色 UAT 就必须覆盖真实 WebSocket 登录、订阅、事件投递、身份隔离和异常边界。示例 06 三进程 UAT 是标准模板：`buyer_uat_test.go` 放普通用户注册模拟、资料/地址维护、下单、支付、本人订单查询、WebSocket 订单订阅和其他用户隔离；`supplier_uat_test.go` 放供应商注册模拟、商品维护/上架、本供应商订单投影查询和其他供应商隔离；`admin_uat_test.go` 放平台管理员支付类型配置和全量订单查询。完整三角色流程测试只负责启动三个真实进程并组合这些角色步骤；共享查找、进程启动、业务 DTO 转换等跨角色辅助可以放独立 helper 文件。不要把三种角色的 API 调用、断言和异常用例全部堆在一个 UAT 大文件中。

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

- 只要服务实现 WebSocket 接口，集成测试和 UAT 都必须使用真实 WebSocket 覆盖该能力，不能只测 HTTP 或 handler。
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

## PrefixedBadgerDB / ReliableWriteStore

- 纯缓存默认损坏策略为 `CorruptionPolicyFail`；只有确认数据可从远端完整重建时才显式使用 `CorruptionPolicyResetCache`。
- **新业务默认**：`ReliableWriteStore` / `PrefixedBadgerDB.UseWriteBehind(WriteBehindTarget)` 绑定远端汇合目标；配置必须满足可靠写要求（含 `SyncWrites=true`、冲突检测与 fail 策略，以 `EnableWriteBehind`/`UseWriteBehind` 校验为准）。
- 示例适配：04 使用 `ModelListWriteBehindTarget`；07 使用订单专用 `WriteBehindTarget` 指向共享远程权威库。
- `DefaultSharedConfig` 默认 `SyncWrites=false`，面向共享缓存；write-behind 必须显式启用持久写。
- `SetSyncDB`、`EnableWriteBehind(ModelList)` 兼容路径已废弃或仅兼容，不得作为新热路径设计中心。
- 待同步记录禁止 TTL。`Close` 返回 `PendingSyncError` 表示本地仍是临时事实源，不能把目录当缓存删除。
- 语义为 at-least-once，远端操作必须幂等。同 key 写入会合并状态，不适用于资金流水或审计事件；不可合并事件使用唯一事件 ID 的 JetStream/outbox。

## Cluster、Transport、MQ 与事件

- Local cluster：`Stable`。
- etcd/Consul：`Conditional`，需要显式配置和外部依赖。
- 内部同步传输默认 gRPC，HTTP 只作为显式备用；自定义 Socket 已删除，迁移见 `docs/codex/GRPC_TRANSPORT_MIGRATION.md`。
- gRPC Client 复用 zrpc，Server 因 go-zero v1.10.2 无法独立停止单 listener 而保留薄 grpc-go 生命周期适配；跨主机生产使用 mTLS，已有双向身份的服务网格使用 mesh。Client 侧可复用 zrpc 指标中间件；服务端与跨服务 call-edge、Pending/Outbox 等低基数指标经 Core Collector 进入 Prometheus，供 Runtime API 聚合。
- QUIC 和 MQ transport：`Unsupported`，配置校验拒绝。
- MQ/EventBridge：Redis Streams、NATS JetStream 为 `Conditional`。
- 有序可靠投递为加性契约：`mq.PublishOptions.OrderingKey`、`OrderedReliableMQProvider`、`MQManager.RequireOrderedReliable`、EventBridge 透传与 Outbox earliest-first / 可选 `OutboxStoreSkipBlocked` 等以 `docs/codex/API_COMPATIBILITY_SURFACE.md` 与当前测试为准；未声明 requirement 时零值兼容。
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
- 为切换 Manage 数据库而在 `OnSearchBefore` 手写查询并返回 `stop=true`，导致标准筛选、排序、分页和关联查询失效。
- public/private 直接返回持久化模型，或复用 Manage 列表 DTO。
- WebSocket 把外部用户订阅与内部 EventBridge 混为一谈。
- private WebSocket 未实现可信身份注入和用户级通知过滤。
- 绕过 models 持久化边界/`ServiceContext`，或在 API 层直接绑定具体数据库驱动。
- public/private 直接 `NewModelList` 或套用 Manage Search/CRUD 做业务读写（正确：models 业务方法 + `IDataAction`）。
- Manage 不用 `ModelList` 却手写 Search `stop=true` 破坏筛选分页；或该重写服务级 `GetList` 时未重写；分库场景用 per-market 自研列表代替 `IDBName` 标准管道。
- 动态分库时 MySQL `Database` 非空、缺 `marketCode` 仍默认真库、View 只带 ID 却期望命中分库。
- 在每个具体 model/API 里散落库连接，未在基础 model/store 集中 DataAction；或把「库类型」与「ModelList vs IDataAction」混为一谈。
- 集成测试重新实现公共 Suite，只测 handler，或依赖开发机已有配置和数据库。
- 仅因配置字段存在就声明能力稳定。
- 单元测试隐式依赖 Docker/本机数据库。
- 已需要高吞吐写时仍用全局 `StartOrderWriteStore`/`SetSyncDB` 或 Manage 式列表轮询，未采用「本地可靠写 → `UseWriteBehind` → 远程权威库」。
- 水平扩展把最终业务库按副本分片，或用每进程私有库冒充共享 remote（开发用 SQLite、生产换共享 MySQL 是 DataAction 切换，不是分片）。
- 恢复 `RouterStats`/`Statistics`；Runtime 把未采集指标写成 0；浏览器直连 Prometheus 或其他实例 `/metrics`。
- 依赖 core 的业务仓库未安装 `.codex/skills/use-digitalway-core`，凭记忆编码（见 `docs/codex/CONSUMER_AI_SKILL_SETUP.md`）。
