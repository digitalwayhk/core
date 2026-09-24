# Manage API

## 完整 CRUD

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

自动生成的操作为 View、Search、Add、Edit、Remove、Submit、Release。

标准 CRUD 的框架错误使用稳定 PublicError：Add 的 ID/hash 写前重复和数据库唯一约束冲突
返回 Conflict / HTTP 409 / `record already exists`；Edit 缺少 ID 返回 Validation /
HTTP 400 / `record id is required`；Edit、Remove 目标不存在返回 NotFound / HTTP 404 /
`record not found`。这些通用文案不得携带 ID、hash、SQL 或具体业务模型名称。消费方若需要
更具体的业务提示，应在自己的显式校验中返回 PublicError，不能依赖数据库原文。

`POST .../search` 是同一条路由、三种模式：主模型列表、外键关联表、主表某行的子表。分流看请求体是否带 `field`+`foreign` 或 `parent`+`childmodel`，不要另写 URL。

`POST .../view` 只返回 schema（字段、命令、子模型），没有业务体。命令 `POST .../{command}` 的 body 是**模型字段本身**（不要包 `{model:...}`），`{command}` 必须等于 schema 里 `commands[].command`。View / Search / 命令的调用约定见 [openapi-and-frontend.md](openapi-and-frontend.md)。

## Manage 继承与 Hook 生命周期

### 推荐继承结构

跨多个 Manage 复用数据源、服务公共能力或领域规则时，使用以下结构：

```text
manage.ManageService[T]
└── manage.HookedManageService[T]      # 将粗粒度 Hook 分派为 On... Hook
    └── common.ServiceManage[T]        # 服务级数据源与公共能力
        └── BaseDataManage[T]          # 可选：领域公共字段、命令和规则
            └── ProductManage          # 最终具体 Manage
```

只有一个简单 Manage 时可以直接嵌入 `ManageService[T]`；需要多层复用时，服务公共层应嵌入 `HookedManageService[T]`。构造链的每一层都必须接收并向下传递**最终具体 Manage** 作为 owner：

```go
type ServiceManage[T persistencetypes.IModel] struct {
	*manage.HookedManageService[T]
}

func NewServiceManage[T persistencetypes.IModel](owner interface{}) *ServiceManage[T] {
	return &ServiceManage[T]{
		HookedManageService: manage.NewHookedManageService[T](owner),
	}
}

func (*ServiceManage[T]) GetList() interface{} {
	return models.NewManageModelList[T]()
}

type BaseDataManage[T persistencetypes.IModel] struct {
	*ServiceManage[T]
}

func NewBaseDataManage[T persistencetypes.IModel](owner interface{}) *BaseDataManage[T] {
	return &BaseDataManage[T]{ServiceManage: NewServiceManage[T](owner)}
}

type ProductManage struct {
	*BaseDataManage[models.Product]
}

func NewProductManage() *ProductManage {
	own := &ProductManage{}
	own.BaseDataManage = NewBaseDataManage[models.Product](own)
	return own
}
```

owner 必须是 `own`，不能传中间基座。否则框架只能看到中间层，具体 Manage 的 `OnAddBefore`、`OnSearchAfter`、View 配置等方法不会被调用。

Go 的嵌入只提供方法提升，不会自动执行父层同名方法。覆盖需要保留父层行为的 Hook 时，必须显式调用父层：

```go
func (own *ProductManage) SearchAfter(
	sender interface{},
	result *view.TableData,
	req servertypes.IRequest,
) (interface{}, error) {
	data, err := own.BaseDataManage.SearchAfter(sender, result, req)
	if err != nil {
		return nil, err
	}
	// 追加 ProductManage 自己的查询结果处理。
	return data, nil
}
```

同样的显式父调用规则适用于 `ParseBefore/After`、`ValidationBefore/After`、`DoBefore/After`、`SearchBefore/After`、`ViewModel`、`ViewFieldModel`、`ViewCommandModel` 和 `ViewChildModel`。不要让具体 Manage 覆盖 `ServiceManage.SearchBefore` 后无意跳过服务级公共处理。

### 标准操作的 Hook 顺序

以下是对外应依赖的概念顺序；Hook 中不要保存请求状态，也不要依赖某个 Hook 只进入一次：

