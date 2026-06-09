# Skill: core-backend-api

## 描述

本 skill 教会 Copilot agent 如何使用 `github.com/digitalwayhk/core` 框架
生成后端 API、管理服务、WebSocket 推送、MQ 事件流和集群配置。
读完本文件即可独立生成符合框架规范的后端代码，无需额外询问。

**模块路径：** `github.com/digitalwayhk/core`
**Go 版本：** 1.26+

---

## 目录

1. [框架核心概念](#1-框架核心概念)
2. [实体 / 模型层](#2-实体--模型层)
3. [Public API（无需登录）](#3-public-api无需登录)
4. [Private API（需要登录）](#4-private-api需要登录)
5. [标准 Manage 管理服务](#5-标准-manage-管理服务)
6. [Manage Hook 扩展](#6-manage-hook-扩展)
7. [高级 Manage 模式（AppManage + IDoBefore）](#7-高级-manage-模式appmanage--idobefore)
8. [自定义 Operation 按钮](#8-自定义-operation-按钮)
9. [WebSocket 广播推送](#9-websocket-广播推送)
10. [WebSocket 定向推送（HashKey）](#10-websocket-定向推送hashkey)
11. [MQ 事件流（EventBridge）](#11-mq-事件流eventbridge)
12. [集群提供者（Cluster）](#12-集群提供者cluster)
13. [传输层选择（Transport）](#13-传输层选择transport)
14. [服务注册 service.go](#14-服务注册-servicego)
15. [入口 main.go](#15-入口-maingo)
16. [路由路径规则](#16-路由路径规则)
17. [关键约定汇总](#17-关键约定汇总)

---

## 1. 框架核心概念

### IRouter 接口（所有 API 必须实现）

```go
type IRouter interface {
    Parse(req types.IRequest) error             // 绑定请求参数
    Validation(req types.IRequest) error        // 参数校验；返回 nil 才会执行 Do
    Do(req types.IRequest) (interface{}, error) // 业务逻辑
    RouterInfo() *types.RouterInfo              // 路由元信息
}
```

### IRequest 常用方法

| 方法 | 说明 |
|------|------|
| `req.Bind(v)` | 把请求 JSON 绑定到结构体 |
| `req.GetValue("key")` | 读 URL query 参数 |
| `req.GetUser()` | 返回 `(userID, name)`；从 JWT claims 获取 |
| `req.GetClaims("key")` | 读 JWT 自定义字段 |
| `req.NewID()` | 生成雪花 ID（uint） |
| `req.CallService(router, cb...)` | 同步/异步调用另一个服务的路由 |
| `req.ServiceName()` | 当前服务名 |

### API 类型（ApiType）

| 常量 | 路径前缀 | 鉴权 |
|------|----------|------|
| `types.PublicType` | `/api/{service}/{router}` | 无需登录 |
| `types.PrivateType` | `/api/{service}/{router}` | 需要 JWT |
| `types.ManageType` | `/api/manage/{service}/{manage}/{operation}` | 需要管理 JWT |

类型由 **包目录名** 自动推断（`api/public/`→ PublicType，`api/private/`→ PrivateType，`api/manage/`→ ManageType）。

---

## 2. 实体 / 模型层

### entity.Model（基础，无业务状态）

```go
import "github.com/digitalwayhk/core/pkg/persistence/entity"

type Book struct {
    *entity.Model                   // ID、CreatedAt、UpdatedAt、Hashcode
    Title  string `json:"title"`
    Author string `json:"author"`
}

func NewBook() *Book { return &Book{Model: entity.NewModel()} }

// 必须实现 NewModel()，供 ModelList.NewItem() 调用
func (own *Book) NewModel() {
    if own.Model == nil {
        own.Model = entity.NewModel()
    }
}
```

### entity.BaseModel（带业务状态）

当业务需要 `State`（0=待提交, 1=已提交, 2=已发布）时使用：

```go
type Order struct {
    *entity.BaseModel               // 在 Model 基础上增加 State、Code、Name 等
    UserID string `json:"userid"`
    Amount decimal.Decimal `json:"amount"`
}

func NewOrder() *Order { return &Order{BaseModel: entity.NewBaseModel()} }

func (own *Order) NewModel() {
    if own.BaseModel == nil {
        own.BaseModel = entity.NewBaseModel()
    }
}

// ⚠️ BaseModel.GetHash() 使用 Code 字段。
// 若业务实体没有 Code，必须覆写 GetHash() 避免哈希碰撞：
func (own *Order) GetHash() string {
    if own.BaseModel != nil && own.BaseModel.Model != nil {
        return own.BaseModel.Model.GetHash() // 使用 ID 哈希
    }
    return own.BaseModel.GetHash()
}
```

### ModelList（持久化操作）

```go
list := entity.NewModelList[Order](nil)

item := list.NewItem()      // 创建新实例（自动设置雪花 ID）
_ = list.Add(item)          // 添加到列表
_ = list.Save()             // 持久化

rows, total, _ := list.SearchAll(page, size)
rows, _ = list.SearchWhere("UserID", userID)
_ = list.Update(item)       // 更新
_ = list.Delete(item)       // 删除（软删除）
```

---

## 3. Public API（无需登录）

**目录：** `api/public/`  
**路径：** `/api/{service}/{structname}`（全小写结构名；URL 不包含 `public`）

```go
package public

import (
    "github.com/digitalwayhk/core/pkg/server/router"
    "github.com/digitalwayhk/core/pkg/server/types"
)

type Ping struct {
    Name string `json:"name" desc:"调用者名称"`
}

func (own *Ping) Parse(req types.IRequest) error {
    return req.Bind(own)
}

func (own *Ping) Validation(req types.IRequest) error {
    if own.Name == "" {
        own.Name = "guest"
    }
    return nil
}

func (own *Ping) Do(req types.IRequest) (interface{}, error) {
    return map[string]string{"message": "hello " + own.Name}, nil
}

func (own *Ping) RouterInfo() *types.RouterInfo {
    return router.DefaultRouterInfo(own) // 路径、类型自动推断
}
```

**GET 方法** — 在 RouterInfo 中指定：

```go
func (own *GetOrders) RouterInfo() *types.RouterInfo {
    info := router.DefaultRouterInfo(own)
    info.Method = "GET"
    return info
}
```

**明确响应结构（OpenAPI）** — 实现 `IRouterResponse`：

```go
type MyResponse struct {
    ID    uint   `json:"id"`
    State string `json:"state"`
}

func (own *CreateOrder) GetResponse() interface{} {
    return &MyResponse{}
}
```

---

## 4. Private API（需要登录）

**目录：** `api/private/`  
路径和实现与 Public 相同，URL 不包含 `private`，框架自动要求 JWT 鉴权。

```go
package private

import (
    "github.com/digitalwayhk/core/pkg/server/router"
    "github.com/digitalwayhk/core/pkg/server/types"
)

type AddOrder struct {
    Price  string `json:"price"`
    Amount string `json:"amount"`
}

func (own *AddOrder) Parse(req types.IRequest) error { return req.Bind(own) }

func (own *AddOrder) Validation(req types.IRequest) error {
    if userID, _ := req.GetUser(); userID == "" {
        return errors.New("需要登录")
    }
    return nil
}

func (own *AddOrder) Do(req types.IRequest) (interface{}, error) {
    userID, _ := req.GetUser()
    list := entity.NewModelList[Order](nil)
    item := list.NewItem()
    item.UserID = userID
    _ = list.Add(item)
    _ = list.Save()
    return item, nil
}

func (own *AddOrder) RouterInfo() *types.RouterInfo {
    return router.DefaultRouterInfo(own)
}
```

---

## 5. 标准 Manage 管理服务

**目录：** `api/manage/`  
一行嵌入 `ManageService[T]` 即可自动生成 **View / Search / Add / Edit / Remove / Submit / Release** 7 个路由。

```go
package manage

import (
    managepkg "github.com/digitalwayhk/core/service/manage"
    "github.com/digitalwayhk/core/service/manage/view"
    "yourmodule/models"
)

type ProductManage struct {
    *managepkg.ManageService[models.Product]
}

func NewProductManage() *ProductManage {
    own := &ProductManage{}
    own.ManageService = managepkg.NewManageService[models.Product](own)
    return own
}

// ViewModel 配置页面属性（可选）
func (own *ProductManage) ViewModel(v *view.ViewModel) {
    v.Title = "商品管理"
    v.AutoLoad = true
}

// 在 service.go 注册：
// manage.NewProductManage().Routers()...
```

### 自动生成的路由

| 路由 | 路径（示例）| 说明 |
|------|------------|------|
| View | GET `/api/manage/{svc}/productmanage/view` | 获取页面 Schema |
| Search | POST `.../search` | 分页查询列表 |
| Add | POST `.../add` | 新增 |
| Edit | POST `.../edit` | 编辑 |
| Remove | POST `.../remove` | 删除 |
| Submit | POST `.../submit` | 状态 0→1 |
| Release | POST `.../release` | 状态 1→2 |

---

## 6. Manage Hook 扩展

覆写以下方法即可介入生命周期：

```go
// 操作前置（Add/Edit/Remove/Submit/Release）
// 返回 (result, err, stop)；stop=true 时中止后续执行
func (own *ArticleManage) DoBefore(sender interface{}, req types.IRequest) (interface{}, error, bool) {
    if add, ok := sender.(*managepkg.Add[models.Article]); ok {
        if add.Model != nil && (*add.Model).Title == "" {
            return nil, errors.New("标题不能为空"), false // false=返回错误但走完流程
        }
    }
    return nil, nil, false
}

// 操作后置
func (own *ArticleManage) DoAfter(sender interface{}, req types.IRequest) (interface{}, error) {
    return nil, nil
}

// 查询前置；返回 stop=true 时跳过 DB，直接返回 result
func (own *ArticleManage) SearchBefore(sender interface{}, req types.IRequest) (interface{}, error, bool) {
    return nil, nil, false
}

// 查询后置；可整理返回数据
func (own *ArticleManage) SearchAfter(sender interface{}, result *view.TableData, req types.IRequest) (interface{}, error) {
    return result, nil
}
```

### ViewFieldModel 字段定制

```go
func (own *MyManage) ViewFieldModel(model interface{}, field *view.FieldModel) {
    switch {
    case field.IsFieldOrTitle("Price"):
        field.Title = "价格"
        field.Visible = true
        field.IsSearch = true
        field.IsEdit = true
        field.Sorter = true
        field.Index = 1
    case field.IsFieldOrTitle("State"):
        field.Title = "状态"
        field.Visible = true
        field.ComBox("待提交", "已提交", "已发布") // 下拉枚举
        field.Index = 2
    }
}
```

### ViewCommandModel 按钮定制

```go
func (own *MyManage) ViewCommandModel(cmd *view.CommandModel) {
    switch cmd.Command {
    case "submit":
        cmd.IsSplit = true       // 与 release 共用一个按钮位
        cmd.SplitName = "release"
        cmd.IsSelectRow = true
        cmd.IsAlert = true
    case "remove":
        cmd.IsAlert = true
        cmd.Title = "确认删除"
    }
}
```

---

## 7. 高级 Manage 模式（AppManage + IDoBefore）

适用于：多业务共享公共逻辑、需要类型安全的钩子分发、默认只读对外暴露。

### AppManage（应用层公共基类）

```go
// api/manage/appmanage.go  package manage

type IDoBefore[T pt.IModel] interface {
    AddBefore(add *manage.Add[T], req st.IRequest) (interface{}, error, bool)
    EditBefore(edit *manage.Edit[T], req st.IRequest) (interface{}, error, bool)
    RemoveBefore(remove *manage.Remove[T], req st.IRequest) (interface{}, error, bool)
}

type AppManage[T pt.IModel] struct {
    *manage.ManageService[T]
    instance interface{}
}

func NewAppManage[T pt.IModel](instance interface{}) *AppManage[T] {
    own := &AppManage[T]{instance: instance}
    own.ManageService = manage.NewManageService[T](instance)
    return own
}

// 默认只暴露只读路由；子类 Routers() 追加写操作
func (own *AppManage[T]) Routers() []st.IRouter {
    return []st.IRouter{own.View, own.Search}
}

// DoBefore 自动分派到 IDoBefore 具体方法
func (own *AppManage[T]) DoBefore(sender interface{}, req st.IRequest) (interface{}, error, bool) {
    if idb, ok := own.instance.(IDoBefore[T]); ok {
        switch s := sender.(type) {
        case *manage.Add[T]:    return idb.AddBefore(s, req)
        case *manage.Edit[T]:   return idb.EditBefore(s, req)
        case *manage.Remove[T]: return idb.RemoveBefore(s, req)
        }
    }
    return nil, nil, false
}

// 默认空实现；子类选择性覆盖
func (own *AppManage[T]) AddBefore(_ *manage.Add[T], _ st.IRequest) (interface{}, error, bool)    { return nil, nil, false }
func (own *AppManage[T]) EditBefore(_ *manage.Edit[T], _ st.IRequest) (interface{}, error, bool)  { return nil, nil, false }
func (own *AppManage[T]) RemoveBefore(_ *manage.Remove[T], _ st.IRequest) (interface{}, error, bool) { return nil, nil, false }
```

### 叶子 Manage（继承 AppManage）

```go
type OrderManage struct {
    *AppManage[models.Order]
}

func NewOrderManage() *OrderManage {
    own := &OrderManage{}
    own.AppManage = NewAppManage[models.Order](own)
    return own
}

// 追加写操作路由
func (own *OrderManage) Routers() []st.IRouter {
    return append(own.AppManage.Routers(), own.Add, own.Edit, own.Remove, own.Submit, own.Release)
}

// 实现 IDoBefore.AddBefore（类型安全，无需 type switch）
func (own *OrderManage) AddBefore(add *manage.Add[models.Order], req st.IRequest) (interface{}, error, bool) {
    if add.Model != nil && add.Model.Amount.IsZero() {
        return nil, errors.New("数量不能为零"), true
    }
    return nil, nil, false
}
```

---

## 8. 自定义 Operation 按钮

在 Manage 页面添加非 CRUD 操作（如导出、审批）：

```go
// api/manage/button/exportdata.go  package button

import (
    pt "github.com/digitalwayhk/core/pkg/persistence/types"
    st "github.com/digitalwayhk/core/pkg/server/types"
    "github.com/digitalwayhk/core/service/manage"
)

type ExportData[T pt.IModel] struct {
    manage.Operation[T] // 值类型嵌入
}

func NewExportData[T pt.IModel](instance interface{}) *ExportData[T] {
    return &ExportData[T]{Operation: manage.NewOperation[T](instance)}
}

// Parse 返回 nil（导出无请求体，避免 Nginx EOF）
func (own *ExportData[T]) Parse(req st.IRequest) error { return nil }

func (own *ExportData[T]) Validation(req st.IRequest) error {
    return own.Operation.Validation(req)
}

func (own *ExportData[T]) Do(req st.IRequest) (interface{}, error) {
    // 查询数据、生成文件，返回下载地址
    return "https://cdn.example.com/export.csv", nil
}

// RouterInfo 使用 manage.RouterInfo，自动设置 ManageType
func (own *ExportData[T]) RouterInfo() *st.RouterInfo {
    return manage.RouterInfo(own)
}
```

注册到 Manage 的 Routers()：

```go
func (own *OrderManage) Routers() []st.IRouter {
    return append(own.AppManage.Routers(),
        own.Add, own.Edit, own.Remove,
        button.NewExportData[models.Order](own), // 追加自定义按钮
    )
}
```

ViewCommandModel 定制按钮属性：

```go
case "exportdata": // 结构名全小写
    cmd.IsAlert = false
    cmd.IsSelectRow = false
    cmd.Title = "导出"
```

---

## 9. WebSocket 广播推送

**原理：** 订阅路由实现 `IWebSocketRouter`；发布路由调用 `routerInfo.NoticeWebSocket(msg)`。

```go
// api/public/watchprice.go
type WatchPrice struct {
    Symbol string `json:"symbol"`
}

func (own *WatchPrice) Parse(req types.IRequest) error       { return req.Bind(own) }
func (own *WatchPrice) Validation(req types.IRequest) error  { return nil }
func (own *WatchPrice) Do(req types.IRequest) (interface{}, error) {
    return map[string]string{"state": "subscribed"}, nil
}
func (own *WatchPrice) RouterInfo() *types.RouterInfo { return router.DefaultRouterInfo(own) }

// 客户端订阅时框架调用
func (own *WatchPrice) RegisterWebSocket(client types.IWebSocket, req types.IRequest) {}
// 断开时框架调用
func (own *WatchPrice) UnRegisterWebSocket(client types.IWebSocket, req types.IRequest) {}
```

```go
// api/public/publishprice.go
func (own *PublishPrice) Do(req types.IRequest) (interface{}, error) {
    msg := map[string]string{"symbol": own.Symbol, "price": own.Price}
    (&WatchPrice{}).RouterInfo().NoticeWebSocket(msg) // 广播给所有 WatchPrice 订阅者
    return msg, nil
}
```

---

## 10. WebSocket 定向推送（HashKey）

按业务键（如 UserID）分片，只推送给匹配的订阅者：

```go
// api/public/watchbalance.go
type WatchBalance struct {
    UserID string `json:"userid"`
}

// GetHashKey 告诉框架该订阅的分片键
func (own *WatchBalance) GetHashKey() uint64 {
    h := fnv.New64a()
    h.Write([]byte(own.UserID))
    return h.Sum64()
}

// NoticeFiltersRouter 过滤：只推送给 UserID 匹配的订阅者
func (own *WatchBalance) NoticeFiltersRouter(message interface{}, api types.IRouter) (bool, interface{}) {
    msg, ok := message.(*BalanceMsg)
    if !ok { return false, nil }
    return msg.UserID == own.UserID, msg
}

func (own *WatchBalance) Parse(req types.IRequest) error       { return req.Bind(own) }
func (own *WatchBalance) Validation(req types.IRequest) error  { return nil }
func (own *WatchBalance) Do(req types.IRequest) (interface{}, error) {
    return map[string]string{"state": "watching"}, nil
}
func (own *WatchBalance) RouterInfo() *types.RouterInfo { return router.DefaultRouterInfo(own) }
```

---

## 11. MQ 事件流（EventBridge）

### 注册自定义 Provider（测试 / 本地）

```go
import "github.com/digitalwayhk/core/pkg/server/mq"

mq.RegisterProviderFactory("my-fake", func(ctx context.Context, cfg *config.MQConfig) (mq.MQProvider, error) {
    return &MyFakeProvider{}, nil
})
```

`MQProvider` 接口：

```go
type MQProvider interface {
    Name() string
    Connect(ctx context.Context) error
    Close() error
    Health(ctx context.Context) error
    Publish(ctx context.Context, subject string, data []byte, opts *PublishOptions) error
    Subscribe(ctx context.Context, subject string, handler func(*Message)) (cancel func(), error)
}
```

### 通过 ServiceContext 使用 EventBridge

```go
cfg := config.NewServiceDefaultConfig("myservice", 18080)
cfg.MQ.Mode     = "on"
cfg.MQ.Provider = "my-fake"
cfg.MQ.Usage    = []string{"event-stream"} // 包含此项时自动启用 EventBridge

sc := router.NewServiceContextWithConfig(&MyService{}, cfg)

// 订阅本地事件流
sc.EventStream.Subscribe("order.created", func(env *event.Envelope) {
    fmt.Println("event:", env.Type)
})

// 通过 MQ 发布（自动桥接到 EventStream）
env := event.NewEnvelope("myservice", "order.created", []byte(`{"id":1}`))
_ = sc.EventBridge.Publish(ctx, "orders.events", env)
```

### Redis Stream（内置 Provider）

```go
cfg.MQ.Provider = "redis-stream"
cfg.MQ.Providers.RedisStream.Addr    = "127.0.0.1:6379"
cfg.MQ.Providers.RedisStream.GroupID = "my-group"
```

### NATS JetStream（内置 Provider）

```go
cfg.MQ.Provider = "nats"
cfg.MQ.Providers.NATS.URL = "nats://127.0.0.1:4222"
```

---

## 12. 集群提供者（Cluster）

### Local（进程内，开发/单机）

```go
import "github.com/digitalwayhk/core/pkg/server/cluster"

provider := cluster.NewLocalProvider(3*time.Second, 10*time.Second, 30*time.Second)
provider.Start()
defer provider.Close()

node := &cluster.NodeInfo{
    ID:          "svc-dc1-m1",
    ServiceName: "myservice",
    Address:     "127.0.0.1",
    Port:        18080,
    Status:      cluster.NodeStatusRunning,
}
_ = provider.Register(ctx, node)
nodes, _ := provider.List(ctx, "myservice", cluster.NodeStatusRunning)
_ = provider.Deregister(ctx, node.ID)
```

### etcd / Consul（通过 ServerConfig）

```go
cfg := config.NewServiceDefaultConfig("myservice", 18080)
cfg.Cluster.Mode     = "on"
cfg.Cluster.Provider = "etcd" // 或 "consul"
cfg.Cluster.ApplyDefaults()
cfg.Cluster.Providers.Etcd.Endpoints = []string{"127.0.0.1:2379"}
cfg.Cluster.Providers.Etcd.TTL       = 10 * time.Second
```

---

## 13. 传输层选择（Transport）

```go
import (
    "github.com/digitalwayhk/core/pkg/server/config"
    "github.com/digitalwayhk/core/pkg/server/transport"
)

cfg := config.TransportConfig{
    Internal: "grpc",              // 内部通信：grpc | http | socket
    Fallback: []string{"http"},    // 降级顺序
}
cfg.ApplyDefaults()

selector, _ := transport.BuildSelector(cfg)
```

> **注意：** `Internal: "mq"` 不是合法的直连传输，MQ 通过 EventBridge 单独配置。

---

## 14. 服务注册 service.go

每个服务目录下一个 `service.go`，实现 `IService`：

```go
package myservice

import (
    "github.com/digitalwayhk/core/pkg/server/types"
    "yourmodule/api/manage"
    "yourmodule/api/private"
    "yourmodule/api/public"
)

type MyService struct{}

func (own *MyService) ServiceName() string { return "myservice" }

func (own *MyService) Routers() []types.IRouter {
    routers := []types.IRouter{
        // Public
        &public.Ping{},
        &public.GetProducts{},
        // Private
        &private.AddOrder{},
    }
    // Manage（返回多个路由，用 ... 展开）
    routers = append(routers, manage.NewProductManage().Routers()...)
    routers = append(routers, manage.NewOrderManage().Routers()...)
    return routers
}

// SubscribeRouters 订阅其他服务的路由（跨服务通知）
func (own *MyService) SubscribeRouters() []*types.ObserveArgs {
    return nil // 无跨服务订阅时返回 nil
}
```

---

## 15. 入口 main.go

```go
package main

import (
    "github.com/digitalwayhk/core/pkg/server/run"
    "github.com/digitalwayhk/core/pkg/server/types"
    "yourmodule"
)

func main() {
    server := run.NewWebServer()
    server.AddIService(&yourmodule.MyService{}, &types.ServerOption{
        IsCors:      true,  // 允许跨域
        IsWebSocket: true,  // 启用 WebSocket（public + private 路由均可被订阅）
    })
    server.Start()
}
```

默认配置文件：`etc/{serviceName}.json`（框架首次启动时自动生成）。

---

## 16. 路由路径规则

```
/api/{serviceName}/{structNameLower}
```

示例：
- `package public`, 服务名 `demo`, 结构 `GetOrder` → `/api/demo/getorder`
- `package private`, 服务名 `demo`, 结构 `AddOrder` → `/api/demo/addorder`
- `ManageService[Product]` 中的 `Search` → `/api/manage/demo/productmanage/search`
- 自定义 Operation `ExportData` → `/api/manage/demo/productmanage/exportdata`

**`router.DefaultRouterInfo(own)`** 会自动推断普通路由路径和 ApiType（依据包路径中 `public` / `private` / `manage` 关键字），但普通路由路径不包含 ApiType。标准 Manage CRUD 和自定义 Operation 使用 `manage.RouterInfo(own)`。

---

## 17. 关键约定汇总

| 约定 | 说明 |
|------|------|
| 每个 IRouter 自己管理字段 | `Parse` 直接绑定到 `own` 本身；不使用全局状态 |
| `Validation` 返回 `nil` 才执行 `Do` | 所有参数校验放在 `Validation`，业务逻辑放在 `Do` |
| `entity.BaseModel` 覆写 `GetHash()` | 若实体无 `Code`，必须委托 `Model.GetHash()`（ID哈希）防止碰撞 |
| `NewModel()` 方法必须实现 | 供 `ModelList.NewItem()` 调用，初始化嵌入的 `*entity.Model` / `*entity.BaseModel` |
| Manage 多层继承先调上层 | `ViewModel/ViewFieldModel/ViewCommandModel` 必须先调 `own.上层.Xxx(...)` |
| `DoBefore` 返回 `stop=true` 中止执行 | `stop=true` 时框架不继续执行后续操作，只返回 `result/err` |
| `SearchBefore` 返回 `stop=true` 跳过 DB | 适合内存缓存或全量本地数据源 |
| `Operation[T]` 值类型嵌入 | 自定义按钮用 `manage.Operation[T]`（值类型），不是 `*Operation` |
| `manage.RouterInfo(own)` 用于 Operation | 自动设置 `ManageType` 和路径 |
| 服务名全小写无连字符 | `ServiceName()` 返回值直接用于路径，建议全小写字母 |
| go test 运行管理服务测试 | `go test ./service/manage/...` |
| go build 验证编译 | `go build ./...` 无错误后再提交 |
| gofmt 所有新增 Go 文件 | `gofmt -w 文件路径` |
