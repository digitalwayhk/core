# pkg/persistence 目录说明

`pkg/persistence` 是 core 的数据实体操作与持久化抽象层。它的目标是让业务代码只决定“什么时候新增、修改、删除、查询和保存”，而不直接关心底层使用 SQLite、MySQL、PostgreSQL、ClickHouse、BadgerDB 等哪种存储实现。

## 核心概念

### Model

`entity.Model` 是标准模型基类，提供：

- `ID`
- `CreatedAt`
- `UpdatedAt`
- `ModelState`
- `Hashcode`

业务模型通常这样定义：

```go
type OrderModel struct {
    *entity.Model
    UserID string
}

func (m *OrderModel) NewModel() {
    if m.Model == nil {
        m.Model = entity.NewModel()
    }
}
```

模型需要实现 `NewModel()`，否则 `ModelList[T].NewItem()` 无法稳定初始化嵌入的基础模型。

### ModelList

`entity.ModelList[T]` 是业务代码最常用的数据操作入口，负责维护新增、修改、删除、查询结果列表，并在 `Save()` 时统一落库。

常用方法：

- `NewItem()`：创建并初始化模型。
- `Add()`：加入新增列表，并执行重复 ID/hash 与模型校验。
- `Update()`：加入更新列表，并加载旧数据做校验。
- `Remove()`：加入删除列表。
- `Save()`：按新增、更新、删除列表执行持久化。
- `SearchAll(page, size)`：分页查询。
- `SearchWhere(name, value)`：条件查询，默认限制到 500 条。
- `SearchId(id)`、`SearchCode(code)`、`SearchHash(hash)`：快捷查询。
- `PreLoadList()`：启用关联预加载。
- `SearchSum(field)`：聚合统计入口。

典型使用：

```go
list := entity.NewModelList[models.OrderModel](nil)
order := list.NewItem()
order.UserID = "u001"
if err := list.Add(order); err != nil {
    return nil, err
}
return order, list.Save()
```

### SearchItem

`types.SearchItem` 是统一查询条件结构，支持分页、where、sort、group、预加载和统计字段。

常用构造方式：

```go
item := list.GetSearchItem()
item.Page = 1
item.Size = 20
item.AddWhereN("UserID", "u001")
item.AddWhereNS("Amount", "isnotnull", nil)
item.AddSortN("ID", true)
err := list.LoadList(item)
```

`SearchItem.Where(db)` 会根据 GORM 命名策略转换列名，并生成 SQL where 片段与参数。

支持的特殊查询符号包括：

- `isnull`
- `isnotnull`
- `like`
- `notlike`
- `left`
- `right`
- `in`
- `notin`
- `between`

## 持久化接口

`types.IDataAction` 是底层数据操作接口：

- `Load`
- `Insert`
- `Update`
- `Delete`
- `Raw`
- `Exec`
- `Transaction`
- `Commit`
- `Rollback`
- `GetModelDB`

业务层一般不直接实现它，而是通过 `ModelList` 间接使用。需要替换数据库或做特殊存储时，应实现或组合 `IDataAction`，不要让业务路由直接依赖具体数据库。

## 适配器与数据库实现

### adapter

`adapter.DefaultAdapter` 管理本地库、远程读库、远程写库、管理库，以及 `SaveType`：

- `LocalAndRemote`
- `OnlyLocal`
- `OnlyRemote`

默认本地库当前会通过模型的 `GetLocalDBName()` 绑定全局 SQLite 实例。

### database/oltp

OLTP 数据库实现基于 GORM，包含 SQLite、MySQL、连接管理和通用 CRUD 逻辑。

`gorm.go` 负责：

- 创建、更新、删除。
- 根据 ID 或 hashcode 定位记录。
- 按 `SearchItem` 生成查询。
- 支持 `IScopes`、`ISession`、`IDBSQL` 扩展查询。
- 支持 `Preload(clause.Associations)`。

### database/olap

包含 ClickHouse、StarRocks 和业务维度模型，面向分析型数据场景。

### database/nosql

包含 BadgerDB、BoltDB、Pebble、Redis、MongoDB 等实现。当前 BadgerDB 相关代码包含锁检测、批量写入、扫描、统计、GC 和关闭控制。

## 模型类型

`entity` 下除了标准 `Model`，还有业务语义模型：

- `BaseModel`：基础资料/可发布类数据。
- `BaseOrderModel`：订单/单据类状态数据。
- `BaseRecordModel`：记录类数据。
- `BaseModelReleaseRecord`：发布记录。
- `analysis/*`：分析维度相关实体。

使用哪种基类，应由业务数据的生命周期决定。

## 服务接口

`PersistenceService` 当前注册为 `persistence` 服务，但默认没有启用管理路由，并通过 `IsCloseServerManage()` 关闭自身服务管理。数据库配置相关 API 文件保留在 `api/public`、`api/private`、`api/manage` 下，后续启用时需要再确认配置与鉴权策略。

## 开发建议

- 业务代码优先使用 `ModelList[T]`，不要直接操作 GORM。
- 模型必须嵌入 `*entity.Model` 或实现等价接口。
- 模型必须实现 `NewModel()` 初始化嵌入模型。
- 金额、价格、数量使用 `decimal.Decimal`。
- 复杂查询优先用 `SearchItem`，必要时通过 `IScopes` 或 `IDBSQL` 扩展。
- `SearchWhere` 默认适合小结果集；大列表使用 `SearchAll(page, size)`。
- 更换存储实现时优先调整 `IDataAction`/adapter，不要修改业务路由。
