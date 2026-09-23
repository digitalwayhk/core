# 管理界面能力目录

本示例专门用来验证 `web/admin` 管理后台（`src/manage-ui`）的完整能力。模型字段和 Manage 的 View 配置覆盖：主查询、外键选择、子表展开、增删改、提交发布、自定义表单命令、导入导出、枚举分段筛选、高级搜索、自定义列，以及字符串、整数、小数、布尔、日期时间、密码和备注控件。

本示例的“完整能力”指管理界面 schema 和前后端交互，不表示它演示了 Core 的全部 Manage 后端架构。这里的两个 Manage 都直接嵌入 `ManageService[T]`，便于集中展示 UI；多层 Manage 继承参考 `examples/03-shop-inheritance`，服务级公共 Hook 参考 `examples/06-shop-microservices`。

## Manage 结构与继承边界

本示例使用一层直接组合：

```text
ManageService[Category]    → CategoryManage
ManageService[CatalogItem] → CatalogItemManage
```

构造时必须把最终具体 Manage 作为 owner：

```go
own := &CatalogItemManage{}
own.ManageService = managepkg.NewManageService[models.CatalogItem](own)
```

这样 View、Parse、Validation、Do、Search 和自定义命令才能回调到 `CatalogItemManage`。不能把 `nil` 或中间基座传给 `NewManageService`。

真实服务需要复用数据源、服务公共能力或领域规则时，推荐结构为：

```text
HookedManageService[T]
└── ServiceManage[T]
    └── BaseDataManage[T] / BusinessManage[T]
        └── ConcreteManage
```

每层构造函数继续传递最终 ConcreteManage owner。Go 嵌入不会自动调用父级同名方法：具体 Manage 覆盖 `SearchAfter`、`DoAfter`、`ViewFieldModel` 等方法时，需要显式调用父层，才能保留父层行为。完整规则见 `docs/ai/core-skill/manage.md` 的“Manage 继承与 Hook 生命周期”。

本示例中的两个 `GetList()` 都只调用 `models.NewManageModelList[T]()`。数据库连接和 DataAction 由 models 层决定，`api/manage` 不创建或选择数据库适配器。

## 本示例使用的 Hook

| Hook / 扩展点 | 本示例用途 |
| --- | --- |
| `GetDefaultItems` | 空表首次 Search 时写入分类和资料条目演示数据 |
| `ValidationAfter` | 根据 Add/Edit/Remove 调用模型校验 |
| `DoAfter` | CatalogItem 新增或修改成功后保存子表 |
| `SearchAfter` | 先执行框架默认数据逻辑，再加载子表和外键对象 |
| `ViewModel` | 配置页面标题、说明、自动加载和分段筛选 |
| `ViewFieldModel` | 配置字段标题、必填、控件、精度、搜索和可见性 |
| `ViewCommandModel` | 配置导入、导出、复制命令及新增下拉分组 |
| `ViewChildModel` | 配置 Lines、Specs 两个可编辑子表 |
| `GetLocaleTitle` | 提供目录和页面中英文标题 |

`CatalogItemManage.SearchAfter` 先调用 `ManageService.SearchAfter`，以保留空表默认数据逻辑，再补充子表和外键。`DoAfter` 的 Add/Edit 分支目前直接返回，是因为本示例直接嵌入的父层没有后置业务；如果以后改成 `ServiceManage` 基座，需要明确父层与具体层的调用顺序，不能无意截断父层公共逻辑。

## 自定义命令的实例与绑定

现有的 `api/manage/importdata.go`、`exportdata.go`、`cloneitem.go` 已经演示 `manage.Operation`，不需要再增加一套命令示例。三者都以值嵌入 `manage.Operation[models.CatalogItem]`，并实现 `New(instance)` 创建请求级实例。请求体由 `Operation.Parse` 绑定到 `operation.Model`，不要再定义一套平行 Request 并手工复制字段。

本示例的 Import/Export 只返回前端提示，`CloneItem` 是用于验证表单命令的演示性本地写入。08 不定义生产服务自定义写命令的统一权限和横切处理方式；相关契约待权限方案明确后补充，不能从本示例推导。

标准 Add/Edit/Remove 的概念顺序是：

