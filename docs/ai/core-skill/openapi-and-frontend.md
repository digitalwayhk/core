# OpenAPI 与前端调用约定

## OpenAPI 文档接口

框架内置 **OpenAPI 3.0.1** 文档生成器，服务启动后无需额外配置即可使用。

### 访问地址

```
# 匿名访问：对外协作、Swagger UI、Postman、Apifox
GET http://localhost:{port}/api/openapi

# 受保护访问：内部联调与完整接口排查
GET http://localhost:{port}/api/internal/openapi
GET http://localhost:{port}/api/internal/openapi?service={serviceName}
```

> 两个端点均返回标准 OpenAPI 3.0.1 JSON。`/api/internal/openapi` 必须通过
> `ServerManageAuth`，并返回 `Cache-Control: private, no-store`。

### 包含内容

| 端点 | 内容 |
|------|------|
| **`/api/openapi`** | 匿名外部视图；包含常规 Public 与 Private 路由，过滤 `WithInternalCallers()` 非空的内部专用路由，并且不输出 `x-internal-callers` |
| **`/api/internal/openapi`** | 完整内部视图；包含 Public、Private 和内部专用路由，保留 `x-internal-callers`，支持按 `service` 查询参数过滤 |
| **服务分组（Tags）** | 每个服务名作为一个 Tag，多服务时分组清晰 |
| **Server URL** | 每个服务的实际访问地址（含端口） |
| **Private 安全要求** | Private 路由自动标注 Bearer 安全要求；服务实现 `IHMACAuthProvider` 时追加 HMAC 备选，语义为 Bearer OR HMAC |

### 可选 HMAC 机器可读契约

服务实现 `IHMACAuthProvider` 时，Private operation 保留 Bearer 安全要求，并增加一组同时要求 AccessKey、Timestamp、Nonce、Signature Header 的 HMAC 安全要求。OpenAPI 多个 `security` 对象是 OR，同一对象内多个 scheme 是 AND。

- `x-core-hmac-auth.headers` 反映当前 `ServerConfig.HMACAuth` 实际 Header 名。
- `x-core-hmac-auth.available_inputs` 只表示 Core 能交给 Provider 的可用字段；签名算法、字段选择与规范化顺序均由 Provider 定义，不得把该列表当作固定签名串。
- 仅当 `ServerOption.IsWebSocket=true` 时，`x-core-websocket-hmac-logon` 才描述 `/ws` 上 `event=sub` 、`channel=logon` 的 `data_schema`，其中 `apiKey`、`timestamp`、`nonce`、`signature` 必填，`recvWindow` 可选。REST-only 服务不宣告该扩展。
- Bearer 与 HMAC 同时出现时 Bearer 优先；Manage / ServerManage 不启用 HMAC。

> ⚠️ **Manage 路由不包含在 OpenAPI 文档中**，仅 Public + Private 路由会被导出。
> Manage 的前端契约权威是 `service/manage/view/model.go` 的 `ViewModel` /
> `FieldModel` / `CommandModel` / `SearchItem`，对应管理端
> `web/admin/src/manage-protocol/`（类型、URL、字段转换、可注入 HTTP 客户端）。
> 不要用 `/api/openapi` 生成 Manage 页面，也不要把 Ant Design 页面壳当成协议。

### curl 示例

```bash
# 匿名获取对外文档
curl -s http://localhost:8080/api/openapi | jq .

# 使用 ServerManage token 获取完整内部文档
curl -s \
  -H "Authorization: Bearer ${SERVER_MANAGE_TOKEN}" \
  "http://localhost:8080/api/internal/openapi?service=orders" | jq .
```

### 返回格式（OpenAPI 3.0.1）

