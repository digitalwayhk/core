# web 目录说明

`web` 当前有效内容是 `web/admin`。它是基于 Ant Design Pro / Umi Max 的管理后台前端，与后端 `service/manage` 自动生成的管理接口配套使用。

`web/admin` 在当前主仓库中以 Git submodule 形式关联到：

```text
https://github.com/bitzoom-futures/futures.admin.git
```

## 前端整体链路

管理页面的核心链路是：

1. 后端 `service/manage` 生成管理路由和页面模型。
2. 前端运行时从后端获取菜单。
3. 菜单 URL 指向 `/main/:s/:c`。
4. `web/admin/src/pages/views/main.tsx` 读取 `s` 和 `c`。
5. `main.tsx` 将路由参数映射为后端管理 API。
6. `WayPage` 调用 `view/search/command` 接口。
7. `WayPage` 根据后端返回的 `ViewModel` 渲染工具栏、表格、表单、子表和外键选择。

## 路由入口

`web/admin/config/routes.ts` 注册了管理页面入口：

```ts
{
  name: 'main',
  icon: 'home',
  path: '/main/:s/:c',
  component: './views/main',
}
```

其中：

- `s` 表示服务名，例如 `demo`。
- `c` 表示管理控制器名，例如 `tokenmanage`。

访问：

```text
/main/demo/tokenmanage
```

会对应后端：

```text
/api/manage/demo/tokenmanage/view
/api/manage/demo/tokenmanage/search
/api/manage/demo/tokenmanage/add
/api/manage/demo/tokenmanage/edit
/api/manage/demo/tokenmanage/remove
```

## main.tsx 的后端映射

`web/admin/src/pages/views/main.tsx` 是管理页面桥接层。

它做三件事：

1. 读取 Umi 路由参数 `s`、`c`。
2. 把参数同步到全局 model `useRouteParams`。
3. 创建 `WayPage` 所需的 `init`、`search`、`execute` 方法。

关键映射：

```ts
init(() => init({
  c: 'manage/' + params.s + '/' + params.c,
  s,
}))

search(item => search({
  c: 'manage/' + params.s + '/' + params.c,
  s,
  item,
}))

execute((method, item) => execute({
  c: 'manage/' + params.s + '/' + params.c,
  m: method,
  s,
  item,
}))
```

所以 `WayPlus/request.ts` 最终请求：

```text
POST /api/manage/{service}/{controller}/view
POST /api/manage/{service}/{controller}/search
POST /api/manage/{service}/{controller}/{command}
```

## 动态菜单

`web/admin/src/app.tsx` 在 ProLayout 中配置动态菜单：

```ts
menu: {
  request: async () => {
    const menuData = await GetMenuItem();
    return menuData;
  },
}
```

`GetMenuItem()` 来自 `web/admin/src/components/WayPlus/waymenu.tsx`，它调用：

```text
GET /api/servermanage/getmenu
```

后端返回的菜单项里，管理 API URL 会被转换：

```ts
const path = childItem.url.replace('/api/manage/', '/main/');
```

也就是说后端菜单如果包含：

```text
/api/manage/demo/tokenmanage
```

前端菜单会跳转到：

```text
/main/demo/tokenmanage
```

## WayPlus 组件结构

`web/admin/src/components/WayPlus` 是配套管理组件目录。

### request.ts

封装后端请求：

- `init(params)` -> `/api/{c}/view`
- `search(params)` -> `/api/{c}/search`
- `execute(params)` -> `/api/{c}/{m}`
- `getMenu()` -> `/api/servermanage/getmenu`
- `getRouters()` -> 动态路由查询
- `getService()` -> 服务查询

内部用 `reqKeys` 防止同一接口重复请求。

### way.d.ts

定义前后端共享的数据结构类型：

- `ModelAttribute`
- `WayFieldAttribute`
- `CommandAttribute`
- `ChildModelAttribute`
- `SearchItem`
- `TableData`
- `ResultData`

这些类型与后端 `service/manage/view/model.go` 中的 `ViewModel`、`FieldModel`、`CommandModel`、`ViewChildModel`、`SearchItem`、`TableData` 对应。

### waymodel.ts

提供模型转换和数据流封装：

