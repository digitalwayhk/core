# digitalway.hk Core — AI 开发指引

## 项目定位

这不是一个技术框架，而是一个**业务开发标准化方案**。它约束开发者用固定方式开发商业功能，实现业务与技术的分离：

- 业务代码只决定**做什么、何时做**（如：何时存储数据）
- 技术实现决定**怎么做**（如：存储到 MySQL 还是 MongoDB）
- 技术方案可独立替换、升级，不影响已实现的业务逻辑

## 技术栈

- **语言**: Go 1.26+
- **HTTP**: Go-Zero, Fiber
- **ORM**: GORM (MySQL, SQLite, ClickHouse, PostgreSQL)
- **存储**: BoltDB, Pebble, Badger, MongoDB
- **网络**: gRPC, QUIC, WebSocket (Melody + gorilla/websocket)
- **数值**: shopspring/decimal（所有金额/精度敏感字段）
- **验证**: go-playground/validator
- **测试**: testify, ginkgo, gomega
- **编码规范**: uber-go, golang-standards/project-layout

## 目录结构

```
pkg/         — 框架核心包 (server, persistence, dec, utils, localization, fileserver)
service/     — 服务层 (manage CRUD 等)
examples/    — 13 个编号示例 + demo 完整示例
web/admin/   — 前端管理后台 (git submodule)
docs/        — 文档 (codex/, copilot/ 为 AI 代理参考)
models/      — 项目数据模型
```

## 核心编码规范

### 1. API 处理器必须实现 IRouter 接口

每个 API 端点都实现以下四个方法：

```go
type IRouter interface {
    Parse(req types.IRequest) error           // 参数绑定
    Validation(req types.IRequest) error      // 校验/默认值
    Do(req types.IRequest) (interface{}, error) // 业务逻辑
    RouterInfo() *types.RouterInfo            // 路由信息
}
```

### 2. 路由路径规则

| 路由类型 | 路径模式 | 说明 |
|----------|----------|------|
| 公开路由 | `/api/{service}/{structNameLower}` | `api/public` 包，无需认证 |
| 私有路由 | `/api/{service}/{structNameLower}` | `api/private` 包，需 JWT 认证 |
| 管理路由 | `/api/manage/{service}/{manageLower}/{operationLower}` | `api/manage` 包，CRUD 操作 |
| 服务管理 | `/api/servermanage/{structNameLower}` | 框架注册时重写 service 段 |

> ⚠️ `public` 和 `private` 不在 URL 路径中，路径由包路径决定认证类型。

### 3. 模型规范

```go
// entity.Model — 无 Code/State 语义的简单记录
type OrderModel struct {
    *entity.Model
    UserID string
}
func NewOrderModel() *OrderModel {
    return &OrderModel{Model: entity.NewModel()}
}
func (own *OrderModel) NewModel() {
    if own.Model == nil { own.Model = entity.NewModel() }
}

// entity.BaseModel — 有 Code/Name/State 的记录
type TokenModel struct {
    *entity.BaseModel
    Price decimal.Decimal
}
func NewTokenModel() *TokenModel {
    return &TokenModel{BaseModel: entity.NewBaseModel()}
}
func (own *TokenModel) NewModel() {
    if own.BaseModel == nil { own.BaseModel = entity.NewBaseModel() }
}
```

**规则**:
- 每个嵌入 `*entity.Model` 或 `*entity.BaseModel` 的结构体必须实现 `NewModel()` 初始化方法
- `BaseModel.GetHash()` 对 `Code` 取哈希；无稳定 `Code` 的记录用 `entity.Model`
- `AddValid()` 要求 ID 非零且 Code 非空；`UpdateValid()` 要求 Code 非空；`RemoveValid()` 拒绝 State > 0 的记录

### 4. 持久化操作

```go
list := entity.NewModelList[models.OrderModel](nil)
item := list.NewItem()
list.Add(item)
list.Save()

// 查询
list.SearchId(id)                    // 按 ID
list.SearchWhere("UserID", userID)   // 按字段（默认最多 500 行）
list.SearchAll(page, size)           // 分页查询
list.Update(item)
list.Remove(item)
```

> ⚠️ `SearchWhere` 默认上限 500 行，需要更多时使用 `SearchAll` 或设置 `SearchItem.Size`。

### 5. 私有路由获取用户

```go
func (own *AddOrder) Validation(req types.IRequest) error {
    userID, _ := req.GetUser()  // 返回当前认证用户 ID
    // 不要信任客户端传的 userID
}
```

### 6. 服务注册

```go
type DemoService struct{}

func (own *DemoService) ServiceName() string { return "demo" }

func (own *DemoService) Routers() []types.IRouter {
    routers := make([]types.IRouter, 0)
    routers = append(routers, &public.GetOrder{})
    routers = append(routers, &private.AddOrder{})
    routers = append(routers, manage.NewTokenManage().Routers()...)
    return routers
}

func (own *DemoService) SubscribeRouters() []*types.ObserveArgs {
    return []*types.ObserveArgs{}
}
```

### 7. Manage CRUD

```go
type TokenManage struct {
    *managepkg.ManageService[models.TokenModel]
}

func NewTokenManage() *TokenManage {
    own := &TokenManage{}
    own.ManageService = managepkg.NewManageService[models.TokenModel](own)
    return own
}
```

自动生成的操作: View, Search, Add, Edit, Remove, Submit, Release。

自定义操作必须**值嵌入** `manage.Operation[T]`（不是指针嵌入）。

## 开发工作流

1. **先读后写** — 查看最近的 example 或同级 service 作为参考模板
2. **一模型一文件** — 每个 model 文件对应一张表，表操作放在 model 或 service 层
3. **参数绑定在 Parse** — 校验和默认值在 Validation，副作用在 Do
4. **验证** — `go test ./...` 或针对性 `go test ./service/manage/...`，编辑后运行 `gofmt`
5. **遵循已有模式** — 不要绕过项目已有的 DB 路由、用户解析、父级校验等封装

## 审查清单

提交代码前检查：
- [ ] URL 路径是否正确（public/private 不在 URL 中）
- [ ] 模型是否有 `NewModel()` 初始化
- [ ] `BaseModel` 是否有 Code 或重写 hash 行为
- [ ] `ManageService[T]` 是否用正确的实例创建（确保 hook 触发）
- [ ] 自定义 manage 操作是否用值嵌入 `manage.Operation[T]`
- [ ] 私有路由是否用 `req.GetUser()` 而非请求字段获取身份
- [ ] 是否调用了父级 Parse/Validation/hook 方法
- [ ] Router 是否已注册，WebSocket 订阅路径是否匹配实际路由

## 测试

```bash
# 针对性测试
go test ./service/manage/...
go test ./pkg/server/router/...
go test ./examples/01-hello-router/...
go test ./pkg/dec/example/...

# 全量测试
go test ./...
```

测试 Token 获取: `GET /api/servermanage/testtoken?userid={id}&type=0`
- type=0: 普通用户 token
- type=1: 管理 token
- type=2: 服务管理 token