```text
ParseBefore → Bind → ParseAfter
→ ValidationBefore → 标准校验 → ValidationAfter
→ DoBefore → 持久化 → DoAfter
```

主列表 Search 使用独立的 `SearchBefore → LoadList → SearchAfter` 管道；外键和子表查询分别使用 `ForeignSearchBefore/After`、`ChildSearchBefore/After`。普通筛选、排序和分页不要通过 `SearchBefore(stop=true)` 自行查库返回，否则会绕过标准 Search 管道。

## 启动

首次运行会在可执行文件所在目录自动创建 `etc/server.json` 和 `etc/catalog.json`：

```bash
cd examples/08-admin-manage-ui/main
go build -o admin-manage-ui .
./admin-manage-ui -view 8888
```

默认服务地址为 `http://127.0.0.1:8081`，管理后台为 `http://127.0.0.1:8888`。`-view` 是开发管理后台（HtmlServer）端口，内嵌当前仓库的 `web/admin` 产物。若日志出现 `port already in use`，用 `-p`、`-grpc` 换端口。

本地用 `web/admin` 的 `yarn start`（默认 `http://127.0.0.1:8000`）时，必须把 `web/admin/config/proxy.ts` 的 `/api/` 指到 HtmlServer，例如 `http://127.0.0.1:8888`。指到 `http://localhost`（80 端口）而视图在 8888 时，页面会 504「请求失败」。正式内嵌后台仍走 `pkg/server/run/dist`，直接打开 `-view` 端口即可。

## 界面操作

1. 打开 `http://127.0.0.1:8888`，开发模式自动签发管理令牌。
2. 左侧「内部系统管理」→「菜单管理」→ 工具栏「更新菜单」，把当前进程的 Manage API 同步成侧栏菜单。
3. 先打开「分类管理」，空表会写入电子 / 图书 / 配件三条演示数据，可验证枚举分段筛选、提交和发布。
4. 再打开「资料条目」，空表会按已有分类写入带明细行和规格参数的演示条目，可验证：
   - 工具栏筛选、高级搜索、自定义列
   - 外键选择分类
   - 展开行切换「明细行」「规格参数」两个子表页签
   - 新增 / 编辑表单中的全部字段控件和多子表增删
   - 新增右侧下拉里的导入、导出、复制（`issplit` / `splitname`）
   - 提交 / 发布

## 接口

| 类型 | 路径 | 说明 |
| --- | --- | --- |
| Manage | `/api/manage/catalog/categorymanage/{view,search,add,edit,remove,submit,release}` | 分类管理 |
| Manage | `/api/manage/catalog/catalogitemmanage/{view,search,add,edit,remove,submit,release,importdata,exportdata,cloneitem}` | 资料条目 |
| Public | `GET /api/catalog/getcategories?id=&name=` | 分类 ID 精确、名称模糊组合筛选 |

获取管理令牌：

```text
GET http://127.0.0.1:8081/api/servermanage/testtoken?userid=admin&type=1
```

## 前端能力对照

| 管理界面能力 | 本示例如何提供 |
| --- | --- |
| 主查询 / 排序 / 分页 | 两个 Manage 都注册 Search，`AutoLoad=true` |
| 外键选择 | `CatalogItem.Category` + `gorm:"foreignkey:ID;references:CategoryID"` |
| 多子表展开与表单子行 | `CatalogItem.Lines` / `Specs` + `SaveChildren` 按 `modelState` 落库 |
| 枚举分段筛选 | `Kind` + `ComBox` + `showInComvtp` |
| 提交 / 发布 | 嵌入 `entity.BaseModel`，注册 Submit / Release |
| 导入 / 导出 | 命令名 `importdata` / `exportdata`，前端拦截后走 Excel |
| 命令分割下拉 | `ViewCommandModel` 设 `issplit` + `splitname=add`，导入/导出/复制挂在新增下 |
| 自定义表单命令 | `CloneItem` 且 `editshow=true` |
| 密码 / 备注 / 日期 / 小数 / 布尔 | `Secret` `Note` `PublishedAt` `Price` `Enabled` |
| 高级搜索 / 自定义列 | 前端本地能力，本页提供足够可搜可显示字段 |

## 集成测试

```bash
go test ./examples/integration/08-admin-manage-ui -count=1
```