- 初始化命令和字段默认值。
- 把后端字段类型转换为前端控件类型。
- 转换枚举、外键、日期、数字等字段。
- 提供 `WayModel` 的 init/search/execute effects。

当前 `main.tsx` 直接给 `WayPage` 传入请求函数，没有强依赖 `WayModel` 的 dva model 方式。

### WayPage

`WayPage` 是管理页面总控组件。

职责：

- 调用 `init` 加载后端 `ViewModel`。
- 当 `autoload=true` 时自动查询列表。
- 管理 loading、选中行、分页、搜索条件、弹窗表单、导入状态。
- 渲染 `WayToolbar`、`WayTable`、`WayForm`、`ImportForm`。
- 根据命令执行新增、编辑、删除、导入、导出等操作。

普通列表页面结构：

```text
PageContainer
  WayToolbar
  WayTable
  WayForm modal
  ImportForm
```

当后端 `viewtype == "form"` 时，页面渲染为表单视图：

```text
PageContainer
  WayToolbar
  WayForm
```

### WayToolbar

根据后端 `commands` 渲染操作按钮和查询栏。

命令按钮来自后端 `CommandModel`：

- `add/create` 显示为主按钮。
- `remove/delete` 显示为危险按钮。
- `edit/update` 显示编辑图标。
- `ImportData/import` 显示导入图标。
- `ExportData/export` 显示导出图标。

按钮是否可点由 `isselectrow`、`selectmultiple`、当前选中行数共同决定。

### WayTable

根据后端 `fields` 渲染 Ant Design Table。

能力：

- 字段列自动生成。
- 排序转换为后端 `SearchItem.sortList`。
- 单选/多选行。
- 双击进入编辑。
- 外键字段展示关联对象显示名。
- 枚举字段显示枚举文本。
- boolean 显示“是/否”。
- datetime 使用 dayjs 格式化。
- 子模型通过展开行和 Tabs 展示。

### WayForm

根据后端 `fields` 渲染 Ant Design Form。

能力：

- 自动生成表单项。
- 根据字段类型选择 `WayTextBox` 控件。
- 根据字段必填、长度、类型生成校验规则。
- 支持 modal/drawer 风格弹窗。
- 支持子表编辑。
- 提交时合并表单值和子表数据。
- 子表新增/编辑/删除会设置 `modelState`，供后端识别。

### WayTextBox

字段输入控件选择器。

根据 `WayFieldAttribute` 自动选择：

- 普通文本输入。
- 数字输入。
- Switch。
- DatePicker。
- Select。
- 外键 Search。

外键字段会通过 `onSearchBefore` 打开选择表格，并把选中行的显示字段和值回填到当前字段。

### WayTable/importform.tsx 和 exportform.tsx

提供管理页面导入和导出能力。`WayPage` 根据命令名 `ImportData/importdata`、`ExportData/exportdata` 触发。

## 后端 ViewModel 到前端组件的对应关系

| 后端字段 | 前端用途 |
| --- | --- |
| `ViewModel.Title` | 页面或弹窗标题 |
| `ViewModel.AutoLoad` | 初始化后是否自动查询 |
| `ViewModel.ViewType` | 决定列表页或表单页 |
| `ViewModel.Fields` | 表格列、表单项、查询字段 |
| `ViewModel.Commands` | 工具栏按钮 |
| `ViewModel.ChildModels` | 子表/展开行/表单子表 |
| `FieldModel.Type` | 控件类型与显示转换 |
| `FieldModel.ComVtp` | Select 枚举 |
| `FieldModel.Foreign` | 外键搜索和显示 |
| `CommandModel.Command` | 后端执行命令名 |
| `CommandModel.IsSelectRow` | 是否需要选中行 |
| `CommandModel.SelectMultiple` | 是否允许多选 |

## 开发建议

- 新增后台管理功能优先在后端 `service/manage` 配好模型和 ViewModel，让前端自动渲染。
- 前端不要为每个模型手写页面，除非 WayPlus 无法满足交互需求。
- 后端字段 `json` 名称要和前端字段访问保持一致。
- 外键、枚举、日期格式优先在后端 `FieldModel` 中描述。
- 菜单由后端 `servermanage/getmenu` 提供，前端只负责 URL 转换和渲染。
- 修改动态管理页时优先看 `views/main.tsx`、`WayPage`、`request.ts`。
