# pkg/server 目录说明

`pkg/server` 是 core 的服务提供层，负责把业务服务注册成可调用 API，并统一处理路由、鉴权、REST/WebSocket、内部服务调用、服务发现、配置、安全、OpenAPI 和运行状态统计。

本说明不分析 `pkg/server/run/dist*` 和 `pkg/server/run/swagger` 静态资源，它们是前端构建产物和 Swagger UI 静态文件。

## 核心路由接口

所有业务 API 都实现 `types.IRouter`：

```go
type IRouter interface {
    Parse(req IRequest) error
    Validation(req IRequest) error
    Do(req IRequest) (interface{}, error)
    RouterInfo() *RouterInfo
}
```

执行顺序由 `RouterInfo.Exec` 统一控制：

1. 创建新的路由实例。
2. `Parse(req)` 解析参数。
3. `Validation(req)` 校验业务条件。
4. 命中缓存时直接返回。
5. 触发请求订阅通知。
6. `Do(req)` 执行业务逻辑。
7. 包装标准响应。
8. 触发成功或错误订阅通知。
9. 记录路由统计。

业务路由应只关注参数、校验和业务逻辑，不需要自己处理 HTTP 响应格式。

### IRouterResponse

路由可以额外实现 `types.IRouterResponse`：

```go
type IRouterResponse interface {
    GetResponse() interface{}
}
```

这个接口主要服务于 OpenAPI 自动文档。`pkg/server/api/public/openapi.go` 和 `pkg/server/run/openapi.go` 在生成接口响应 schema 时，会先查找 `router.TestResult[info.Path]`；如果没有测试执行结果，就判断路由实例是否实现了 `IRouterResponse`，并调用 `GetResponse()` 取得示例响应数据。

适用场景：

- `Do()` 返回结构比较复杂，OpenAPI 仅靠空路由实例无法推断响应结构。
- 希望 Swagger/OpenAPI 页面展示稳定的响应示例。
- 不希望为了生成文档而真实执行 `Do()`。

示例：

```go
func (own *GetOrder) GetResponse() interface{} {
    return []*models.OrderModel{}
}
```

这样 OpenAPI 会把标准响应包装里的 `data` 示例推断为订单数组。

## 路由自动生成

`router.DefaultRouterInfo(api)` 会根据包路径和结构体名自动推导 API 元数据。

包路径规则：

- `api/public`：公开接口。
- `api/private`：需要用户鉴权。
- `api/manage`：管理后台接口。

路径规则：

```text
/api/{serviceName}/{routerStructNameLower}
```

例如 `examples/demo/api/private/AddOrder` 会生成：

```text
/api/demo/addorder
```

`service/manage` 生成的管理路由会使用特殊路径：

```text
/api/manage/{serviceName}/{modelManageName}/{command}
```

例如：

```text
/api/manage/demo/tokenmanage/view
/api/manage/demo/tokenmanage/search
/api/manage/demo/tokenmanage/add
```

## 服务注册

业务服务实现 `types.IService`：

```go
type IService interface {
    ServiceName() string
    Routers() []IRouter
    SubscribeRouters() []*ObserveArgs
}
```

启动时通过：

```go
server := run.NewWebServer()
server.AddIService(&demo.DemoService{}, &types.ServerOption{
    IsCors: true,
    IsWebSocket: true,
})
server.Start()
```

`run.NewWebServer()` 会自动加入 `SystemManage` 服务。`SystemManage` 提供服务管理、配置、菜单、路由查询、日志、OpenAPI、健康检查、测试 token 等内置 API。

## ServiceContext

`router.ServiceContext` 是服务运行上下文，负责：

- 读取或创建服务配置。
- 生成服务 Snowflake ID。
- 初始化服务路由表。
- 维护服务依赖。
- 注册 observe 订阅。
- 调用本地或远程服务。
- 保存 HTTP server、socket server、Hub 等运行对象。
- 提供路由统计查询。

`Request.NewID()` 最终会调用 `ServiceContext.NewID()`，所以业务新增数据时可以通过请求生成全局 ID。

## 配置处理

`pkg/server/config` 当前通过 `ServerConfig` 表达服务配置，并由 `ReadConfig(servicename)` 读取 `etc/{service}.json`。配置文件不存在时，`NewServiceDefaultConfig(servicename, port)` 会创建默认配置并保存。

新增配置应继续使用 `ServerConfig` 的结构化字段，不应再放入 `CustomerDataList`。推荐处理方式：

1. 在 `ServerConfig` 增加结构化配置字段，例如 `Cluster`、`Transport`、`MQ`。
2. 在 `pkg/server/config` 中拆出对应配置文件，例如 `clusterconfig.go`、`transportconfig.go`、`mqconfig.go`。
3. `ReadConfig`、`NewServiceDefaultConfig`、`Save`、`ModifyConfig` 都应调用统一的 `ApplyDefaults()` 和 `Validate()`。
4. 旧配置缺少新字段时必须自动补默认值，避免旧 `etc/{service}.json` 无法启动。
5. `CustomerDataList` 只作为历史兼容读取入口，不再承载新规范配置。

