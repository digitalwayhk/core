# 最简商城完整示例设计

## 1. 文档状态

- 状态：设计已确认，等待书面规格复审
- 日期：2026-07-14
- 范围：重建 `examples`，只保留一个可运行、可集成测试的完整示例
- 非目标：不修改框架核心行为，不引入 Redis、Docker、外部 MQ 或前端项目

## 2. 背景与目标

当前 `examples` 由多个局部示例组成，存在模式重复、旧接口残留和单个示例无法展示完整业务闭环的问题。新示例以一个最小商城为业务载体，同时演示 Digitalway Core 的核心使用方式：

1. 使用 `ServiceContext` 和 `IRouter` 组装服务。
2. 使用 `entity.Model`、`ModelList` 和 SQLite 持久化数据。
3. 使用 Manage Router 实现标准管理能力。
4. 使用 public/private Router 区分公开接口和用户接口。
5. 使用框架内建 TestToken 获取普通用户和管理员令牌。
6. 使用 `/ws`、`logon` 和路由订阅向最终用户推送订单变更。
7. 使用真实 HTTP、WebSocket 和 SQLite 集成测试验证全部功能。

示例优先追求完整、准确、易读，不承担性能压测、多节点部署或复杂领域建模职责。

## 3. 方案选择

### 3.1 已选方案：单一完整示例

删除现有 `examples` 下的分散示例，建立 `01-simple-shop` 和统一的 `integration` 测试目录。一个示例覆盖模型、管理接口、公开接口、私有接口、鉴权、WebSocket 和持久化全链路。

选择原因：

- 初次使用者可以沿一条业务路径理解框架，不需要在多个示例间拼接知识。
- 集成测试可以验证公开契约，降低示例随框架演进失效的风险。
- 目录规模仍然足够小，适合作为第一个教学示例。

### 3.2 未选方案

- 保留多个能力碎片示例：便于单点查阅，但继续存在重复配置和组合方式不明确的问题。
- 将所有代码放入单个文件：启动快，但无法展示实际项目的模型、路由和管理边界。

## 4. 目录结构

```text
examples/
├── README.md
├── 01-simple-shop/
│   ├── README.md
│   ├── service.go
│   ├── main/
│   │   ├── main.go
│   │   └── etc/
│   │       ├── server.json
│   │       └── shop.json
│   ├── models/
│   │   ├── product.go
│   │   └── order.go
│   └── api/
│       ├── manage/
│       │   ├── productmanage.go
│       │   └── ordermanage.go
│       ├── public/
│       │   └── getproducts.go
│       └── private/
│           ├── addorder.go
│           ├── getorders.go
│           └── deleteorder.go
└── integration/
    ├── helpers_test.go
    ├── shop_http_test.go
    ├── shop_manage_test.go
    └── shop_websocket_test.go
```

`examples/README.md` 只负责指向完整示例、说明运行命令和测试命令。业务说明、接口清单、请求样例和 WebSocket 协议放在 `01-simple-shop/README.md`。

## 5. 服务与配置

服务名固定为 `shop`。`main` 使用 `run.NewWebServer()`，注册 `ShopService` 后启动。框架自带的 `servermanage` 服务继续由 WebServer 自动注册。

示例配置满足以下约束：

- 使用固定的本地演示端口，`server` 与 `shop` 不冲突。
- SQLite 数据存入示例运行目录；集成测试覆盖为 `t.TempDir()`。
- Cluster 使用本地模式。
- 不配置 Redis、外部 MQ 或外部服务发现。
- CORS 默认关闭；示例不依赖浏览器前端。
- `TrustedProxies` 默认为空。
- Auth、ManageAuth 和 ServerManageAuth 使用不同的演示密钥。

集成测试动态创建隔离配置、分配可用端口并恢复涉及的进程级测试配置。由于配置目录和 SQLite 测试路径包含进程级兼容变量，这组集成测试不使用 `t.Parallel()`。

## 6. 数据模型

### 6.1 Product

`Product` 嵌入已初始化的 `*entity.Model`，不使用需要稳定 Code 的 `BaseModel`。

| 字段 | 类型 | 约束 |
| --- | --- | --- |
| ID | 框架模型主键 | 自动生成 |
| Name | string | 必填，去除首尾空白后不能为空 |
| Price | decimal.Decimal | 必须大于 0 |

商品名称不要求全局唯一。本示例不实现库存、分类、上下架和软删除。

### 6.2 Order

`Order` 同样嵌入已初始化的 `*entity.Model`。

| 字段 | 类型 | 约束 |
| --- | --- | --- |
| ID | 框架模型主键 | 自动生成 |
| ProductID | 主键对应整数类型 | 下单时商品必须存在 |
| ProductName | string | 保存下单时名称快照 |
| UnitPrice | decimal.Decimal | 保存下单时价格快照 |
| Quantity | int | 必须大于 0 |
| UserID | string | 只从认证上下文获取，不能为空 |