```json
{
  "openapi": "3.0.1",
  "info": {
    "title": "Open API",
    "description": "Project API Document includ private and public",
    "version": "1.0.0"
  },
  "servers": [
    { "url": "http://localhost:8080/" }
  ],
  "tags": [
    { "name": "trades", "description": "http://localhost:8080/" }
  ],
  "paths": {
    "/api/trades/placeorder": {
      "post": {
        "tags": ["trades"],
        "summary": "PlaceOrder",
        "operationId": "api_trades_placeorder",
        "requestBody": { ... },
        "responses": { ... }
      }
    },
    "/api/trades/getorders": {
      "get": {
        "tags": ["trades"],
        "summary": "GetOrders",
        "parameters": [ ... ],
        "security": [{ "Bearer": [] }]    // Private 路由自动加此字段
      }
    }
  },
  "components": {
    "securitySchemes": {
      "Bearer": {
        "type": "http",
        "scheme": "bearer",
        "bearerFormat": "JWT",
        "description": "Get TestToken from http://localhost:8080/api/servermanage/testtoken?userid=12345"
      }
    }
  }
}
```

### 与 Swagger UI 集成

将 OpenAPI 端点 URL 填入 Swagger UI 的 `url` 参数即可实时预览：

```html
<!-- swagger-ui dist -->
<script>
  SwaggerUIBundle({
    url: "http://localhost:8080/api/openapi",
    dom_id: '#swagger-ui',
  })
</script>
```

---


## 前端调用 API（Web 集成）

前端基于 **Umi + Ant Design Pro（React）**，框架提供了一套约定式的 API 调用机制。
所有接口均为 `POST`（除 TestToken、getmenu 为 `GET`），响应统一为 `ResultData` 结构。

### 统一响应格式

所有后端接口（Public / Private / Manage）均返回同一结构：

```typescript
interface ResultData {
  success: boolean;              // true = 成功
  errorCode?: number;            // 业务状态码（json: errorCode）
  errorMessage?: string;         // 错误描述（json: errorMessage）
  data?: TableData | object;     // 业务数据
  showType?: number;             // 前端展示方式（json: showType；0=静默 1=warn 2=error 3=notification）
  traceid?: string;
  host?: string;
  // 历史读取别名：部分前端仍回退 result.code / result.message
}

interface TableData {
  rows: any[];    // 数据行（Search 结果）
  total: number;  // 总记录数（用于分页）
  tag?: string;   // 原样回传，前端不解释
}
```

### 语言协商（`X-Locale`）

管理端当前语言通过请求头 `X-Locale` 传给后端，合法值 `zh-CN`、`en-US`，缺省与无法识别一律回退 `zh-CN`。

- 拦截器在 `src/requestErrorConfig.ts` 的 `requestInterceptors` 里与 Casdoor token 同处注入 `getLocale()`，所有走 Umi `request` 的调用都会带上，包括 `getmenu`、`view`、`search` 和各命令。
- **不要用查询参数传语言**（污染缓存与书签），后端也**不读 Cookie / `umi_locale`**（`localStorage` 不随请求发送），更不会用浏览器 `Accept-Language` 覆盖 `X-Locale`。
- `getmenu` 返回的 `title` 已经是当前语言，前端继续用 `childItem.title ?? childItem.name` 渲染，**不要在前端用语言包翻译业务 `ViewModel`**。`titleen` 仅供调试，展示权威始终是 `title`。
- `SelectLang` 只暴露 `zh-CN` 与 `en-US`；后端第一期只有中英文案，其它语言会切出未翻译的界面。切换后沿用 Umi 默认整页刷新，刷新后重新请求 `getmenu` 与当前页 `view`。

后端如何声明中英标题见 [manage.md](manage.md) 的「管理后台中英标题」。

### URL 规则与三个核心请求

**URL 规则：**
```
Public  API:  POST /api/{serviceName}/{structNameLower}
Private API:  POST /api/{serviceName}/{structNameLower}   （需要 Authorization: Bearer token）
Manage  View: POST /api/manage/{serviceName}/{controllerName}/view
Manage  Srch: POST /api/manage/{serviceName}/{controllerName}/search
Manage  Cmd:  POST /api/manage/{serviceName}/{controllerName}/{command}
GetMenu:      GET  /api/servermanage/getmenu
```