新增水平扩展、内部传输和 MQ 的完整配置建议见仓库根目录 `OPTIMIZATION_PLAN.md`。

## 测试规划

server 新能力应同时规划单元测试和集成测试：

- 单元测试跟随被测包放置，例如 `pkg/server/config/*_test.go`、`pkg/server/cluster/*_test.go`、`pkg/server/transport/*_test.go`。
- 集成测试集中放在 `tests/integration/`，并使用 `integration` build tag，避免普通 `go test ./...` 依赖 Redis、NATS、etcd、Consul 等外部服务。
- 旧配置兼容、默认值、Validate、MachineID claim、服务分片、transport selector、MQ switcher、WebSocket `routePath + hash` 摘要和 `NoticeFiltersRouter` 本地过滤都应有测试覆盖。

## REST 服务

`trans/rest.Server` 基于 go-zero `rest.Server` 注册路由。

注册时会：

- 遍历 `ServiceRouter.GetRouters()`。
- 按 `RouterInfo.Method` 和 `RouterInfo.Path` 注册 HTTP route。
- 对 private/manage 路由启用 JWT、Casdoor 或 Logto 鉴权。
- 执行 IP 白名单校验。
- 创建 `router.Request`。
- 调用 `RouterInfo.Exec(req)`。
- 统一输出标准响应。

默认 HTTP 方法是 `POST`，路由可以在 `RouterInfo()` 中覆盖，例如：

```go
info := router.DefaultRouterInfo(own)
info.Method = "GET"
return info
```

## WebSocket

当 `ServerOption.IsWebSocket` 为 true 时，REST server 会注册 `/ws`。

当前 WebSocket 实现使用 `trans/websocket/melody`，包含：

- 连接数限制。
- 连接频率限制。
- IP 白名单校验。
- session 订阅管理。
- 路由通知分发。

业务代码通常不直接操作 WebSocket，而是通过 `RouterInfo.NoticeWebSocket(data)` 或 observe 回调间接推送。

### WebSocket 订阅协议

客户端通过 `/ws` 建立连接后，按路由 path 订阅，例如：

```json
{"channel":"/api/demo/getorder","event":"sub","data":{"page":1,"size":10}}
```

取消订阅：

```json
{"channel":"/api/demo/getorder","event":"unsub","data":{"page":1,"size":10}}
```

`melody.SessionSubscriptions` 会根据 `channel` 查找 `RouterInfo`，用 `data` 创建路由实例，并调用 `RouterInfo.RegisterWebSocketClient(api, client, req)`。订阅会按路由参数 hash 分组，同一接口不同参数可以对应不同订阅组。

### IWebSocketRouter

路由可以实现：

```go
type IWebSocketRouter interface {
    RegisterWebSocket(client IWebSocket, req IRequest)
    UnRegisterWebSocket(client IWebSocket, req IRequest)
}
```

当某个订阅 hash 第一次出现时，框架会在注册后回调 `RegisterWebSocket`；当该 hash 下最后一个客户端退订或断开时，框架会回调 `UnRegisterWebSocket`。它适合做订阅生命周期管理，例如启动/停止某个后台数据源、记录订阅人数、按用户创建外部行情订阅等。

注意：回调应保持轻量，并自行处理错误；框架会捕获 panic，但不要在这里做长时间阻塞操作。

### IWebSocketRouterNotice

路由可以实现：

```go
type IWebSocketRouterNotice interface {
    NoticeFiltersRouter(message interface{}, api IRouter) (bool, interface{})
}
```

当业务调用 `RouterInfo.NoticeWebSocket(message)` 时，框架会遍历已订阅的 hash 组，并把当前通知消息和该组对应的订阅路由参数 `api` 传给 `NoticeFiltersRouter`。

返回值含义：

- `ok == true`：向该订阅组推送。
- `ok == false`：跳过该订阅组。
- 第二个返回值：实际推送给客户端的数据，可对原始 `message` 做裁剪、转换或补充。

典型用途：

- 按用户、币种、订单 ID、分页条件等过滤推送。
- 将后端事件转换为前端更适合消费的数据。
- 避免把所有通知广播给所有订阅者。

当前全局 WebSocket 通知系统会用 worker 池异步执行过滤，并带超时保护；因此过滤函数应尽量只做内存判断，不要发起慢查询。

在横向扩展场景下，目标节点收到跨节点 notice 后仍必须调用 `NoticeFiltersRouter(message, api)`。跨节点同步只能说明某个节点可能存在 `routePath + hash` 订阅，最终是否推送仍由本接口根据本地订阅参数 `api` 决定。

### IRouterHashKey

路由可以实现：

```go
type IRouterHashKey interface {
    GetHashKey() uint64
}
```

默认情况下，框架会遍历路由字段值并计算 hash，作为订阅分组、缓存键和退订识别依据。实现 `IRouterHashKey` 后，框架改用业务自定义 hash。

适用场景：

