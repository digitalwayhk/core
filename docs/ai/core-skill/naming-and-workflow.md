# 命名规范与开发设计流程

## 命名规范

遵循 Go 语言官方及业界通行规范（[Effective Go](https://go.dev/doc/effective_go) + [Google Go Style Guide](https://google.github.io/styleguide/go/guide)）。

### 通用命名规则

| 类型 | 规范 | 正确示例 | 错误示例 |
|------|------|----------|----------|
| 导出类型 / 函数 | `PascalCase` | `TradeModel`、`PlaceOrder` | `trade_model`、`Tr_Model` |
| 未导出类型 / 变量 | `camelCase` | `tradeAction`、`marketID` | `trade_action`、`TradeAction` |
| 接口 | 动词 / `er` 结尾 | `Retrier`、`MarketProvider` | `IRetry`、`IMarketProvider` |
| 常量 | `PascalCase`（业务） / `ALL_CAPS`（环境变量名字符串） | `FundStatusPending`、`envMySQLDSN` | `FUND_STATUS_PENDING` |
| 包名 | 小写单词，不用下划线 | `package manage`、`package button` | `package manage_api` |
| 文件名 | `snake_case.go` | `place_order.go`、`trade_model.go` | `PlaceOrder.go`、`tradeModel.go` |

### 缩写词规则

Go 规范：缩写词应全部大写或全部小写，不混用。

```go
// ✅ 正确
type UserID uint
type APIClient struct{}
func GetUserID() uint {}

// ❌ 错误
type UserId uint      // Id 应为 ID
type ApiClient struct {} // Api 应为 API
```

### 类型命名

```go
// ✅ Model：名词，描述业务实体，无前缀/下划线
type TradeOrder struct { *TradeBusinessModel } // 交易订单：业务事实支路
type DepositRecord struct { *FundBusinessModel } // 充值记录：业务事实支路
type MarketConfig struct { *TradeBaseDataModel } // 市场配置：基础资料支路

// ❌ 避免
type Tr_Order struct {}     // 下划线 + 缩写前缀不可读
type TrOrder struct {}      // 无意义缩写前缀
type OrderModel struct {}   // 结尾加 Model 冗余（包名已表达语境）

// ✅ ModelList：在 model 包内约定用 TypeNameList 命名
type TradeOrderList[T pt.IModel] struct { *entity.ModelList[T] }

// ✅ API handler：动词+名词，清晰表达动作
type PlaceOrder struct {}
type CancelOrder struct {}
type GetOrderBook struct {}

// ✅ Manage 控制器：业务名 + Manage
type DepositRecordManage struct {}
type TradeOrderManage struct {}

// ✅ Button：动词（操作语义）
type Retry[T pt.IModel] struct {}
type ForceSync[T pt.IModel] struct {}
type ExportData[T pt.IModel] struct {}
```

### 注释规范（godoc 格式）

```go
// TradeOrder 表示一笔已成交的交易订单。
// 状态由 State 字段控制：0=待处理 1=已提交 2=已完成。
// 不可删除；继承 TradeBusinessModel，业务唯一键由订单幂等契约决定。
type TradeOrder struct {
    *TradeBusinessModel
    MarketID uint            `json:"marketId"`
    Price  decimal.Decimal `json:"price"  desc:"成交价格"`
    Amount decimal.Decimal `json:"amount" desc:"成交数量"`
}

// PlaceOrder 提交新订单。
// 需要 marketId 作为必填参数；价格和数量均不得为零。
type PlaceOrder struct {
    MarketAPI                           // 嵌入服务公共基类
    Price  string `json:"price"`
    Amount string `json:"amount"`
}

// NewTradeOrderList 返回 trades 服务专属的 ModelList。
// 自动从环境变量 PROJ_MYSQL_DSN_TRADES 获取连接，降级到全局 DSN。
func NewTradeOrderList() *entity.ModelList[TradeOrder] { ... }
```

### 命名反例（参考 futures 项目，不要模仿）

```go
// ❌ 以下命名来自 futures 项目，不符合 Go 规范，不要模仿

type Tr_Model struct {}         // 下划线 + 缩写前缀
type Com_Model struct {}        // 下划线
type Com_ModelList[T] struct {} // 下划线
type TrApi struct {}            // 无意义缩写 + Api（应为 API）
type ComApi struct {}           // 同上

// ✅ 对应的正确写法；公共基座命名不能与基础资料分类混淆
type TradeServiceModel struct {}
type ProjectModel struct {}
type ProjectModelList[T] struct {}
type TradeAPI struct {}
type BaseAPI struct {}
```

---

## 开发设计流程

使用本框架开发一个业务模块时，按以下顺序进行，每步都为下一步提供输入。

### 第一步：设计 Model

先明确数据结构，再考虑 API。

```
models/
├── order.go          ← 核心业务实体
├── market.go         ← 依赖的关联实体
└── order_record.go   ← 操作记录（只写不改）
```

设计 model 时思考：
- 哪些字段是用户提交的？→ 这些字段的 Public/Private API
- 哪些字段需要管理员配置？→ 这些字段需要 Manage API
- 哪些字段有状态流转（State）？→ 需要 Submit/Release/自定义 Operation
- 哪些字段会重试/失败（RetryCount/Status）？→ 需要 Retry 按钮

### 第二步：设计 Public / Private API

实现用户端直接调用的接口。

```
api/public/
├── get_orders.go     ← 查询（GET，无需登录）
├── get_market.go
api/private/
├── place_order.go    ← 下单（POST，需要登录）
├── cancel_order.go
```

**审视结果：**
- 某字段只能通过管理员设置？→ 第三步设计 Manage API
- 某操作需要审批流？→ 第三步的 Submit/Release
- 某类记录需要人工重试？→ 第四步的 Retry 按钮

### 第三步：设计 Manage API

为管理后台设计 CRUD 控制器。每个需要管理的 model 对应一个 Manage 控制器。

```
api/manage/
├── market_config_manage.go    ← 市场配置（Add/Edit/Remove/Submit/Release）
├── order_manage.go            ← 订单查看（仅 View/Search，不可编辑）
└── deposit_record_manage.go   ← 充值记录（View/Search + Retry 按钮）
```

设计 Manage 控制器时：
- 纯查看记录 → 只暴露 `View + Search`
- 需要创建/修改 → 追加 `Add + Edit`
- 有生命周期状态 → 追加 `Submit + Release`
- 不可删除（BaseOrderModel）→ 不暴露 `Remove`

### 第四步：设计 Button（Operation）

当 Manage 页面需要非 CRUD 的自定义操作时，设计 Button。

**判断放在哪里：**

| 判断条件 | 放置位置 |
|----------|----------|
| 依赖特定 model 字段（如 `RetryCount`），任何包含该字段的 model 均可用 | `internal/pkg/api/manage/button/` |
| 仅该服务内部多个 Manage 复用 | `internal/core/{svc}/api/manage/button/` |
| 仅某一个 Manage 使用，无复用价值 | 内联到该 Manage 文件或 manage 同级文件 |

```
internal/
├── pkg/
│   └── api/
│       └── manage/
│           └── button/               ← 跨服务可复用按钮
│               ├── retry.go          ← 适用于所有含 RetryCount 字段的 model
│               ├── export_data.go    ← 通用数据导出
│               └── force_sync.go     ← 通用强制同步
└── core/
    └── {serviceName}/
        └── api/
            └── manage/
                ├── button/           ← 该服务内多处复用的按钮
                │   └── start_task.go
                └── order_manage.go
```

---


### 何时修改哪一层

| 需求 | 修改位置 | 影响范围 |
|------|----------|----------|
| 全项目所有模型新增公共字段（如审计字段） | `internal/pkg/models/project_model.go` | 全部服务所有模型 |
| 切换全项目默认数据库连接逻辑 | `internal/pkg/models/project_model.go` 的连接工厂 | 全部服务 |
| Manage 列表统一隐藏/展示某字段 | `internal/pkg/services/base_manage_service.go` 的 `ViewFieldModel` | 全部服务所有 Manage 页 |
| 全项目 handler 新增公共工具方法 | `internal/pkg/api/base_api.go` | 全部服务所有 handler |
| 某服务所有模型切换 DB 名称 | `internal/core/{svc}/models/trade_service_model.go` 的 `GetLocalDBName` | 该服务所有模型 |
| 基础资料统一启停/引用保护 | `models/basedata` + `BaseDataManage[T]` | 该服务基础资料 |
| 业务事实统一只读/状态机/分页上限 | `models/transaction` + `BusinessManage[T]` | 该服务业务事实 |
| 某服务所有接口新增公共请求字段 | `internal/core/{svc}/api/trade_api.go` | 该服务所有 handler |
| 某个具体接口的逻辑 | 具体 handler 文件 | 仅该接口 |

> **原则：** 能在上层解决的问题不下放到下层，越靠近根的改动影响面越大，
> 改之前确认所有继承方都适用该变更。

---