| 操作 | 顺序与可用 Hook |
| --- | --- |
| Add / Edit / Remove | `ParseBefore → Bind → ParseAfter → ValidationBefore → 标准校验 → ValidationAfter → DoBefore → 持久化 → DoAfter` |
| View | `ParseBefore → DoBefore/OnViewBefore → 生成 schema → DoAfter/OnViewAfter` |
| 主列表 Search | `SearchBefore/OnSearchBefore → 标准 LoadList → ManageService.SearchAfter → OnSearchAfter` |
| 外键 Search | `ForeignSearchBefore → 查询 → ForeignSearchAfter` |
| 子表 Search | `ChildSearchBefore → 查询 → ChildSearchAfter` |
| Submit | 优先调用 `ISubmitHook.OnSubmit`；未实现时回退 `DoBefore`；成功后调用 `DoAfter` |
| Release | 优先调用 `IReleaseHook.OnRelease`；未实现时回退 `DoBefore`；当前 Release 不调用 `DoAfter` |

`HookedManageService[T]` 把 `DoBefore/DoAfter` 分派为：

- 所有标准 View/Add/Edit/Remove 先进入 `OnDoBefore`，再进入对应的 `OnViewBefore`、`OnAddBefore`、`OnEditBefore`、`OnRemoveBefore`。
- 后置阶段先进入 `OnDoAfter`；它返回非 nil data 或 error 时停止，返回 nil 才进入对应的类型 Hook。
- 主列表 Search 单独进入 `OnSearchBefore/OnSearchAfter`，不会先进入 `OnDoBefore/OnDoAfter`。
- 外键和子表 Search 使用 `ForeignSearchBefore/After`、`ChildSearchBefore/After` 粗粒度 Hook，不由 `HookedManageService` 转换为 `On...` 方法。
- `stop=true` 表示 Hook 已经处理完成，框架不得再执行默认动作；要同时返回最终 data 或明确 error。不要用 `stop=true` 手写普通列表查询。

类型安全 Hook 的签名以 `service/manage/hooks.go` 为准。可运行参考：多层继承看 `examples/03-shop-inheritance`，服务级公共 Hook 看 `examples/06-shop-microservices/*-service/api/manage/common/service_manage.go`。

### 自定义命令的实例与绑定

自定义命令应以值嵌入 `manage.Operation[T]`，并实现 `New(instance)` 为每次请求创建独立对象。`Operation.Parse` 会调用 owner 的 `ParseBefore/ParseAfter`，并把请求体绑定到 `operation.Model`；不要另建一套平行 Request 再手工复制字段。

Core Manage RBAC 启用后，自定义 command 与标准 CRUD 一样在 Router 执行前按稳定 `path + command` 集中鉴权，不要求命令手工调用 owner `DoBefore` 才获得角色权限。自定义命令仍应按业务需要复用 owner Hook 或 business 层完成 owner/租户限域、审计、缓存失效等领域横切语义；这些语义不能反过来替代 Core RBAC。完整角色契约见 [auth-casdoor-and-admin.md](auth-casdoor-and-admin.md)，可运行示例见 `examples/09-admin-manage-rbac`。

### 继承与 Hook 常见错误

| 错误 | 后果 | 正确做法 |
| --- | --- | --- |
| 构造基座时把中间层作为 owner | 最终 Manage 的 Hook 不触发 | 每一层都传最终 `own` |
| 覆盖同名方法但不调用父层 | 默认数据或其他父层公共逻辑被截断 | 在明确的前后顺序中显式调用父层 |
| 具体 Manage 覆盖 `SearchBefore` 并自行返回列表 | 绕过标准筛选、排序、分页和后置 Hook | 只校验或补充 `SearchItem`，让标准 `LoadList` 继续执行 |
| 在 Manage/命令内创建带 DataAction 的 ModelList | API 层决定数据库，破坏持久化边界 | 通过 owner `GetList()`，最终落到 models 的无参数工厂 |
| 把请求、用户或 trace 保存在 Manage 单例 | 并发请求串数据 | 只使用 Hook 的 `req` 参数和请求级 Operation 实例 |

## 指定 Manage 数据源（服务级 `GetList`）