- 路由结构里有不适合参与 hash 的临时字段。
- 希望多个不同请求参数共享同一个订阅组。
- 希望 hash 只由关键业务字段决定，例如 `UserID + Symbol`。
- 需要让路由缓存和 WebSocket 分组使用同一套业务 key。

`IRouterHashKey` 同时会影响 `RouterInfo` 的路由结果缓存键，所以实现时要保证同一业务语义生成稳定 hash，不同业务语义不要碰撞。

在横向扩展场景下，`IRouterHashKey` 还用于生成订阅摘要：`routePath + hash -> local clients`。节点只向 event-stream 发布 `serviceName + routePath + hash + nodeID`，不暴露具体 client。其他节点根据摘要判断 notice 是否需要跨节点转发，再由目标节点本地的 `IWebSocketRouterNotice` 做最终过滤。

高频推送路由应优先实现稳定的 `IRouterHashKey`，否则只能退化为 `routePath` 级别广播，跨节点推送成本会明显上升。

## Observe 订阅

服务可以在 `SubscribeRouters()` 返回 `types.ObserveArgs`，订阅另一个路由的请求、响应或错误事件。

典型用途：

- `AddOrder` 成功后通知订阅 `GetOrder` 的 WebSocket 客户端。
- 跨服务事件通知。
- 业务操作后的异步联动。

`types.NewObserveArgs(router, types.ObserveResponse, callback)` 是最常用形式。

## 内部服务调用

服务间调用入口包括：

- `req.CallService(router)`
- `req.CallTargetService(router, targetInfo)`
- `ServiceContext.CallServiceUseApi(api)`
- `ServiceContext.CallService(payload)`

调用会被包装成 `types.PayLoad`，携带 trace、源服务、目标服务、目标路径、用户、客户端 IP、HTTP 方法和实例参数。

## 安全能力

`safe` 目录包含：

- JWT。
- IP 黑白名单。
- Casdoor 鉴权。
- Logto 鉴权。
- 二次验证。
- 网关验证。

REST 注册路由时会根据服务配置决定使用哪种认证方式。

## 运行参数

`run.WebServer` 支持命令行参数：

- `-p`：HTTP 端口，默认 8080。
- `-socket`：内部 socket 服务端口，默认为 0，不启用。
- `-view`：视图服务端口，默认 80。
- `-server`：父服务器地址。

## OpenAPI 自动文档

`pkg/server/api/public/openapi.go` 提供公开 OpenAPI 文档接口，`pkg/server/run/openapi.go` 提供同类生成能力和 Swagger 静态资源挂载辅助。

OpenAPI 生成逻辑会：

- 遍历当前服务路由。
- 跳过 `server` 系统服务自身。
- 只生成 `public` 和 `private` 路由文档，不生成 `manage` 管理端路由。
- 根据 `RouterInfo.Method` 生成 GET query 参数或 POST request body。
- 读取字段 `json` 和 `desc` tag，生成参数说明。
- 为 private 路由添加 Bearer JWT 安全声明。
- 生成标准响应包装结构。
- 当路由实现 `IRouterResponse` 时，用 `GetResponse()` 作为响应示例数据。

因此 OpenAPI 的定位是：自动生成并测试非管理端 API，也就是业务公开接口和私有接口。管理后台接口由 `service/manage` 和 `web/admin/WayPlus` 配套使用，不在 OpenAPI 中展开。

测试 private API 时，文档中的 Bearer 说明会指向测试 token 获取方式：

```text
/api/servermanage/testtoken?userid=12345
```

常见访问入口：

```text
/api/{service}/openapi
/swagger/
```

具体挂载方式取决于运行服务如何注册 `OpenAPIHandler` 和 Swagger 静态文件。

## 主要目录

- `types`：核心接口、路由元数据、响应、服务、payload、observe、WebSocket 抽象、统计结构。
- `router`：服务上下文、路由表、请求/响应、服务调用和统计管理。
- `run`：WebServer 启动器、HTML 服务、OpenAPI 支持。
- `trans/rest`：HTTP 服务和客户端。
- `trans/websocket`：WebSocket 客户端和 melody 实现。
- `trans/socket`：内部 socket 通信。
- `trans/quic`：QUIC 通信实验/实现。
- `safe`：鉴权和安全控制。
- `api`：系统内置管理、公开、私有 API。
- `config`：服务配置、Casdoor、Melody 配置；未来承载 Cluster、Transport、MQ 的结构化配置和默认值校验。
- `smodels`：系统管理模型，如菜单、目录、权限、IP 白名单。

## 开发建议

- 新业务接口放在业务服务自己的 `api/public`、`api/private`、`api/manage` 包下。
- 优先使用 `router.DefaultRouterInfo(own)`。
- 不要手写路径，除非是在扩展底层路由机制。
- 鉴权需求通过包路径和服务配置表达。
- 业务间调用优先用 `req.CallService` 或 `ServiceContext.CallServiceUseApi`。
- 需要推送时优先用 observe + `NoticeWebSocket`，不要在业务中直接管理 WebSocket 连接。