订单总价不重复入库，由 `UnitPrice * Quantity` 计算。商品后续改名或改价不会改变历史订单。

两个模型都通过 `entity.NewModelList[T](nil)` 操作，不增加 Repository 层。

## 7. 路由与权限

### 7.1 管理接口

`ProductManage` 使用 `manage.NewManageService[Product](owner)` 暴露完整管理能力：

- View
- Search
- Add
- Edit
- Remove

`OrderManage` 同样复用 ManageService，但只返回 View 和 Search Router。订单管理端没有 Add、Edit、Remove 路由；订单写操作只能经过用户私有接口，以保留所有权校验和通知语义。

管理员令牌通过以下框架内建接口获取：

```text
GET /api/servermanage/testtoken?userid=admin&type=1
```

### 7.2 公开商品查询

```text
GET /api/shop/getproducts
```

可选查询参数：

- `id`：商品 ID 精确匹配。
- `name`：商品名称模糊匹配。
- 两者都为空：返回全部商品。
- 两者同时存在：按 AND 组合筛选。

响应项只公开 `id`、`name`、`price`。没有匹配商品时返回空数组，不返回错误。

### 7.3 用户私有接口

普通用户令牌通过当前 `shop` 服务的内建 TestToken 路由获取：

```text
GET /api/servermanage/testtoken?userid=user-a
```

HTTP 请求使用 `Authorization: Bearer <token>`。私有接口只通过 `req.GetUser()` 读取用户身份，忽略并禁止请求体提供 UserID。

#### 新增订单

```text
POST /api/shop/addorder
```

请求只包含 `productID` 和 `quantity`。执行顺序为：

1. 校验登录用户和数量。
2. 查询商品；不存在则拒绝。
3. 从商品复制 ID、名称和单价快照。
4. 保存订单。
5. 数据库提交成功后发布 `created` WebSocket 事件。

#### 查询本人订单

```text
GET /api/shop/getorders
```

只返回当前用户的全部订单，不接受 UserID 参数。响应按稳定顺序排列，便于客户端和集成测试比较。

#### 删除本人订单

```text
POST /api/shop/deleteorder
```

请求只包含订单 ID。查询和删除条件必须同时包含订单 ID 与当前 UserID，采用物理删除。订单不存在或属于其他用户时统一返回“订单不存在或无权操作”，避免泄露订单是否存在。数据库删除成功后发布 `deleted` WebSocket 事件，事件携带删除前的订单快照。

## 8. WebSocket 设计

WebSocket 只用于最终外部用户订阅，不用于内部服务通信。服务间调用继续使用 TransportSelector；内部事件和跨节点控制由当前 ServiceContext 的 EventBridge、RouteWebSocketHub 和已配置 MQ 路径负责。

### 8.1 登录与订阅

客户端连接：

```text
ws://127.0.0.1:{shopPort}/ws
```

连接后通过现行协议登录：

```json
{
  "event": "sub",
  "channel": "logon",
  "data": {"token": "<普通用户 TestToken>"}
}
```

收到 `event=success`、`channel=logon` 后订阅订单路由：

```json
{
  "event": "sub",
  "channel": "/api/shop/getorders",
  "data": {}
}
```

旧示例中的 `/wsauth` 不属于当前契约，不再记录或测试。

### 8.2 用户隔离

`GetOrders` 实现框架识别的用户注入能力，并使用 UserID 生成稳定订阅哈希。它还实现 `IWebSocketRouterNotice`，发送前再次比较事件 UserID 与订阅 Router 的 UserID。

因此用户隔离同时存在于两个层次：

1. 不同 UserID 进入不同订阅 hash。
2. 通知过滤器拒绝 UserID 不匹配的消息。

不得广播订单事件，也不得读取客户端提交的 UserID 建立订阅。

### 8.3 事件结构

内部通知结构：

```json
{
  "action": "created",
  "order": {
    "id": 1,
    "productID": 10,
    "productName": "示例商品",
    "unitPrice": "19.90",
    "quantity": 2,
    "userID": "user-a"
  }
}
```

`action` 只允许 `created` 或 `deleted`。UserID 用于服务端过滤；对客户端的最终消息可以保留 UserID，以便示例清楚展示身份隔离，但不得包含 token、claims 或其他认证数据。

只有数据库写操作成功后才发布事件。没有订阅者时观察型通知按 EventBridge 既定语义直接丢弃，不影响 HTTP 写操作成功。

## 9. 错误与日志契约

示例使用稳定中文业务错误：

- `商品不存在`
- `商品名称不能为空`
- `商品价格必须大于 0`
- `订单数量必须大于 0`
- `订单不存在或无权操作`
- `用户身份无效`

未知数据库错误交给框架公开错误契约脱敏，不能将 SQL、路径或底层错误直接返回客户端。