Manage **应当**使用 `ModelList`，以获得默认筛选、排序、分页等标准能力（适合管理人员配置系统，不追求业务级吞吐）。

- **未重写 `GetList()`** 时：`ManageService[T].GetList()` 返回 `entity.NewModelList[T](nil)`，最终落到进程运行目录下 `db/` 中的**本地 SQLite**（按模型库名，路径形如 `<工作目录>/db/<库名>/<库名>.ldb`）。
- **任何服务数据源**：在 models 提供唯一的无参数 `NewManageModelList[T]()`，并在本服务最基础的 `common.ServiceManage[T]` **统一重写** `GetList()`。`api/manage` 不得感知 `IDataAction`，也不得决定使用本地库、远程权威库或动态分库适配器。
- 不要在具体 Manage 的 `OnSearchBefore` 手写列表并 `stop=true`，否则会绕过 `SearchItem`/`LoadList`，破坏前端筛选、排序、分页、关联与 `SearchAfter`。

```go
// models 内部选择连接；不向 api/manage 暴露。
func manageDataAction() persistencetypes.IDataAction {
	return store.GetRemote() // 也可以集中切换为本地库或动态分库适配器
}

// NewManageModelList 为当前服务的 Manage API 创建模型列表。
//
// 本方法是 Manage 访问 ModelList 的唯一 models 层入口。调用方只声明要管理的
// 模型类型，不得感知或传入 IDataAction，也不得决定数据库位置。连接类型、
// 数据库位置和路由策略全部由当前服务的 models 持久化组合根集中选择。
func NewManageModelList[T persistencetypes.IModel]() *entity.ModelList[T] {
	return entity.NewModelList[T](manageDataAction())
}

// common.ServiceManage 只依赖 models 语义入口。
func (*ServiceManage[T]) GetList() interface{} {
	return models.NewManageModelList[T]()
}
```

具体 `OrderManage`、`ProductManage` 等继续使用框架标准 `Search`，只在确有业务语义时实现 Hook。普通水平分库仍使用同一个 `NewManageModelList[T]()`，由模型 `IDBName`/`SearchWhere` 决定目标库。仅当**单个**模型确实连接不同权威库且无法由模型路由表达时，才在 models 增加语义明确的专用 ModelList 工厂，并由该模型的 Manage 重写 `GetList()`；专用工厂仍不得接受 DataAction 参数。

## 只读管理

订单管理只注册 `view/search`，不注册 `add/edit/remove`。只读不是依赖 handler 内拒绝写入，而是根本不把写 command 暴露为路由。集成测试应断言未注册 command 返回 404。

**再次强调：** `ModelList` 是 Manage 路径的正确默认；public/private 默认用 models 业务方法 + `IDataAction`；仅高吞吐写再上 04/07 专用 store。共用 model 结构，不共用 Manage 列表访问方式。

## Manage 动态分库（IDBName + Where 写回 + 空 Database MySQL）

这是 Core **正统**的 Manage 分库路径：保留 `ModelList` 筛选/排序/分页生命周期，用模型上的 `IDBName` 按请求条件路由到不同库。**不要**用 `OnSearchBefore` + `stop=true` 自研列表（那是历史旁路）。

### 标准链路（与实现一致）

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
              2) model.GetRemoteDBName()，为空则回退 model.GetLocalDBName() → 动态库（每次重算，不固化）
              3) 两者都为空时用缓存的 m.Name 兜底；全空才返回错误
```

证据：

- `types.IDBName`：`pkg/persistence/types/interface.go`
- `resolveDBName`：`pkg/persistence/database/oltp/mysql.go`（注释明确多交易对与「不固化」）
- `searchHook`：`pkg/persistence/entity/modellist.go`
- Manage Search：`service/manage/search.go`（`stop=true` 才跳过 `LoadList`）
- 字段驱动多库：`sharedbadger_test.go` 多远程 DB 路由（`SearchItem.Model` + `GetRemoteDBName`）

### fail-closed 必须同时约束两个方法

`types.IDBName` 的两个方法**都只返回 `string`，没有 error 返回值**，所以模型无法"返回错误"来拒绝路由；唯一手段是控制返回的库名。

更关键的是 `resolveDBName` 的第 2 步会在 `GetRemoteDBName()` 返回空时**回退 `GetLocalDBName()`**：

```go
if idb, ok := data.(types.IDBName); ok {
	dbName := idb.GetRemoteDBName()
	if dbName == "" {
		dbName = idb.GetLocalDBName()
	}
	if dbName != "" {
		return dbName, nil
	}
}
```

而 `entity.Model` 的默认实现让 `GetLocalDBName()` 返回 `"models"`。因此**只让 `GetRemoteDBName()` 返回空串并不会 fail-closed**，反而会静默把查询路由到 `models` 库——正是要避免的"落到默认真库"。

正确写法是返回一个不存在业务数据的哨兵库名（推荐），或同时让两个方法都返回空：

```go
// 推荐：哨兵库名。缺键时路由到确定不存在业务数据的库，错误可观测。
const unboundDBName = "__unbound_market__"