> ⚠️ Public 和 Private API 的 URL 格式相同；区别在于 Private 需要携带 Bearer token，框架通过路由的 PathType 来验证。

规范客户端在 `web/admin/src/manage-protocol/`，Admin 通过 `@/services/manage` 注入 Umi `request`（认证与 `X-Locale` 仍由拦截器处理）：

```typescript
import { createManageClient } from '@/manage-protocol';

const client = createManageClient({ request: myRequest });
await client.view({ service: 'demo', controller: 'ordermanage' });
await client.search({ service: 'demo', controller: 'ordermanage' }, searchItem);
await client.execute({ service: 'demo', controller: 'ordermanage' }, 'add', formData);
```

Admin 页面仍可使用兼容封装（`c` 是 `manage/{service}/{controller}`）：

```typescript
import { init, search, execute } from '@/services/manage';

const schema = await init({ c: 'manage/demo/ordermanage', s: 'demo' });
const result = await search({ c: 'manage/demo/ordermanage', s: 'demo', item: searchItem });
const saved = await execute({ c: 'manage/demo/ordermanage', m: 'add', s: 'demo', item: formData });
```

### Manage Search 的三种模式

三种查询走**同一个**接口：

```
POST /api/manage/{service}/{controller}/search
```

不要为外键或子表另找 URL，也不要把子表行指望主查询一次带齐。后端 `service/manage/search.go` 的 `Search.Do` 只看请求体分流，**顺序固定**：

1. `field` **并且** `foreign` 都有值 → **外键关联查询**（查关联表，不是当前主表）
2. 否则 `parent` **并且** `childmodel` 都有值 → **子表查询**（查当前主表某一行的数组子集）
3. 否则 → **主模型查询**（当前 Manage 的主表，筛选 + 排序 + 分页）

混用标记会走错分支：主查询或子查询的 body 里一旦带了 `field`+`foreign`，就会被当成外键查询。先 `POST .../view` 看 schema，再决定用哪一种。

`page` 从 1 开始，缺省 1；`size` 缺省 10。`sortList` 为空时后端默认 `id DESC`。

#### 1. 主模型查询

当前 Manage 的主表列表。工具栏筛选、表头排序、底部分页都是这种。

**何时用：** 打开 `/main/{service}/{controller}` 后查列表；`view` 返回的就是这个主模型。

**请求体只允许这些**（不要带 `field`/`foreign`/`parent`/`childmodel`）：

```json
{
  "page": 1,
  "size": 20,
  "whereList": [
    { "name": "name", "symbol": "like", "value": "%iPhone%" },
    { "name": "price", "symbol": ">", "value": "1000", "relation": "and" }
  ],
  "sortList": [{ "name": "ID", "isdesc": true }]
}
```

| 字段 | 含义 |
| --- | --- |
| `page` / `size` | 主表分页 |
| `whereList` | 主表筛选。`name` 用 View 字段的 `field`（json 名）或 `porpfield`（Go 属性名），后端 `ViewField` 两者都认；不要发明列名 |
| `sortList` | `{ name, isdesc }`，`name` 规则同上。前端表头排序会转成 `porpfield` |

**响应** `data` 为 `TableData`：`{ rows, total, tag }`。`rows` 是主表行。外键列通常只带显示名（`foreign.onedisplayname`，默认 `name`），**不会**在这里分页拉出整张关联表。子表数组也**不会**按子查询分页返回；要子表数据必须用模式 3。

#### 2. 外键关联查询

主模型上 `foreign.isfkey === true` 的字段（View 由模型里的关联 struct/`gorm:"foreignKey;references"` 生成）可以弹出关联表选择器。查询的是**关联对象那张表**。

**何时用：** 表单/搜索栏的外键 Search 控件；或需要列出某外键字段对应的外部表行。先在 `view.fields` 里找到目标字段，确认 `foreign.isfkey`，**原样带上这个 field 和它的 foreign**，不要手搓 ForeignModel。

