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
| **Private 安全要求** | Private 路由自动标注 Bearer 安全要求 |

> ⚠️ **Manage 路由不包含在 OpenAPI 文档中**，仅 Public + Private 路由会被导出。

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
所有接口均为 `POST`（除 TestToken 为 `GET`），响应统一为 `ResultData` 结构。

### 统一响应格式

所有后端接口（Public / Private / Manage）均返回同一结构：

```typescript
interface ResultData {
  success: boolean;              // true = 成功
  code: number;                  // 业务状态码
  message: string;               // 错误描述（success=false 时有效）
  data: TableData | object;      // 业务数据
  showtype: number;              // 前端展示方式（0=静默 1=warn 2=error 3=notification）
  traceid: string;
  host: string;
}

interface TableData {
  rows: any[];    // 数据行（Search 结果）
  total: number;  // 总记录数（用于分页）
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
```

> ⚠️ Public 和 Private API 的 URL 格式相同；区别在于 Private 需要携带 Bearer token，框架通过路由的 PathType 来验证。

`request.ts` 封装了三个函数（`c` 是 controller path，`m` 是命令名）：

```typescript
import { init, search, execute } from '@/components/WayPlus/request';

// 1. init — 获取 Manage schema（字段、命令按钮、子模型）
//    POST /api/{c}/view
const schema = await init({ c: 'manage/demo/ordermanage', s: 'demo' });

// 2. search — 分页查询
//    POST /api/{c}/search，body = SearchItem
const result = await search({ c: 'manage/demo/ordermanage', s: 'demo', item: searchItem });

// 3. execute — 执行命令（add / edit / remove / submit / release / 自定义）
//    POST /api/{c}/{m}，body = 表单数据
const result = await execute({ c: 'manage/demo/ordermanage', m: 'add', s: 'demo', item: formData });
```

### SearchItem 参数结构

```typescript
interface SearchItem {
  page: number;          // 页码，从 1 开始
  size: number;          // 每页条数，默认 10
  whereList?: SearchWhere[];
  sortList?: string[];
}

interface SearchWhere {
  name: string;    // 字段名（Go 属性名，与后端 SearchItem.AddWhereN 的字段名对应）
  symbol: string;  // 操作符：= / like / in / between / isnull / > / >= / < / <= / !=
  value: string;   // 查询值（数字也传字符串，后端自动转换）
}

// 示例
const item: SearchItem = {
  page: 1,
  size: 20,
  whereList: [
    { name: 'Name',  symbol: 'like', value: 'iPhone' },
    { name: 'Price', symbol: '>',    value: '1000'   },
  ],
  sortList: [],
};
```

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

// Manage Search
const res = await fetch('/api/manage/demo/ordermanage/search', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${manageToken}`,   // type=1 的 token
  },
  body: JSON.stringify({ page: 1, size: 10, whereList: [], sortList: [] }),
});
```

### ModelAttribute schema 类型说明

后端 `View` 接口（`POST /api/manage/{s}/{c}/view`）返回 `ModelAttribute`，
描述所有字段和命令按钮的元信息，用于动态渲染 UI：

```typescript
interface ModelAttribute {
  name?: string;           // 控制器名称
  title?: string;          // 页面标题
  servicename?: string;    // 服务名
  autoload?: boolean;      // true = 进入页面自动查询
  viewtype?: string;       // 'form' = 表单视图（单条记录）；默认表格+列表视图
  fields?: WayFieldAttribute[];
  commands?: CommandAttribute[];
  childmodels?: ChildModelAttribute[];
}

interface WayFieldAttribute {
  field: string;           // JSON 字段名（提交数据时用）
  porpfield?: string;      // Go 属性名（SearchWhere.name 使用此值）
  title?: string;          // 列标题 / 表单标签
  type?: string;           // string / int / int64 / decimal / bool / date / datetime
  visible?: boolean;       // 是否在表格中显示列
  isedit?: boolean;        // 是否在表单中可编辑
  issearch?: boolean;      // 是否出现在搜索栏
  required?: boolean;      // 表单必填
  iskey?: boolean;         // 是否为主键（id 字段）
  comvtp?: ComboxAttribute;   // 下拉枚举（isvtp=true 启用）
  foreign?: ForeignAttribute; // 外键关联
}

interface CommandAttribute {
  command: string;          // 命令名 → 对应 execute() 的 m 参数
  name: string;             // 显示名称
  isselectrow?: boolean;    // true = 需先选中一行
  selectmultiple?: boolean; // true = 支持多选
  isalert?: boolean;        // true = 执行前弹确认框
  editshow?: boolean;       // true = 在编辑表单内显示（不在工具栏）
  issplit?: boolean;        // true = 按钮放入下拉分组
  splitname?: string;       // 分组父按钮名称
}
```