func (m *ServiceBaseModel) GetRemoteDBName() string {
	if m.MarketCode == "" {
		return unboundDBName // 不要 return ""，否则回退到 GetLocalDBName
	}
	return "bitzoom_positions_" + m.MarketCode // 按服务域命名
}

// 若确实想让 resolveDBName 报错，必须两个方法一起置空。
func (m *ServiceBaseModel) GetLocalDBName() string {
	if m.MarketCode == "" {
		return ""
	}
	return "bitzoom_positions_" + m.MarketCode
}
```

更可靠的做法是**不把 fail-closed 只押在库名上**：在 `OnSearchBefore` 校验 Where 是否带分库键，缺失时直接返回业务错误并 `stop=true`，库名哨兵只作为兜底防线。

### 推荐目标形态（分库服务）

```go
// models：连接选择保持私有；Database 必须为空以启用模型动态路由。
func manageDataAction() persistencetypes.IDataAction {
	return oltp.NewMySQL(&oltp.Config{
		Host: host, Port: port, User: user, Password: pass,
		Database: "", // 关键：非空则永远固定库
	})
}

func NewManageModelList[T persistencetypes.IModel]() *entity.ModelList[T] {
	return entity.NewModelList[T](manageDataAction())
}

// ServiceManage.GetList
func (*ServiceManage[T]) GetList() interface{} {
	return models.NewManageModelList[T]()
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

### 硬条件（易踩坑）

| 条件 | 说明 |
| --- | --- |
| `config.Database` 必须为空 | 非空时 `resolveDBName` 永远用固定库，`GetRemoteDBName` 不参与 |
| Adapter 可动态切库 | 同一 host 的 MySQL 实例 + 空 Database；不要 `NewMySQL` 时写死 `bitzoom_trades_BTCUSDT`，也不要用无模型切库语义的全局 SQLite `store.Get()` 冒充分库路由 |
| Where 字段能写到 hook 字段 | 分库键在基础 model 上；指针嵌入须 `NewModel()`；写失败时 `SetPropertyValue` 可能静默不 set，须靠库名哨兵兜底 |
| 缺键 fail-closed | 不能只让 `GetRemoteDBName` 返回空（会回退 `GetLocalDBName`，默认是 `models`）。用哨兵库名，或两个方法一起置空，并在 `OnSearchBefore` 前置拦截 |
| 单次 Search 单库 | 动态名是「一次查询一个 DB」；跨市场全扫需产品层多次 Search 或非本路径方案 |
| View / 按 ID | 仅 `Id` 无 market 时库名仍解析不了；View 条件须带 market，或约定别的入口 |

### 与「旁路」对照

| 做法 | 评价 |
| --- | --- |
| `IDBName` + 空 Database MySQL + 标准 `LoadList` | **推荐**：保留筛选/分页/关联与 SearchAfter |
| `OnSearchBefore` + `stop=true` + per-market 手写 store | **历史适配**：绕过标准管道；长期应迁回上一行 |
| Manage `GetList` 绑固定 `bitzoom_trades` 控制面库 | 仅控制面/兼容；分库业务数据应走可路由 adapter + `GetRemoteDBName` |

### public/private 分库写路径

业务高吞吐写仍可按市场分库，但是 **models/business/专用 store** 路径（或 04/07 write-behind 目标绑定分库），**不是** Manage `ModelList` 动态路由。两者可共用「按 MarketCode 拼库名」规则，但 API 访问方式仍分离。

## 多服务独立进程的菜单同步

`UpdateMenu` 由 `server` SystemManage 接收，但 `server` 可以使用本地 Provider，不能作为
集群发现权威源。框架会选取同进程的业务 `ServiceContext` 并调用
`List(ctx, "", running)` 收集服务全集：同进程服务直接读取自己的 RouterInfo，独立进程
则按发现到的每个运行副本调用固定的 `/api/servermanage/queryrouters` 菜单快照，取得
Manage 路由、服务与控制器中英标题以及 `ReportDef` 菜单。管理入口进程的
`router.GetContexts()` 只用于识别本地业务服务，不能代表集群服务全集。

任一已发现的远程副本无法调用、返回了跨服务路由，或同服务运行副本快照不一致时，
整次 `UpdateMenu` 必须失败，
不得用部分发现结果继续同步或对外报告成功。业务
服务必须注册到同一个 ClusterProvider，并发布可由内部传输访问的地址；前端不需要再逐服务
调用 `queryrouters/{service}` 做二次聚合。`QueryRouters` 未请求菜单快照时仍返回原有
`[]*RouterInfo`，现有运维调用保持兼容。

## 可复用 Button（跨服务通用操作）


Button 是 Manage 页面上非 CRUD 的自定义操作入口，以值嵌入 `manage.Operation[T]`。

#### 放置规则

| 条件 | 位置 |
|------|------|
| 基于通用字段（如 `RetryCount`），任何含该字段的 model 均可复用 | `internal/pkg/api/manage/button/` |
| 仅该服务内多个 Manage 复用 | `internal/core/{svc}/api/manage/button/` |
| 仅某一个 Manage 使用 | 内联到该 Manage 文件或同级独立文件 |

#### 通用 Retry 按钮示例

`Retry[T]` 是最典型的跨服务通用按钮：通过反射判断 model 是否含 `RetryCount` 字段，
无需 model 实现任何接口，任何包含该字段的 model 都可直接使用。

```go
// internal/pkg/api/manage/button/retry.go
package button

import (
    "github.com/digitalwayhk/core/pkg/persistence/entity"
    persisttypes "github.com/digitalwayhk/core/pkg/persistence/types"
    stypes "github.com/digitalwayhk/core/pkg/server/types"
    "github.com/digitalwayhk/core/pkg/utils"
    "github.com/digitalwayhk/core/service/manage"
)

// Retry 通用重试按钮。
// 适用于所有含 RetryCount 字段的 model；直接在 Manage 的 Routers() 中注册即可。
type Retry[T persisttypes.IModel] struct {
    manage.Operation[T]
}

func NewRetry[T persisttypes.IModel](own interface{}) *Retry[T] {
    return &Retry[T]{Operation: manage.NewOperation[T](own)}
}

// New 为每次请求创建独立命令实例，并保留最终 Manage owner。
func (own *Retry[T]) New(instance interface{}) stypes.IRouter {
    return NewRetry[T](instance)
}

func (own *Retry[T]) RouterInfo() *stypes.RouterInfo {
    return manage.RouterInfo(own)   // 路由注册到 /api/manage/{svc}/{controller}/retry
}

func (own *Retry[T]) Do(req stypes.IRequest) (interface{}, error) {
    // 复用 owner.GetList()，数据源仍由 models.NewManageModelList 决定。
    provider := own.GetInstance().(manage.IGetModelList)
    list := provider.GetList().(*entity.ModelList[T])
    item := own.Model
    // 通过反射将 RetryCount 重置为 0、将 Status 置为待处理
    utils.SetPropertyValue(item, "RetryCount", 0)
    utils.SetPropertyValue(item, "Status", 0)
    if err := list.Update(item); err != nil {
        return nil, err
    }
    if err := list.Save(); err != nil {
        return nil, err
    }
    return item, nil
}
```

上例只说明请求级 `Operation`、模型绑定和通过 owner `GetList()` 保持数据源边界。自定义写命令的 Core 角色权限由 Router 前 RBAC 统一处理；命令自身仍负责本节描述的业务持久化与领域 Hook 边界。

**在 Manage 控制器的 Routers() 中注册：**

```go
// internal/core/trades/api/manage/order_manage.go
import pkgbutton "yourproject/internal/pkg/api/manage/button"

func (own *TradeOrderManage) Routers() []stypes.IRouter {
    routers := own.BaseManageService.Routers()          // 默认 View + Search
    routers = append(routers, own.Add, own.Edit)        // 追加写操作
    routers = append(routers,
        pkgbutton.NewRetry[TradeOrder](own),             // 复用通用 Retry 按钮
    )
    return routers
}
```

#### 服务级 Button 示例（仅该服务内复用）

```go
// internal/core/trades/api/manage/button/cancel_order.go
package button

// CancelOrder 仅 trades 服务内部使用的取消订单按钮。
// 需要访问 trades 内部服务，不适合放到 internal/pkg 层。
type CancelOrder[T persisttypes.IModel] struct {
    manage.Operation[T]
}
```

## 管理后台中英标题（`ILocaleTitle`）

管理端把当前语言放在请求头 `X-Locale`（合法值 `zh-CN`、`en-US`；缺省、空值和无法识别一律回退 `zh-CN`）。后端每个请求当场解析，**不要把语言存进 Manage 单例、全局变量或 `ServiceContext`**。

```go
import "github.com/digitalwayhk/core/pkg/server/locale"

current := locale.FromRequest(req) // 已规范化的 zh-CN 或 en-US
```

`locale.Normalize` 接受 `zh`、`zh_CN`、`zh-Hans`、`en`、`en_US` 等写法；`ja-JP`、`pt-BR`、`zh-TW` 第一期回退中文且不报错。请求未实现 `types.IRequestHttp` 时同样返回 `zh-CN`，不 panic。

### 声明中英标题

`ILocaleTitle` 是 `ITitle` 之外的**加性**接口，不实现就保持原有行为。服务实例实现它决定目录标题，Manage 控制器实现它决定菜单和页面标题。

```go
type ILocaleTitle interface {
    GetLocaleTitle(locale string) string
}

func (own *OrderManage) GetLocaleTitle(locale string) string {
    if locale == "en-US" {
        return "Orders"
    }
    return "订单管理"
}
```

回退顺序：`GetLocaleTitle(locale)` 非空 → `ITitle.GetTitle()` → `Name` 或 Go 类型名。某语言没有文案就返回空串由框架回退，不要返回空格或占位符。

返回值**只用于展示**。`Name`、`Url`、权限和路由都是稳定键，不随语言变。

### 落库与同步

`DirectoryModel` 和 `MenuModel` 各有 `Title`（默认中文，兼容旧前端与「菜单管理」编辑器）和 `TitleEN`（英文，空则回退 `Title`）两列。`TitleEN` 由框架首次访问时自动补列，**不要写迁移脚本**。

菜单同步以**代码为翻译权威源**：权限集合未变但代码里的中英标题变了，同步仍会更新 `Title` 和 `TitleEN`。`Sort`、`Icon`、`Description` 是用户字段，生成结果不覆盖。改了 `GetLocaleTitle` 的返回值后，下一次菜单同步即可看到新标题，不需要删表重建。

### 框架默认标题

`View.Do` 在调用 `ViewModel(vm)` 之后，若 Manage 控制器实现 `ILocaleTitle` 且当前语言文案非空，就用它覆盖 `vm.Title`；消费方仍可在 `OnViewAfter` 再覆盖。

标准命令与框架公共字段有内置中英文案，消费方可用 `ViewCommandModel` / `ViewFieldModel` 覆盖：

| 键 | zh-CN | en-US |
| --- | --- | --- |
| `add` / `edit` / `remove` | 新增 / 编辑 / 删除 | Add / Edit / Remove |
| `submit` / `release` | 提交 / 发布 | Submit / Release |
| `ID` / `TraceID` | 编号 / 追踪号 | ID / Trace ID |
| `CreatedAt` / `UpdatedAt` | 创建时间 / 更新时间 | Created At / Updated At |
| `CreatedUserName` / `UpdatedUserName` | 创建人 / 更新人 | Created By / Updated By |

自定义命令不在表内，`Title` 保持类型名。需要在 Manage 之外按指定语言生成命令时用 `manage.RouterToLocaleCommand(info, current)`；`manage.RouterToCommand(info)` 等价于默认语言。

---