**请求体：**

```json
{
  "page": 1,
  "size": 10,
  "field": { "...来自 view.fields 的整段 FieldModel..." },
  "foreign": { "...该字段的 foreign 整段 ForeignModel..." },
  "value": "可选，选择器输入框关键字",
  "whereList": [{ "name": "name", "symbol": "like", "value": "%苹果%" }],
  "sortList": []
}
```

| 字段 | 含义 |
| --- | --- |
| `field` + `foreign` | 模式开关，必须成对出现。`foreign.oneobjectfield` 是主模型上的关联对象属性，`foreign.model` 是关联表自己的 View |
| `page` / `size` / `whereList` / `sortList` | 作用在**关联表**上，不是主表 |
| `value` | 选择器搜索框原文，可选 |

**响应** `data` 为 `ForeigData`：`{ rows, total, model }`。`rows` 是关联表行；`model` 是关联表 ViewModel，供选择表格渲染。不要按主表 `TableData` 去读，也不要换到另一个 Manage 的 `/search`（除非你确实在管理那张表）。

选中一行后，写回主模型的是 `foreign.oneobjectfieldkey`（默认 `id`）对应的值，显示文本用 `foreign.onedisplayname`（默认 `name`）。

#### 3. 子表查询

主模型 View 的 `childmodels` 来自模型里的**数组/切片字段**（`[]*Child` + `gorm:"foreignKey:;references:"`）。主表每一行都可以展开，再查这一行对应的子表行。

**何时用：** `view.childmodels` 非空；用户展开某一主表行，或要拉某行的子集。主查询不会替代它。

**请求体：**

```json
{
  "page": 1,
  "size": 10,
  "parent": { "id": 123 },
  "childmodel": { "...来自 view.childmodels[i] 的整段 ViewChildModel..." },
  "whereList": [],
  "sortList": []
}
```

| 字段 | 含义 |
| --- | --- |
| `parent` | **当前主表行**（至少含主键；若 `childmodel.references` 非空，还要带该属性）。前端展开行时传整行 |
| `childmodel` | 模式开关。用 `view.childmodels` 里的那一项，不要只传名字。`name` 是主模型上的切片字段名；`foreignKey` 是子表指向主表的列 |
| `page` / `size` / `whereList` / `sortList` | 作用在**子表**上 |

后端会自动追加子表条件：`references` 为空时 `foreignKey = parent.id`，否则 `foreignKey = parent[references]`。调用方不必自己拼这条主外键 where，但不要覆盖掉后端追加的条件。

**响应** `data` 为该行的子表 `TableData`：`{ rows, total }`。前端把 `rows` 挂到主表行的 `childmodel.name` / `propertyname` 上。换一行必须重新查；不要把 A 行的子表当成 B 行的。

#### 共用的 where / sort

```typescript
interface SearchWhere {
  name: string;      // View 字段 field 或 porpfield
  symbol: string;    // 见下表
  value?: unknown;   // 数字也可传字符串，后端按字段类型转
  relation?: string; // and / or / not；从第二条起缺省 AND
  prefix?: string;   // 拼在列名左侧的原始片段，调用方不要发明
  suffix?: string;   // 拼在条件右侧的原始片段，调用方不要发明
}
interface SearchSort {
  name: string;
  isdesc: boolean;
}
```

`symbol`（大小写不敏感）：

| symbol | SQL | 值 |
| --- | --- | --- |
| `=` `!=` `>` `>=` `<` `<=` | 比较 | 标量 |
| `like` / `notlike` | LIKE / NOT LIKE | **原样**，需要模糊时自己加 `%` |
| `left` / `right` | LIKE | 后端补 `value%` 或 `%value` |
| `in` / `notin` | IN / NOT IN | 数组 |
| `between` | BETWEEN | 长度为 2 的数组 |
| `isnull` / `isnotnull` | IS NULL / IS NOT NULL | 不需要值 |

日期范围不要用一条 `between` 糊弄：前端会拆成 `>=` 起始 **and** `<=` 结束两条。

