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

## 可复用 Button（跨服务通用操作）


Button 是 Manage 页面上非 CRUD 的自定义操作入口，实现 `manage.Operation[T]` 接口。

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
    r := &Retry[T]{}
    r.Operation = *manage.NewOperation[T](own)
    return r
}

func (own *Retry[T]) RouterInfo() *stypes.RouterInfo {
    return manage.RouterInfo(own)   // 路由注册到 /api/manage/{svc}/{controller}/retry
}

func (own *Retry[T]) Do(req stypes.IRequest) (interface{}, error) {
    list := entity.NewModelList[T](nil)
    if err := list.LoadByID(req); err != nil {
        return nil, err
    }
    item := list.GetItem()
    // 通过反射将 RetryCount 重置为 0、将 Status 置为待处理
    utils.SetProperty(item, "RetryCount", 0)
    utils.SetProperty(item, "Status", 0)
    return list.Save()
}
```

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