日志只记录稳定事件名及必要字段，例如路由、操作和错误分类；不记录 token、完整请求体、响应体、用户 claims 或模型对象转储。预期业务校验失败不重复打印高等级异常日志。

## 10. 中文注释要求

示例中的以下代码必须有准确的中文注释：

- 每个公开类型及其业务职责。
- 每个自定义函数和方法，包括 `Parse`、`Validation`、`Do`、`RouterInfo`、WebSocket 过滤和用户注入方法。
- 服务注册和启动函数。
- 集成测试辅助函数及每个测试用例。

注释解释业务约束或框架扩展点，不写“给变量赋值”一类无信息注释。接口实现的注释应说明为什么需要该接口以及框架何时调用。

## 11. 集成测试设计

集成测试启动真实 WebServer，并使用真实 HTTP、真实 WebSocket 和独立 SQLite 临时数据库。测试不得绕过 RouterInfo 直接调用 `Do` 来替代端到端验收。

### 11.1 测试生命周期

测试辅助层负责：

1. 创建 `t.TempDir()`。
2. 生成隔离的 `server.json` 和 `shop.json`，配置独立端口和固定测试密钥。
3. 将 SQLite 路径指向临时目录。
4. 启动 WebServer 并等待健康可用，不用固定 `time.Sleep` 猜测启动时间。
5. 通过 `/api/servermanage/testtoken` 获取管理员及两个普通用户令牌。
6. 测试结束后关闭 WebServer、WebSocket 和数据库资源。
7. 恢复测试修改的进程级配置，断言无残留文件和后台 worker。

测试串行运行，不使用外部 Docker 服务。

### 11.2 管理接口验收

- 管理员可以新增商品。
- 管理员可以查询、编辑和删除商品。
- 商品名称与价格校验生效。
- 订单管理端可以 View 和 Search。
- 订单管理端不存在 Add、Edit、Remove 路由。
- 普通用户令牌不能调用 Manage Router。

### 11.3 HTTP 业务验收

- 无筛选条件返回全部商品。
- `id` 精确筛选正确。
- `name` 模糊筛选正确。
- `id + name` 使用 AND 语义。
- 用户可以对存在商品下单。
- 订单保存商品名称和价格快照；后续修改商品不改变历史订单。
- 商品不存在、数量为零或负数时拒绝下单。
- 用户只能查询自己的订单。
- 用户只能物理删除自己的订单。
- 越权删除和不存在订单返回相同公开错误。
- 删除后本人订单查询不再返回该订单。

### 11.4 WebSocket 验收

- 未登录连接不能订阅 private 订单路由。
- 使用 TestToken 通过 `logon` 登录成功。
- 用户 A 和用户 B 分别建立连接并订阅订单路由。
- 用户 A 新增订单后，只向 A 推送 `created` 事件。
- 用户 A 删除订单后，只向 A 推送 `deleted` 事件。
- 推送订单内容与数据库提交后的快照一致。
- 用户 B 在有限读超时内收不到用户 A 的事件。
- 连接关闭后订阅清理，服务停止后 WebSocket worker 收口。

### 11.5 验证命令

实施完成后至少运行：

```bash
find examples -name '*.go' -print0 | xargs -0 gofmt -w
go test ./examples/... -count=1
go test ./examples/integration -count=10
go test -race ./examples/integration -count=1
./scripts/check-logging.sh
./scripts/ci.sh required/quick
./scripts/ci.sh required/contracts
```

若集成测试所需的框架生命周期缺陷使测试无法可靠关闭，应先以最小范围修复核心缺陷并增加对应核心包回归测试，不允许通过强制退出、忽略错误或任意 sleep 掩盖。

## 12. 删除与兼容边界

实施时删除当前 `examples` 下除新设计文件外的所有旧示例，再创建本设计目录。由于 `examples` 是教学和验证代码，不属于承诺稳定的 Go 公共 API，但删除可能影响直接导入旧示例包的外部使用者，因此需要：

- 在变更说明中明确列出旧示例已被单一完整示例替代。
- 全仓搜索并更新指向旧示例的 README、脚本和测试引用。
- 不从已删除的旧示例复制失效的 `/wsauth`、自定义登录、全局 WebSocket 单例或请求身份字段模式。
- 不在本任务中修改框架公共 API、路由生成规则或错误码契约。

## 13. 完成定义

同时满足以下条件才算完成：

- `examples` 只包含一个完整商城示例和统一集成测试目录。
- 示例可以按 README 独立启动。
- 商品和订单两个模型通过 SQLite 真实持久化。
- Manage、public、private 和 WebSocket 功能均有端到端测试。
- TestToken 是测试和文档中唯一的示例令牌来源。
- 用户身份只来自认证上下文，订单查询、删除和推送均按 UserID 隔离。
- 所有示例方法及测试辅助方法具有准确中文注释。
- 定向测试、重复测试、race、日志检查和必要 CI 门禁通过。
- 没有引入 Docker、Redis、外部 MQ 或额外 Repository 抽象。
