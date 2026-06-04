# service 目录说明

`service` 目录存放 core 的基础业务服务能力。当前有效内容集中在 `service/manage`，它提供通用管理界面后端能力：初始化视图、查询、新增、编辑、删除、提交、发布等标准功能，并允许业务服务通过继承和 hook 使用极少代码扩展。

## manage 的定位

`service/manage` 是后端管理界面的自动 CRUD 框架。业务只需要：

1. 定义模型。
2. 创建一个管理服务结构体。
3. 嵌入 `*manage.ManageService[T]`。
4. 在业务服务 `Routers()` 中追加 `NewXxxManage().Routers()...`。

示例：

```go
type TokenManage struct {
    *manage.ManageService[models.TokenModel]
}

func NewTokenManage() *TokenManage {
    own := &TokenManage{}
    own.ManageService = manage.NewManageService[models.TokenModel](own)
    return own
}

func (own *TokenManage) ViewModel(model *view.ViewModel) {
    model.AutoLoad = true
}
```

这会自动提供 7 个管理路由：

- `view`
- `search`
- `add`
- `edit`
- `remove`
- `submit`
- `release`

路径格式：

```text
/api/manage/{serviceName}/{manageStructNameLower}/{command}
```

例如：

```text
/api/manage/demo/tokenmanage/view
/api/manage/demo/tokenmanage/search
/api/manage/demo/tokenmanage/add
```

## 核心结构

### ManageService[T]

`ManageService[T]` 聚合所有标准管理操作：

```go
type ManageService[T pt.IModel] struct {
    View    *View[T]
    Search  *Search[T]
    Add     *Add[T]
    Edit    *Edit[T]
    Remove  *Remove[T]
    Submit  *Submit[T]
    Release *Release[T]
    Req     st.IRequest
}
```

`Routers()` 返回上述 7 个路由。每个路由都实现 `IRouter`，因此可以被 `pkg/server` 正常注册和执行。

### View

`View[T]` 负责生成前端页面模型 `view.ViewModel`，包括：

- 页面名称、标题、服务名。
- 字段列表。
- 命令列表。
- 子模型列表。
- 是否自动加载。
- 是否显示左侧查询工具栏。

字段来自模型反射，命令来自 `ManageService.Routers()`。

业务可以通过 hook 调整：

- `ViewModel(model *view.ViewModel)`
- `ViewFieldModel(model interface{}, field *view.FieldModel)`
- `ViewCommandModel(cmd *view.CommandModel)`
- `ViewChildModel(child *view.ViewChildModel)`

### Search

`Search[T]` 负责列表查询、外键查询和子表查询。

前端提交的是 `view.SearchItem`，后端会转换为 `pkg/persistence/types.SearchItem`，再通过 `ModelList.LoadList` 查询。

扩展点：

- `SearchBefore`
- `SearchAfter`
- `ForeignSearchBefore`
- `ForeignSearchAfter`
- `ChildSearchBefore`
- `ChildSearchAfter`
- `OnSearchData`

### Operation

`Operation[T]` 是 `Add`、`Edit`、`Remove`、`Submit`、`Release` 的基础操作类。

它负责：

- 绑定请求数据到模型。
- 获取 `ModelList[T]`。
- 调用 `ParseBefore` / `ParseAfter`。
- 调用 `ValidationBefore` / `ValidationAfter`。
- 暴露当前 `Model`。

### Add

`Add[T]` 会：

1. 绑定模型。
2. 调用 `DoBefore`。
3. 当模型 ID 为空时使用 `req.NewID()` 设置 ID。
4. 设置 `CreatedUser`、`CreatedUserName`。
5. `list.Add(model)`。
6. `list.Save()`。
7. 调用 `DoAfter`。

### Edit

`Edit[T]` 会：

1. 校验 ID 不为空。
2. 按 ID 读取旧数据。
3. 合并提交字段到旧数据。
4. 设置 `UpdatedUser`、`UpdatedUserName`。
5. `list.Update(oldItem)`。
6. `list.Save()`。

### Remove

`Remove[T]` 会按 ID 查询目标数据，存在后执行 `list.Remove(model)` 和 `list.Save()`。

### Submit / Release

`Submit[T]` 用于提交数据状态，当前会尝试把 `BaseModel.State` 从 0 改为 1。

`Release[T]` 用于发布类操作，当前主要执行更新逻辑，并预留 `DoBefore` 扩展。

## ViewModel 数据结构

`service/manage/view/model.go` 定义前端需要的结构。

### ViewModel

表示一个管理页面：

- `Name`
- `ServiceName`
- `Title`
- `ViewType`
- `AutoLoad`
- `Fields`
- `Commands`
- `ChildModels`
- `ShowComvtp`
- `AutoSearch`

### FieldModel

表示一个字段：

- 字段名、属性字段名、标题。
- 是否禁用、可见、可编辑、必填、可搜索。
- 字段类型、长度、精度、最小值。
- 下拉枚举 `ComVtp`。
- 外键信息 `Foreign`。
- 日期时间格式。

### CommandModel

表示一个操作按钮：

- `command`
- `name`
- `title`
- 是否需要选中行。
- 是否支持多选。
- 是否弹窗确认。
- 是否拆分按钮。
- 图标、排序、可见、禁用。

### ViewChildModel

表示子表/子模型：

- 是否允许新增、编辑、删除、选择、勾选。
- 外键字段。
- 关联引用字段。
- 排序。

## 继承与扩展方式

业务管理服务通过实现接口方法扩展默认行为。

常用扩展：

```go
func (m *OrderManage) ViewModel(model *view.ViewModel) {
    model.Title = "订单管理"
    model.AutoLoad = true
}

func (m *OrderManage) ViewFieldModel(model interface{}, field *view.FieldModel) {
    if field.IsFieldOrTitle("Amount", "数量") {
        field.Required = true
        field.Precision = 8
    }
}

func (m *OrderManage) SearchBefore(sender interface{}, req st.IRequest) (interface{}, error, bool) {
    return nil, nil, false
}

func (m *OrderManage) DoBefore(sender interface{}, req st.IRequest) (interface{}, error, bool) {
    return nil, nil, false
}
```

`stop == true` 表示拦截默认逻辑，由 hook 返回结果。

## 与前端的关系

`service/manage` 输出的 `ViewModel`、`FieldModel`、`CommandModel` 和查询结果，会被 `web/admin/src/components/WayPlus` 消费。

后端标准路由：

- `view`：返回页面模型。
- `search`：返回 `{ rows, total }`。
- `add/edit/remove/submit/release`：执行命令并返回结果。

前端根据 `/main/:s/:c` 路由参数生成对应 API：

```text
/api/manage/{s}/{c}/view
/api/manage/{s}/{c}/search
/api/manage/{s}/{c}/{command}
```

## 开发建议

- 简单管理页面只写 `NewXxxManage()` 和少量 `ViewModel` 配置。
- 字段显示、必填、下拉、外键优先通过 `ViewFieldModel` 调整。
- 查询过滤、默认数据、权限裁剪优先通过 `SearchBefore` / `SearchAfter`。
- 新增、编辑、删除的业务规则优先通过 `ValidationBefore` / `DoBefore` / `DoAfter`。
- 需要完全自定义返回时，在 hook 中返回 `stop=true`。
- 管理服务应该保持轻量，不要把复杂业务流程全部堆在前端。