#### 调用时不要做的事

- 主查询 body 里带 `field`+`foreign` 或 `parent`+`childmodel`。
- 为外键/子表改 URL，或对关联表再猜一个 `{controller}/search`。
- 假设主查询 `rows[]` 里已经带好分页后的子表。
- `sortList` 传 `string[]`。
- `like` 不传通配符却期望模糊匹配（要用 `%x%`，或改用 `left`/`right`）。
- 子查询只传 `childmodel.name` 而不传 `parent` 行。
- 外键查询手写 `foreign`，不复用 `view.fields[].foreign`。

### Manage View：先拿 schema 再操作

`POST /api/manage/{service}/{controller}/view` **没有业务请求体**（空 JSON 即可）。返回的 `data` 是 `ViewModel`，前端所有表格、表单、按钮、外键选择、子表展开都只认这份 schema。不要用 `/api/openapi` 推断 Manage 页面。

**schema 从哪来（`service/manage/view.go`）：**

| View 字段 | 来源 |
| --- | --- |
| `fields` | 主模型的标量/`time.Time`/`decimal` 属性。`field` = json 名（提交用），`porpfield` = Go 属性名 |
| `fields[].foreign` | 主模型上的关联 struct/指针（`gorm:"foreignKey;references"`），`isfkey=true` 才能走外键查询 |
| `childmodels` | 主模型上的数组/切片字段。`name` 是切片属性名，`foreignKey`/`references` 来自 gorm tag |
| `commands` | `Manage.Routers()` 里除 `View`/`Search` 以外的每个 Router，一条命令 |
| `autoload` | 默认 `true`（`ViewModel` 钩子）。为 true 时前端拿到 schema 后立刻做一次主查询 |
| `viewtype` | 空 = 列表页；`form` = 整页表单，工具栏命令直接提交页面表单 |
| `title` | 页面标题，已按 `X-Locale` 填好，前端不要再翻译 |

命令怎么进 schema：`RouterToLocaleCommand` 用 Router **结构体名**（去掉泛型括号）生成：

- `command`：小写，等于 URL 最后一段，例如 `Add` → `add`
- `name`：结构体名，稳定键，不随语言变
- `title`：当前语言的展示文案
- `View` / `Search` **不会**出现在 `commands` 里
- 默认：`add` 不要求选中行、不确认；其余 `isselectrow=true`；`add`/`edit` 以外 `isalert=true`

字段默认：`visible`/`isedit`/`issearch`/`sorter` 为 true。`ID` 隐藏且不可编辑、不可搜索。`IBaseModel` 上 `code` 可编辑则必填，`state` 禁用，`TraceID` 隐藏，时间戳不可编辑。业务可通过 `ViewFieldModel` / `ViewCommandModel` / `ViewChildModel` / `ViewModel` 改这些标记；**没出现在 schema 里的列或按钮，调用方不要自己发明。**

只读管理：不要注册 `add`/`edit`/`remove` 路由，这样 `commands` 里就不会有它们，打对应 URL 是 404。不要靠 handler 里拒绝来伪装只读。

前端 `initmodel` 还会把 `bool`→`boolean`、`date`→`datatime`、`uint`/`bigint`→`int`，并给未设的 `showComvtp`/`autoSearch`/`showCustomizeSetting`/`showAdvancedSearch` 补 `true`。这些是 UI 规范化，不是后端 json。

### Manage 命令执行

```
POST /api/manage/{service}/{controller}/{command}
```

`{command}` 必须等于 `view.commands[].command`（小写）。请求体是 **模型字段本身**（json 名与 `fields[].field` 一致），由 `Operation.Parse` 直接 `Bind` 到 `list.NewItem()`。

**不要**包一层 `{ "model": { ... } }`。`Operation` 结构体虽有 `json:"model"` 字段，Parse **不会**按这个包一层绑定。

