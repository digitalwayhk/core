# 目录结构、业务分层与启动组合根

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

示例 06 的每个服务也按示例 05 的模型目录拆分：`models/common` 放服务公共模型基座、数据库名和 TraceID，`models/basedata` 放供应商、商品、支付类型、用户、地址等基础资料，`models/transaction` 放订单、支付、投影等业务事实，`models/internal/store` 放 DataAction、Outbox/Inbox 等基础设施持久化模型和事务互斥，`models/schema` 统一建表，根 `models` 只保留 `models.go` 兼容门面，不放具体模型或持久化实现。基础资料模型与业务事实模型是继承服务公共基座的两条平行支路；这个公共基座不是“基础资料 Model”。写路径从入口 `req.GetTraceId()` 传到 business，再写入业务事实、Outbox、Inbox 和投影；事件 Metadata 同步携带 TraceID，但 EventID 仍负责事件幂等。

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


## 项目共享层 internal/pkg


#### `internal/pkg/models/project_model.go`

所有服务模型的中性公共基座。只集中定义：
- 全项目公共字段（如 `TraceID`）
- 数据库连接工厂（从环境变量读 DSN，统一切换 MySQL）

不要在这里放 Code/Name/Enabled 等基础资料专属字段，也不要放业务状态机字段。

```go
// internal/pkg/models/project_model.go
package models

import "github.com/digitalwayhk/core/pkg/persistence/entity"
import persisttypes "github.com/digitalwayhk/core/pkg/persistence/types"

// ProjectModel 是全项目中性持久化基座，不代表基础资料 Model。
type ProjectModel struct {
    *entity.Model
    TraceID string `json:"traceId"` // 请求追踪ID
}

func NewProjectModel() *ProjectModel {
    return &ProjectModel{Model: entity.NewModel()}
}

func (own *ProjectModel) NewModel() {
    if own.Model == nil {
        own.Model = entity.NewModel()
    }
}

// ProjectModelList 封装全项目公共连接获取逻辑。
type ProjectModelList[T persisttypes.IModel] struct {
    *entity.ModelList[T]
}

func NewProjectModelList[T persisttypes.IModel](action persisttypes.IDataAction) *ProjectModelList[T] {
    return &ProjectModelList[T]{
        ModelList: entity.NewModelList[T](action),
    }
}
```

#### `internal/pkg/api/base_api.go`

所有服务 handler 的公共基类。集中定义：
- 默认 `Parse/Validation/Do` 空实现（子类按需覆写）
- 公共工具方法（如 `GetUintID` 从 query 解析 uint）

```go
// internal/pkg/api/base_api.go
package api

import (
    "fmt"
    "strconv"
    "github.com/digitalwayhk/core/pkg/server/router"
    "github.com/digitalwayhk/core/pkg/server/types"
)

// BaseAPI 全项目 handler 公共基类。
// 提供默认空实现和公共工具方法；业务 handler 嵌入此类型后只需覆写需要的方法。
type BaseAPI struct{}

func (own *BaseAPI) Parse(req types.IRequest) error      { return nil }
func (own *BaseAPI) Validation(req types.IRequest) error  { return nil }
func (own *BaseAPI) Do(req types.IRequest) (interface{}, error) { return nil, nil }
func (own *BaseAPI) RouterInfo() *types.RouterInfo {
    return router.DefaultRouterInfo(own)
}

// GetUintID 从 query 参数读取 uint，支持 camelCase / snake_case 两种写法
func (own *BaseAPI) GetUintID(req types.IRequest, key string) (uint, error) {
    idStr := req.GetValue(key)
    if idStr == "" {
        return 0, nil
    }
    id, err := strconv.Atoi(idStr)
    if err != nil {
        return 0, fmt.Errorf("invalid %s: %s", key, idStr)
    }
    return uint(id), nil
}
```

#### `internal/pkg/services/base_manage_service.go`

所有服务 Manage 控制器的公共基类。集中定义：
- 公共字段显示规则（隐藏 UpdatedUserID 等系统字段）
- `IDoBefore[T]` 接口 + 自动分发逻辑
- 默认只暴露 View + Search（写操作由子类追加）

```go
// internal/pkg/services/base_manage_service.go
package services

import (
    persisttypes "github.com/digitalwayhk/core/pkg/persistence/types"
    stypes "github.com/digitalwayhk/core/pkg/server/types"
    "github.com/digitalwayhk/core/service/manage"
    "github.com/digitalwayhk/core/service/manage/view"
    "strings"
)

// IDoBefore 服务级钩子接口，供各 Manage 控制器实现。
// 分发由 BaseManageService.DoBefore 自动完成，控制器只需实现对应方法。
type IDoBefore[T persisttypes.IModel] interface {
    AddBefore(add *manage.Add[T], req stypes.IRequest) (interface{}, error, bool)
    EditBefore(edit *manage.Edit[T], req stypes.IRequest) (interface{}, error, bool)
    RemoveBefore(remove *manage.Remove[T], req stypes.IRequest) (interface{}, error, bool)
}

// BaseManageService 全项目 Manage 公共基类，封装框架 manage.ManageService[T]。
type BaseManageService[T persisttypes.IModel] struct {
    *manage.ManageService[T]
    instance interface{}
    doBefore IDoBefore[T]
}

func NewBaseManageService[T persisttypes.IModel](instance interface{}) *BaseManageService[T] {
    own := &BaseManageService[T]{instance: instance}
    own.ManageService = manage.NewManageService[T](instance)
    if do, ok := instance.(IDoBefore[T]); ok {
        own.doBefore = do
    }
    return own
}

// Routers 默认只暴露只读路由；写路由由子类 Routers() 追加
func (own *BaseManageService[T]) Routers() []stypes.IRouter {
    return []stypes.IRouter{own.View, own.Search}
}

// ViewModel 全项目默认设置
func (own *BaseManageService[T]) ViewModel(v *view.ViewModel) {
    v.AutoLoad = true
}

// ViewFieldModel 全项目公共字段规则（仅需在此改一次，所有 Manage 生效）
func (own *BaseManageService[T]) ViewFieldModel(model interface{}, field *view.FieldModel) {
    // 隐藏操作人系统字段
    if field.IsFieldOrTitle("UpdatedUserName", "UpdatedUserID", "CreatedUserName", "CreatedUserID") {
        field.Visible = false
    }
    // 时间字段统一展示
    if field.IsFieldOrTitle("CreatedAt") {
        field.Title = "创建时间"
        field.Visible = true
        field.Index = 800
        field.IsSearch = true
    }
    // ID 后缀的外键字段默认隐藏
    if strings.HasSuffix(field.Field, "id") {
        field.Visible = false
        field.IsEdit = false
    }
}

// DoBefore 自动分发到 IDoBefore 具体方法
func (own *BaseManageService[T]) DoBefore(sender interface{}, req stypes.IRequest) (interface{}, error, bool) {
    if own.doBefore == nil {
        return nil, nil, false
    }
    switch s := sender.(type) {
    case *manage.Add[T]:    return own.doBefore.AddBefore(s, req)
    case *manage.Edit[T]:   return own.doBefore.EditBefore(s, req)
    case *manage.Remove[T]: return own.doBefore.RemoveBefore(s, req)
    }
    return nil, nil, false
}
```

---

