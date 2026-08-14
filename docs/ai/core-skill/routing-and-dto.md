# 路由契约、Public/Private 与 DTO

## 路由基础契约

### 无依赖服务契约

每个业务服务建立最底层 `contract` 包，供本服务各层和其他服务安全引用。该包不得导入其他包，不保存数据库模型、ServiceContext、RouterInfo、连接、请求或用户状态。

```go
package contract

const ServiceName = "shop"
```

`IService.ServiceName()` 返回这个唯一常量。服务名用于配置、ServiceContext 注册和内部服务本地/远程分流；不要在各 API 中重复字符串。

路由 Path 不放入 contract。任何 Router 都通过 `RouterInfo()` 提供 Path、Method、认证类型和服务归属。服务完成注册后，框架按路由类型身份返回 ServiceContext 持有并冻结的 RouterInfo；服务关闭时注销。相同 Router 类型如果同时归属多个 ServiceContext，无服务上下文的 `RouterInfo()` 调用会 fail closed，调用方必须从目标 ServiceContext 解析。

路由元数据必须在 `RouterInfo()` 构造表达式中通过 Option 一次声明，注册后只读：

```go
func (own *GetProducts) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(
		own,
		router.WithMethod(http.MethodGet),
	)
}
```

读取时使用 `GetPath()`、`GetMethod()`、`GetAuth()`、`GetServiceName()`、`GetPathType()` 等 Getter。当前导出的同名字段仅为旧消费方源码兼容保留，已废弃；新代码不得直接读写。后续破坏性版本会将这些冻结属性改为非导出字段，因此不要依赖字段赋值。Option 只在首次创建且尚未 Freeze 时执行；再次调用 `RouterInfo()` 返回已注册单例，不会重放 Option 或改写元数据。

内部调用可先判断目标服务是否位于当前进程：

```go
serviceName := contract.ServiceName
info := targetAPI.RouterInfo()
if target := router.GetContext(serviceName); target != nil {
	// 通过目标 ServiceContext 中的已注册 RouterInfo 走本地调用链。
} else {
	// 使用 serviceName、info.GetPath() 和服务发现结果走 Transport。
}
```

`GetContext(serviceName)==nil` 只表示目标服务不在当前进程，不表示远程节点一定存在。远程服务发现失败必须明确返回错误；不得缓存 ServiceContext 指针，也不得直接调用目标 API 的 `Do()` 绕过完整执行链。

所有普通 API 实现：

```go
type IRouter interface {
	Parse(req types.IRequest) error
	Validation(req types.IRequest) error
	Do(req types.IRequest) (interface{}, error)
	RouterInfo() *types.RouterInfo
}
```

职责：

- `Parse`：绑定 JSON/query，不执行业务副作用。
- `Validation`：校验身份、参数和调用前条件，不写数据库。
- `Do`：查询事实数据并执行业务副作用。
- `RouterInfo`：无自定义元数据时使用 `router.DefaultRouterInfo(own)`；需要覆盖 Method、Path、Auth、PathType、PoolSize 时使用 `router.DefaultRouterInfoWithOptions(own, options...)`。旧构造函数保留精确签名，保证函数值和既有消费方兼容。

路径：

```text
public/private: /api/{service}/{structLower}
manage:         /api/manage/{service}/{manageLower}/{operationLower}
server manage:  /api/servermanage/{structLower}
```

`api/public` 与 `api/private` 只决定认证策略，不进入 URL。private 身份只能来自：

```go
userID, userName := req.GetUser()
```

禁止从 body/query 的 UserID 推断认证身份，也不要把当前请求、用户、trace 或响应保存在 `RouterInfo`、ServiceContext 或其他共享对象中。

## DTO 与响应契约

public/private API 返回独立 `api/dto` 类型，不直接序列化持久化模型。原因包括：

- 持久化模型可能具有很深的嵌入关系和内部字段。
- 对外字段、名称和时间格式需要稳定，不应随数据库模型重构漂移。
- HTTP、OpenAPI 与 WebSocket 可以复用同一份公开结构。

标准样例使用：

- `dto.ProductResponse`：只暴露 ID、名称和价格。
- `dto.OrderResponse`：暴露订单快照、数量、用户和创建时间。
- `OrderResponse.Action`：HTTP 响应为空；WebSocket 事件复制 DTO 后设置 `created` 或 `deleted`。

普通 API 实现 `IRouterResponse`，让 OpenAPI 在不执行路由的情况下获得响应结构：

```go
func (own *GetProducts) GetResponse() interface{} {
	return []*dto.ProductResponse{}
}
```

DTO 转换集中放在 `api/dto`，不要放入通用集成测试 helpers，也不要让测试 DTO 进入生产包。

## Public API

Public API 无需身份，但仍执行参数解析、校验、类型化错误和 DTO 转换。

`GetProducts` 展示标准可选筛选模式：

- `id` 为空时不按 ID 限制；有值时精确匹配。
- `name` 为空时不按名称限制；有值时模糊匹配。
- 两者同时存在时组合筛选。
- 条件全部为空时返回全部可下单商品。
- 非法 ID 返回稳定的公开校验错误。

不要为了 public 查询复用 Manage 的列表请求/响应结构；它们面向不同调用方和兼容性契约。

## Private API

### 创建订单

下单只接收 `productID` 和 `quantity`。UserID 从 `req.GetUser()` 获取，商品名称和价格从数据库中的当前商品读取。订单保存商品 ID、名称和价格快照，因此商品后来改名或改价不会改变历史订单。

标准顺序：

1. `Parse` 绑定商品 ID 与数量。
2. `Validation` 验证可信身份、商品 ID 和正数数量。
3. `Do` 查询商品事实数据。
4. 创建订单并设置框架 ID、UserID、商品快照和秒级 CreatedAt。
5. 持久化成功后才发布 `created` WebSocket 通知。
6. HTTP 返回不带 `action` 的订单 DTO。

### 查询本人订单

`GetOrders` 不接受 UserID 参数，只按可信身份调用 `QueryByUser`。响应使用订单 DTO，不能返回其他用户订单。

### 删除本人订单

删除先以 `ID + UserID` 查询所有权，再物理删除。不存在与不属于当前用户返回同一公开错误，避免泄露其他用户订单是否存在。持久化成功后发布 `deleted` 通知。