| command | 前端怎么点 | 请求体 | 后端做什么 |
| --- | --- | --- | --- |
| `add` | 开空表单，提交表单值 | 新行字段，**不要传 id**（服务端 `req.NewID()`） | `Add`：写入 CreatedUser，`list.Add` + `Save` |
| `edit` | 必须先选中一行，开表单，底稿是**整行** | 整行 + 修改；**id 必填且非 0** | `Edit`：按 id 找旧行，覆盖除 TraceID/Hashcode 外的字段，`Update` + `Save` |
| `remove` | 必须先选中一行，确认框 | 该行（至少含 id） | `Remove`：按 id 找行，`Remove` + `Save`。`BaseModel.State>0` 通常不能删 |
| `submit` | 必须先选中一行，确认框 | 该行（至少含 id） | 找到行；若嵌入 `BaseModel` 且 `state==0`，改为 `1` 后保存 |
| `release` | 必须先选中一行，确认框 | 该行（至少含 id） | 优先 `IReleaseHook.OnRelease`，否则 `DoBefore`，再 `Update` |
| 自定义 | 见 `editshow` / `isselectrow` | 见下 | 值嵌入 `manage.Operation[T]` 的 Router，`Do` 默认原样返回 Model，业务在 `Do`/`DoBefore` 里实现 |

前端对按钮的分流（`WayPage`，`name` 是 `command` 小写）：

1. `command === 'add'` **或** `editshow===true`：打开表单。`add` 底稿为空；`editshow && isselectrow` 底稿为当前选中行。提交后把**表单值**当 body。
2. `command === 'edit'`：打开表单，底稿为当前选中行，提交表单值。
3. 其余（`remove`/`submit`/`release`/普通自定义）：不打开表单，body 为当前选中行（`isselectrow`）或选中 id 列表（`selectmultiple`）。
4. `viewtype === 'form'`：没有列表弹窗，`add`/`editshow` 直接提交整页表单。
5. `isalert===true`：执行前确认。API 调用方不必模拟对话框，但必须满足选中行/id 约束。
6. 成功后若不是 `viewtype=form`，前端会再发一次**主查询**刷新列表。

表单里的子表随 add/edit 一起提交：子行在对应切片字段（`childmodels[].name` / `propertyname`）里，并用 `modelState` 区分：`1` 新增、`2` 修改、`3` 删除（已删行仍留在数组里带 state=3）。主查询展开子表是 Search 模式 3，不要和这个写路径混用。

自定义命令：值嵌入 `manage.Operation[T]`（不要指针嵌入），放进 `Routers()`。`command` 仍是结构体名小写。需要表单的，在 `ViewCommandModel` 里设 `editshow=true`；只对当前行执行的，保持默认 `isselectrow=true`。

按钮多时可用命令分割：`issplit=true` 且 `splitname` 等于某条主命令的 `command`（小写，例如 `add`），前端把该按钮藏进宿主按钮的下拉，宿主本身仍可点。找不到宿主时仍作为主按钮显示。示例见 `examples/08-admin-manage-ui` 把导入、导出、复制挂到 `add`。

#### 调用时不要做的事

- 执行 `view.commands` 里没有的 command（未注册就是 404）。
- body 写成 `{ "model": { "name": "x" } }` 或把 `userid` 放进 Manage 命令体。
- `edit`/`remove`/`submit`/`release` 不带 `id`，或 `add` 自己编一个会冲突的 id。
- 把 Search 的 `SearchItem` 当命令 body。
- 用 OpenAPI / 猜路径替代 `view.commands[].command`。
- 只读页面对未注册的 `add` 发请求。
- 改子表却不走 add/edit 表单、也不设 `modelState`。

### 获取测试 Token（开发 / 调试用）

框架内置 `TestToken` 接口，**无需登录即可获取 JWT**，专为开发调试设计。它是 **ServerManage 路由**（`api.ServerRouterInfoWithOptions` 把路径解析为 `/api/servermanage/{structNameLower}`），虽然 Go 类型放在 `public` 包下，但 `public` 不会进入 URL：

