# Skill: core-backend-api

## 描述

本 skill 教会 Copilot agent 如何使用 `github.com/digitalwayhk/core` 框架
生成后端 API、管理服务、WebSocket 推送、MQ 事件流和集群配置。
读完本文件即可独立生成符合框架规范的后端代码，无需额外询问。

**模块路径：** `github.com/digitalwayhk/core`（通过 `go.mod` 引用，不是直接修改框架仓库）
**Go 版本：** 1.26+

> ⚠️ **核心原则：** 业务代码写在**你自己的仓库**里，通过 `go.mod` 引用框架。
> 不要把业务逻辑代码写进 `github.com/digitalwayhk/core` 仓库，
> 也不要使用 `replace github.com/digitalwayhk/core => ../core`（仅临时本地调试时允许）。

---

## 目录

0. [快速开始（新项目接入）](#0-快速开始新项目接入)
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
18. [前端调用 API（Web 集成）](#18-前端调用-apiweb-集成)
    - 18.1 统一响应格式
    - 18.2 URL 规则与三个核心请求
    - 18.3 SearchItem 参数结构
    - 18.4 获取测试 Token（TestToken）
    - 18.5 直接调用 API
    - 18.6 ModelAttribute schema 类型说明

---

## 0. 快速开始（新项目接入）

### 引用框架（go.mod）

在你自己的仓库里通过 `go get` 引入：

```sh
go mod init github.com/yourorg/yourservice
go get github.com/digitalwayhk/core@latest
```

`go.mod` 示例：

```
module github.com/yourorg/yourservice

go 1.26

require github.com/digitalwayhk/core v0.0.0
```

> **禁止做法：** 不要把业务代码写进 `github.com/digitalwayhk/core` 仓库；
> 不要在 `go.mod` 里加 `replace github.com/digitalwayhk/core => ../core`（仅临时本地调试允许）。

---

### 推荐目录结构

框架支持**单服务**和**多服务**两种项目布局，按需选择。

---

#### 单服务项目

适用于功能单一、规模较小的项目：

```
yourservice/
├── cmd/
│   └── main.go                  # 服务入口（见第 15 节）
├── deploy/
│   ├── environments/
│   │   ├── local/               # 本地开发环境配置
│   │   ├── test/                # 测试环境配置
│   │   └── prod/                # 生产环境配置
│   └── docker/                  # Docker / docker-compose 文件
├── internal/core/{serviceName}/ # 服务业务代码（见下方服务子目录说明）
├── go.mod
└── go.sum
```

---

#### 多服务项目（推荐，参考 futures 工程）

适用于拆分多个业务领域的中大型项目，每个服务独立目录，共享 `internal/pkg`：

```
yourproject/
├── cmd/                              # 各服务独立入口
│   ├── {serviceA}/
│   │   └── main.go                  # 单独启动 serviceA
│   ├── {serviceB}/
│   │   └── main.go                  # 单独启动 serviceB
│   └── all-in-one/
│       └── main.go                  # 合并启动所有服务（开发/单机部署用）
├── deploy/                           # 部署相关
│   ├── environments/
│   │   ├── local/                   # 本地环境（etc/*.json 由框架自动生成）
│   │   ├── test/                    # 测试环境
│   │   └── prod/                    # 生产环境
│   └── docker/                      # Dockerfile、docker-compose.yml
├── internal/
│   ├── core/                        # 各业务服务（一个目录 = 一个服务）
│   │   ├── {serviceA}/              # 服务 A（见下方服务子目录说明）
│   │   └── {serviceB}/              # 服务 B
│   └── pkg/                         # 跨服务共享包
│       ├── models/                  # 共享数据模型
│       ├── services/                # 共享业务逻辑
│       └── api/                     # 共享 API 工具（如公共按钮、vo 等）
├── go.mod
└── go.sum
```

---

#### 服务子目录说明（`internal/core/{serviceName}/`）

每个服务目录的内部结构统一如下：

```
internal/core/{serviceName}/
├── service.go                   # 服务注册，实现 IService 接口（见第 14 节）
├── api/
│   ├── public/                  # 无需登录的 API（见第 3 节）
│   ├── private/                 # 需要 JWT 的 API（见第 4 节）
│   ├── manage/                  # 管理后台 API（见第 5 节）
│   └── release/                 # 跨服务内部调用 API（ManageType，不对外暴露）
├── models/                      # 数据模型，一个文件 = 一张表（见第 2 节）
│   ├── order.go
│   └── market.go
├── services/                    # 业务逻辑层（供 api/ 和 task/ 调用）
│   ├── trade/
│   └── risk/
├── task/                        # 定时任务 / 后台 Worker
├── send/                        # WebSocket 推送 / 跨服务通知（见第 9–10 节）
└── test/                        # 本服务的集成测试
    └── api_test/                # API 层测试
```

> **注意：**
> - `service.go` 放在服务根目录（不是 `internal/core/` 根目录）
> - `models/` 可按领域建子目录（如 `models/orders/`、`models/markets/`），但每个 Go 文件只定义一个模型
> - `task/` 对应框架的后台定时/消费逻辑，不是 Go test 文件
> - `test/` 存放该服务的 Go test 文件；`api_test/` 下放 API 集成测试

---

### 配置文件（自动生成）

**不需要预先创建任何配置文件。** 框架在首次启动时自动生成：

- 文件路径：`etc/{serviceName}.json`（JSON 格式）
- 默认端口：8080（第一个服务），之后每增加一个服务 +1
- 首次运行后配置文件写入磁盘，后续修改该文件即可覆盖默认值

```sh
go run ./cmd/main.go
# 首次运行后，etc/myservice.json 自动生成
```

端口也可通过命令行参数临时覆盖（不影响已保存的配置文件）：

```sh
go run ./cmd/main.go -p 9090
```

---

### 启动并验证

```sh
go run ./cmd/main.go
# etc/myservice.json 首次运行时自动生成（默认端口 8080）

# TestToken 接口验证服务正常（见第 18.4 节）
curl "http://localhost:8080/api/myservice/public/testtoken?userid=test&type=0"
```

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

### API 类型（ApiType）与实际 URL

| 常量 | 实际 URL 路径 | 鉴权 |
|------|--------------|------|
| `types.PublicType` | `/api/{serviceName}/{structNameLower}` | 无需登录 |
| `types.PrivateType` | `/api/{serviceName}/{structNameLower}` | 需要 JWT |
| `types.ManageType` | `/api/manage/{serviceName}/{controllerNameLower}/{opNameLower}` | 需要管理 JWT |

> ⚠️ **URL 中没有 `/public/` 或 `/private/` 前缀。**  
> 目录名（`api/public/`、`api/private/`）只用于推断 ApiType（决定是否需要 JWT），**不出现在实际路由路径中**。  
> `ManageService` 的路由由框架统一挂载在 `/api/manage/...` 下。

类型由 **包目录路径** 自动推断（`api/public/`→ PublicType，`api/private/`→ PrivateType，`api/manage/`→ ManageType）。

---

## 2. 实体 / 模型层

### 🔑 核心约定（必读）

1. **自动建表建库**：调用 `entity.NewModelList[T](nil)` 时框架自动检测并创建表（GORM AutoMigrate），
   无需编写 SQL 或手动迁移脚本。新增字段后重启即自动补列。
2. **一个 model 文件 = 一张表**：每个 Go 文件只定义一个业务模型结构体，
   对该表的所有字段、索引、关联关系、业务验证逻辑，全部写在同一文件。
   修改表结构时只需改一个文件。
3. **可无缝切换数据库**：默认 SQLite（开发），配置 MySQL 后业务代码零改动。
   切换方式通过 `IDBName` 或配置文件实现，不侵入 model 代码。
4. **表名默认规则**：`entity.Model.GetLocalDBName()` 返回 `"models"`，即所有模型默认存储在
   同一个 `models` 数据库。实现 `IDBName` 接口可路由到独立库/表。

---

### 基础类型选择指南

| 嵌入类型 | 适用场景 | 包含字段 | GetHash 默认值 |
|----------|----------|----------|---------------|
| `*entity.Model` | 通用实体（商品、用户、菜单等） | ID, CreatedAt, UpdatedAt, Hashcode | ID |
| `*entity.BaseModel` | 带业务状态的实体（有 Code 唯一标识） | + State, Code, Name, Describe, 操作人 | Code |
| `*entity.BaseOrderModel` | 单据/交易（不可删除，TraceID+UserID 唯一） | + TraceID, Code, UserID, State | TraceID+UserID |
| `*entity.BaseRecordModel` | 日志/记录（只写不改不删） | + TraceID | TraceID |

---

### entity.Model（通用实体）

```go
// models/product.go
package models

import "github.com/digitalwayhk/core/pkg/persistence/entity"

// Product 商品表。一个 model 文件对应一张表，所有字段、关联、验证都在此文件。
type Product struct {
    *entity.Model
    Name     string  `json:"name"  desc:"商品名称"`
    Price    int64   `json:"price" desc:"价格（分）"`
    Category string  `json:"category" desc:"分类"`
    // 外键关联：一个商品对应多个订单行
    OrderLines []*OrderLine `json:"orderlines,omitempty" gorm:"foreignkey:ProductID"`
}

func NewProduct() *Product { return &Product{Model: entity.NewModel()} }

// NewModel 必须实现，供 ModelList.NewItem() 调用
func (own *Product) NewModel() {
    if own.Model == nil {
        own.Model = entity.NewModel()
    }
}
```

### entity.BaseModel（带业务状态 + Code 唯一标识）

```go
// models/token.go
package models

import "github.com/digitalwayhk/core/pkg/persistence/entity"

// Token 币种表。Code 是业务唯一键，State 控制生命周期（0=待上线 1=上线 2=下线）。
type Token struct {
    *entity.BaseModel               // Code, Name, State, Describe, 操作人...
    Symbol   string `json:"symbol" desc:"交易对符号"`
    Decimals int    `json:"decimals" desc:"精度位数"`
}

func NewToken() *Token { return &Token{BaseModel: entity.NewBaseModel()} }

func (own *Token) NewModel() {
    if own.BaseModel == nil {
        own.BaseModel = entity.NewBaseModel()
    }
}
// GetHash 默认使用 Code（由 BaseModel 提供），Code 唯一即不需要覆写。
// ⚠️ 若实体没有业务 Code，必须覆写为 ID 哈希（见下方 Order 示例）。
```

### entity.BaseModel 无 Code 时的 GetHash 修复

```go
// models/order.go
package models

import (
    "github.com/digitalwayhk/core/pkg/persistence/entity"
    "github.com/shopspring/decimal"
)

// Order 订单表。无 Code 字段，GetHash 必须委托给 Model（ID 哈希），否则所有空 Code 会哈希碰撞。
type Order struct {
    *entity.BaseModel
    UserID  string          `json:"userid"  desc:"用户ID"`
    Price   decimal.Decimal `json:"price"   desc:"成交价"`
    Amount  decimal.Decimal `json:"amount"  desc:"成交量"`
    TokenID uint            `json:"tokenid" desc:"币种ID"`
    // 自关联：子订单
    ChildDetail []*Order `json:"childdetail,omitempty" gorm:"foreignkey:ParentID"`
    ParentID    uint     `json:"parentid"`
}

func NewOrder() *Order { return &Order{BaseModel: entity.NewBaseModel()} }

func (own *Order) NewModel() {
    if own.BaseModel == nil {
        own.BaseModel = entity.NewBaseModel()
    }
}

// ⚠️ 覆写 GetHash：无 Code 时委托给 Model.GetHash()（ID 哈希），防止碰撞
func (own *Order) GetHash() string {
    if own.BaseModel != nil && own.BaseModel.Model != nil {
        return own.BaseModel.Model.GetHash()
    }
    return own.BaseModel.GetHash()
}
```

### entity.BaseOrderModel（单据，不可删除）

```go
// models/tradeorder.go
package models

import (
    "github.com/digitalwayhk/core/pkg/persistence/entity"
    "github.com/shopspring/decimal"
)

// TradeOrder 交易单据。TraceID+UserID 构成唯一键，不允许删除。
type TradeOrder struct {
    *entity.BaseOrderModel          // TraceID, Code, UserID, State；RemoveValid 返回错误
    Price  decimal.Decimal `json:"price"`
    Amount decimal.Decimal `json:"amount"`
}

func NewTradeOrder() *TradeOrder { return &TradeOrder{BaseOrderModel: entity.NewBaseOrderModel()} }

func (own *TradeOrder) NewModel() {
    if own.BaseOrderModel == nil {
        own.BaseOrderModel = entity.NewBaseOrderModel()
    }
}
// GetHash 由 BaseOrderModel 提供：utils.HashCodes(TraceID, UserID) —— 不需要覆写
```

### entity.BaseRecordModel（日志，只写不改不删）

```go
// models/auditlog.go
package models

import "github.com/digitalwayhk/core/pkg/persistence/entity"

// AuditLog 审计日志。只允许写入，UpdateValid/RemoveValid 均返回错误。
type AuditLog struct {
    *entity.BaseRecordModel         // TraceID；UpdateValid/RemoveValid 直接报错
    Action   string `json:"action"   desc:"操作"`
    Operator string `json:"operator" desc:"操作人"`
    Detail   string `json:"detail"   desc:"详情"`
}

func NewAuditLog() *AuditLog { return &AuditLog{BaseRecordModel: entity.NewBaseRecordModel()} }

func (own *AuditLog) NewModel() {
    if own.BaseRecordModel == nil {
        own.BaseRecordModel = entity.NewBaseRecordModel()
    }
}
// GetHash 由 BaseRecordModel 提供：utils.HashCodes(TraceID) —— 不需要覆写
```

---

### 可选 Hook 接口（写在 model 文件中）

#### IDBName — 路由到独立库

```go
// 默认所有模型都存入 "models" 库。
// 实现此接口后，该表会存入独立数据库文件（SQLite）或独立 MySQL 数据库。
func (own *IPWhiteModel) GetLocalDBName() string  { return "security" }
func (own *IPWhiteModel) GetRemoteDBName() string { return "security" }
```

#### IScopesTableName — 自定义表名

```go
// 默认表名由 GORM 根据结构名生成（蛇形）。需要自定义时实现：
func (own *OrderArchive) TableName() string { return "order_archives_2024" }
```

#### IModelValidHook — 写操作前置验证

```go
// 在 Add/Update/Remove 真正执行前调用。返回错误则拒绝操作。
// entity.Model 已提供空实现；entity.BaseModel/BaseOrderModel/BaseRecordModel 已有内置校验。
// 在 model 文件中覆写：

func (own *Product) AddValid() error {
    if own.Name == "" { return errors.New("商品名不能为空") }
    if own.Price < 0  { return errors.New("价格不能为负") }
    return nil
}

func (own *Product) UpdateValid(old interface{}) error {
    if own.Price < 0 { return errors.New("价格不能为负") }
    return nil
}

func (own *Product) RemoveValid() error {
    // 业务约束：已上架商品不可删除
    if own.State > 0 { return errors.New("已上架商品不能删除") }
    return nil
}
```

#### IModelSearchHook — 自定义查询条件注入

```go
// SearchWhere 在每次查询前调用，可动态注入额外条件（如租户隔离、逻辑删除过滤）。
func (own *Product) SearchWhere(ws []*types.WhereItem) ([]*types.WhereItem, error) {
    // 仅查询未删除的商品
    ws = append(ws, &types.WhereItem{Column: "deleted_at", Symbol: "isnull"})
    return ws, nil
}
```

#### GetHash — 唯一标识计算

```go
// GetHash 决定去重逻辑：Add 时若 GetHash() 与已有记录碰撞，视为重复插入。
// 规则：
//   - entity.Model:          使用 Hashcode 字段（若非空）或 ID
//   - entity.BaseModel:      使用 Code 字段
//   - entity.BaseOrderModel: 使用 TraceID + UserID
//   - entity.BaseRecordModel:使用 TraceID
// 自定义示例（按业务唯一键）：
func (own *MenuModel) GetHash() string {
    return utils.HashCodes(own.Url) // Url 是菜单的业务唯一键
}
```

#### IsPreload — 查询时自动预加载关联

```go
// 实现此方法返回 true，查询时自动 Preload 所有 gorm 外键关联字段。
func (own *MenuModel) IsPreload() bool { return true }
```

---

### ModelList 完整 API

```go
list := entity.NewModelList[Product](nil) // nil = 使用默认 DB adapter

// ── 写操作 ──
item := list.NewItem()           // 创建实例并调用 NewModel()
_ = list.Add(item)               // 入队（调用 AddValid 验证）
_ = list.Update(item)            // 更新（调用 UpdateValid 验证）
_ = list.Remove(item)            // 删除（调用 RemoveValid 验证）
_ = list.Save()                  // 提交所有待处理操作到 DB

// ── 快捷查询 ──
item, _  := list.SearchId(id)                     // 按 ID 查询
item, _  := list.SearchCode("CODE-001")            // 按 Code 查询（BaseModel）
item, _  := list.SearchHash(hash)                 // 按 Hashcode 查询
items, _ := list.SearchName("iPhone")             // 按 Name 查询（BaseModel）
items, _ := list.SearchWhere("Category", "phone") // 按任意字段等值查询

// ── 分页查询 ──
rows, total, _ := list.SearchAll(page, size)

// ── 带条件分页查询 ──
rows, total, _ := list.SearchAll(1, 20, func(si *pt.SearchItem) {
    si.AddWhereN("Category", "phone")          // WHERE category = 'phone'
    si.AddWhere(&pt.WhereItem{
        Column: "Price",
        Symbol: "between",
        Value:  []int64{1000, 5000},           // BETWEEN 1000 AND 5000
    })
    si.AddWhere(&pt.WhereItem{
        Column:   "Name",
        Symbol:   "like",
        Value:    "iPhone",                    // LIKE '%iPhone%'
        Relation: "And",
    })
    si.AddSortN("Price", true)                 // ORDER BY price ASC
})

// ── 聚合 ──
total, _ := list.SearchSum("Price", func(si *pt.SearchItem) {
    si.AddWhereN("Category", "phone")
})

// ── 单条 ──
item, _ := list.SearchOne(func(si *pt.SearchItem) {
    si.AddWhereN("Name", "iPhone 15")
})
```

### SearchItem 支持的 Symbol

| Symbol | SQL | 备注 |
|--------|-----|------|
| `""` / `"="` | `= value` | 默认等值 |
| `"like"` | `LIKE '%value%'` | 模糊查询 |
| `"left"` | `LIKE 'value%'` | 前缀 |
| `"right"` | `LIKE '%value'` | 后缀 |
| `"in"` | `IN (v1, v2)` | Value 传切片 |
| `"notin"` | `NOT IN (...)` | |
| `"between"` | `BETWEEN v1 AND v2` | Value 传 `[2]T` 或 `[]T` |
| `"isnull"` | `IS NULL` | |
| `"isnotnull"` | `IS NOT NULL` | |
| `">"` `">="` `"<"` `"<="` `"!="` | 比较运算符 | |

---

## 3. Public API（无需登录）

**目录：** `api/public/`  
**实际路径：** `/api/{serviceName}/{structNameLower}`（无 `/public/` 前缀）  
**鉴权：** 无（`auth: false`）

> 写完 handler 后，必须在 `api/release/routers.go` 的 `GetPublicRouters()` 中添加该路由，
> `service.go` 才会注册它。

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
**实际路径：** `/api/{serviceName}/{structNameLower}`（与 Public 相同，无 `/private/` 前缀）  
**鉴权：** 需要 Bearer JWT（`auth: true`）

> 写完 handler 后，必须在 `api/release/routers.go` 的对应函数中添加该路由。

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

## 4.1 release/routers.go — 路由发布注册表

**每个服务必须有 `api/release/routers.go`**，它是路由的"发布清单"。  
框架只注册 `service.go` 的 `Routers()` 所返回的路由，  
而 `Routers()` 通过调用 `release.GetPublicRouters()` / `release.GetManageRouters()` 等函数来收集路由。

> **新增一个 handler 后，必须在此文件中添加，否则框架不会注册该路由。**

```go
// api/release/routers.go
package release

import (
    "yourmodule/api/manage"
    "yourmodule/api/private"
    "yourmodule/api/public"
    "github.com/digitalwayhk/core/pkg/server/types"
)

// GetPublicRouters 返回所有无需登录的路由（auth: false）
// 路由路径格式：/api/{serviceName}/{structNameLower}
func GetPublicRouters() []types.IRouter {
    return []types.IRouter{
        &public.Ping{},
        &public.GetProducts{},
        &public.PlaceOrder{},    // ← 新增时在此添加
    }
}

// GetPrivateRouters 返回需要 JWT 的路由（auth: true）
// 路由路径格式：/api/{serviceName}/{structNameLower}
func GetPrivateRouters() []types.IRouter {
    return []types.IRouter{
        &private.AddOrder{},     // ← 新增时在此添加
    }
}

// GetManageRouters 返回管理后台路由（auth: true，管理员 JWT）
// 路由路径格式：/api/manage/{serviceName}/{controllerNameLower}/{op}
func GetManageRouters() []types.IRouter {
    routers := make([]types.IRouter, 0)
    routers = append(routers, manage.NewProductManage().Routers()...)
    routers = append(routers, manage.NewOrderManage().Routers()...)
    return routers
}
```

`service.go` 中的 `Routers()` 直接调用以上函数：

```go
func (own *MyService) Routers() []types.IRouter {
    routers := make([]types.IRouter, 0)
    routers = append(routers, release.GetPublicRouters()...)
    routers = append(routers, release.GetPrivateRouters()...)
    routers = append(routers, release.GetManageRouters()...)
    return routers
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

// 在 api/release/routers.go 的 GetManageRouters() 中添加：
// routers = append(routers, manage.NewProductManage().Routers()...)
```

### 自动生成的路由

| 路由 | 路径（示例）| 说明 |
|------|------------|------|
| View | GET `/api/{svc}/manage/productmanage/view` | 获取页面 Schema |
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
    "yourmodule/api/release"
)

type MyService struct{}

func (own *MyService) ServiceName() string { return "myservice" }

// Routers 通过 release 包收集所有已发布的路由。
// 新增 API 时，在 api/release/routers.go 对应函数中添加，不要直接修改此函数。
func (own *MyService) Routers() []types.IRouter {
    routers := make([]types.IRouter, 0)
    routers = append(routers, release.GetPublicRouters()...)
    routers = append(routers, release.GetPrivateRouters()...)
    routers = append(routers, release.GetManageRouters()...)
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

默认端口 8080，配置文件 `etc/{serviceName}.json` 首次运行自动生成，无需预先创建。

---

## 16. 路由路径规则

### Public / Private API

```
/api/{serviceName}/{structNameLower}
```

- 目录 `api/public/` 或 `api/private/` **只影响鉴权**，不影响 URL
- 服务名取自 `ServiceName()` 返回值（全小写）
- 结构名全小写，例如 `PlaceOrder` → `placeorder`

示例：
- `package public`，服务名 `trades`，结构 `PlaceOrder` → `POST /api/trades/placeorder`（auth: false）
- `package private`，服务名 `trades`，结构 `AddOrder` → `POST /api/trades/addorder`（auth: true）

### ManageService API

```
/api/manage/{serviceName}/{controllerNameLower}/{opNameLower}
```

示例：
- `ProductManage`（服务名 `demo`）中的 `Search` → `POST /api/manage/demo/productmanage/search`
- 自定义 Operation `ExportData` → `POST /api/manage/demo/productmanage/exportdata`

### 框架内置路由（servermanage）

框架自动注册以下系统路由（不需要业务代码）：

```
GET  /api/{serviceName}/testtoken      # 获取测试 JWT
GET  /api/health                       # 健康检查
POST /api/servermanage/queryservice    # 服务发现
POST /api/servermanage/queryrouters    # 路由查询
...
```

**`router.DefaultRouterInfo(own)`** 自动推断路径和 ApiType（依据包路径中 `public` / `private` / `manage` 关键字）。

---

## 17. 关键约定汇总

### Model 层约定

| 约定 | 说明 |
|------|------|
| **一个 model 文件 = 一张表** | 每个 Go 文件只定义一个结构体；该表的字段、关联、验证全部写在此文件 |
| **自动建表，无需 migration** | `NewModelList[T](nil)` 首次使用时自动 `AutoMigrate`；新增字段重启即生效 |
| **数据库可替换，代码零改动** | 默认 SQLite；改配置文件即切 MySQL；实现 `IDBName` 可按模型路由到不同库 |
| **`NewModel()` 方法必须实现** | 供 `ModelList.NewItem()` 调用，负责初始化嵌入的 `*entity.Model` / `*entity.BaseModel` |
| **`entity.BaseModel` 覆写 `GetHash()`** | 若实体无 `Code`，必须委托 `Model.GetHash()`（ID哈希）防止碰撞 |
| **BaseOrderModel 不可删除** | `RemoveValid()` 已内置返回错误；单据类型永远用此基类 |
| **BaseRecordModel 不可改不可删** | `UpdateValid/RemoveValid` 已内置返回错误；日志/流水永远用此基类 |
| **写操作验证放在 Valid 方法** | `AddValid/UpdateValid/RemoveValid` 在 DB 操作前自动调用 |
| **默认库名 "models"** | 所有模型默认存入 `models` DB；实现 `GetLocalDBName()` 可改为独立库 |
| **外键关联用 gorm 标签** | `gorm:"foreignkey:XxxID"` 或 `gorm:"foreignkey:ID;references:XxxID"` |
| **`IsPreload() bool`** | 返回 true 时查询自动预加载所有 gorm 关联字段 |

### API 层约定

| 约定 | 说明 |
|------|------|
| **框架通过 go.mod 引用** | `go get github.com/digitalwayhk/core@latest`；业务代码写在自己仓库，不改框架代码 |
| **配置文件** | `etc/{serviceName}.json`（JSON），首次运行自动生成；无需预先创建 |
| 每个 IRouter 自己管理字段 | `Parse` 直接绑定到 `own` 本身；不使用全局状态 |
| `Validation` 返回 `nil` 才执行 `Do` | 所有参数校验放在 `Validation`，业务逻辑放在 `Do` |
| Manage 多层继承先调上层 | `ViewModel/ViewFieldModel/ViewCommandModel` 必须先调 `own.上层.Xxx(...)` |
| `DoBefore` 返回 `stop=true` 中止执行 | `stop=true` 时框架不继续执行后续操作，只返回 `result/err` |
| `SearchBefore` 返回 `stop=true` 跳过 DB | 适合内存缓存或全量本地数据源 |
| `Operation[T]` 值类型嵌入 | 自定义按钮用 `manage.Operation[T]`（值类型），不是 `*Operation` |
| `manage.RouterInfo(own)` 用于 Operation | 自动设置 `ManageType` 和路径 |
| 服务名全小写无连字符 | `ServiceName()` 返回值直接用于路径，建议全小写字母 |
| go test 运行管理服务测试 | `go test ./service/manage/...` |
| go build 验证编译 | `go build ./...` 无错误后再提交 |
| gofmt 所有新增 Go 文件 | `gofmt -w 文件路径` |
| **URL 无 `/public/` `/private/` 前缀** | 目录名只决定鉴权；实际 URL = `/api/{svc}/{structLower}` |
| **新增 API 必须在 release/routers.go 注册** | `service.go` 通过调用 `release.GetPublicRouters()` 等函数收集路由；不在 release 中添加则不生效 |

---

## 18. 前端调用 API（Web 集成）

前端基于 **Umi + Ant Design Pro（React）**，框架提供了一套约定式的 API 调用机制。
所有接口均为 `POST`（除 TestToken 为 `GET`），响应统一为 `ResultData` 结构。

### 18.1 统一响应格式

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

### 18.2 URL 规则与三个核心请求

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

### 18.3 SearchItem 参数结构

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

### 18.4 获取测试 Token（开发 / 调试用）

框架内置 `TestToken` 接口，**无需登录即可获取 JWT**，专为开发调试设计。

```
GET /api/{serviceName}/public/testtoken?userid={userId}&type={tokenType}
```

| 参数 | 说明 |
|------|------|
| `userid` | 任意用户 ID 字符串（必填） |
| `type` | `0` = 普通用户 token（用于 Private API）<br>`1` = 管理员 token（用于 Manage API）<br>`2` = 服务管理 token（用于 ServerManage API） |

**示例（curl）：**
```bash
# 获取普通用户 token（用于调用 /private/ 接口）
curl "http://localhost:18080/api/demo/public/testtoken?userid=user001&type=0"

# 获取管理员 token（用于调用 /manage/ 接口）
curl "http://localhost:18080/api/demo/public/testtoken?userid=admin001&type=1"
```

**返回值**（`data` 字段即为 token 字符串）：
```json
{
  "success": true,
  "data": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "code": 200
}
```

**使用 token 调用 Private API：**
```bash
TOKEN=$(curl -s "http://localhost:18080/api/demo/public/testtoken?userid=user001&type=0" | jq -r .data)

curl -X POST "http://localhost:18080/api/demo/private/addorder" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"userid":"user001","amount":"100.00","tokenid":"1"}'
```

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

**⚠️ 注意：** `TestToken` 是注册在 `public` 包下的无鉴权接口，生产环境应通过网关/防火墙屏蔽该路径，或在正式上线前替换为真实的认证流程（Casdoor / Logto）。

### 18.5 直接调用 API（无需 WayPage）

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

### 18.6 ModelAttribute schema 类型说明

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

---

> **⚠️ 不要依赖 core 仓库内置前端集成：**
>
> 以下前端能力存在于 core 仓库的 `web/admin` 子模块中，尚不稳定，
> 业务项目**不应依赖**这些组件或集成方式：
>
> - **WayPage 组件**（`src/components/WayPlus/WayPage`）：自动渲染完整 CRUD 页面，集成规范待稳定。
> - **JWT 前端拦截器**（`requestErrorConfig.ts`）：依赖 `casdoor_token` 的 localStorage 方案待完善。
> - **Umi 路由配置**（`config/routes.ts`）：动态菜单路由 `/main/:s/:c` 方案待稳定。
> - **开发代理配置**（`config/proxy.ts`）：目标地址依项目而定，暂不作为通用示例。
>
> **推荐做法：**
> - 后端验证：使用 **TestToken + curl / fetch** 直接调用 API（见 18.4、18.5）。
> - 业务前端：自行实现 HTTP 请求（`axios` / `fetch`），按 18.1 解析 `ResultData` 响应格式；
>   不要直接引用或依赖 core 仓库中的前端代码。