```
GET /api/servermanage/testtoken?userid={userId}&type={tokenType}
```

| 参数 | 说明 |
|------|------|
| `userid` | 任意用户 ID 字符串（必填） |
| `type` | `0` = 普通用户 token（用于 Private API）<br>`1` = 管理员 token（用于 Manage API）<br>`2` = 服务管理 token（用于 ServerManage API） |
| `service` | 可选。多服务进程中指定目标服务名，如 `&service=shop-supplier` |

**示例（curl）：**
```bash
# 获取普通用户 token（用于调用 Private 接口）
curl "http://localhost:18080/api/servermanage/testtoken?userid=user001&type=0"

# 获取管理员 token（用于调用 Manage 接口）
curl "http://localhost:18080/api/servermanage/testtoken?userid=admin001&type=1"
```

**返回值**：`data` 是 `safe.TokenPairResponse` 对象，**不是** token 字符串。取值用 `data.access_token`：
```json
{
  "success": true,
  "data": {
    "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "refresh_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "token_type": "Bearer",
    "access_expires_in": 3600,
    "refresh_expires_in": 604800
  },
  "code": 200
}
```

**使用 token 调用 Private API**（注意 URL 里没有 `private` 段，身份也不通过请求体传递）：
```bash
TOKEN=$(curl -s "http://localhost:18080/api/servermanage/testtoken?userid=user001&type=0" | jq -r .data.access_token)

curl -X POST "http://localhost:18080/api/demo/addorder" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"amount":"100.00","tokenid":"1"}'
```

请求体不带 `userid`：Private 路由的身份只能由 `req.GetUser()` 从 Token 解析，接受客户端自报的 UserID 属于越权漏洞。

**在 Go API handler 中读取 token 中的用户信息：**
```go
func (own *AddOrder) Do(req types.IRequest) (interface{}, error) {
    uid, uname := req.GetUser()  // 从 JWT token 中提取 uid 和 uname
    _ = uname
    // uid 即 testtoken?userid= 传入的值
    own.UserID = uid
    // ...
}
```

**⚠️ 注意：** `TestToken` 无需登录，但框架并非完全不设防：`api.ServerArgs.Validation` 会拒绝来自非本地 IP 的未授权请求（`utils.HasLocalIPAddr`），除非显式开启 `ServerOption.RemoteAccessManageAPI`。因此不要靠开启该选项把 TestToken 暴露到公网；生产应通过网关/防火墙屏蔽 `/api/servermanage/*`，并在正式上线前改用真实的 Casdoor 认证流程。

### 直接调用 API（无需 WayPage）

任何 HTTP 客户端均可调用，以下示例使用 `fetch` / `axios`：

```typescript
// Public API（无需 token）
const res = await fetch('/api/demo/public/getorder', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ id: '12345' }),
});
const result = await res.json();  // ResultData

// Private API（需要在 Authorization 头中传 Bearer token）
const token = '/* 从 testtoken 接口获取 */';
const res = await fetch('/api/demo/private/addorder', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${token}`,
  },
  body: JSON.stringify({ userid: 'user001', amount: '100.00', tokenid: '1' }),
});

// Manage Search（主模型查询；外键/子表见上文三种模式）
const res = await fetch('/api/manage/demo/ordermanage/search', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${manageToken}`,   // type=1 的 token
  },
  body: JSON.stringify({ page: 1, size: 10, whereList: [], sortList: [] }),
});
```

### View / Command 字段速查

完整行为见上文「Manage View」与「Manage 命令执行」。JSON 名以 Go tag 为准（`isdate`、冻结拼写 `porpfield`）。前端 UI 层曾称 `ViewModel` 为 `ModelAttribute`。

- 提交数据用 `fields[].field`（json 名），查询条件 `name` 用 `field` 或 `porpfield`。
- 执行命令用 `commands[].command` 当 URL 最后一段；`name` 是稳定键，`title` 是展示文案。
- `commands` 不含 `view`/`search`；未出现在数组里的 command 不要调用。
