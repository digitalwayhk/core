package types

// IRouter 是 Digitalway Core 的请求级业务路由契约。
//
// RouterInfo 是路由注册期创建、由 ServiceContext 持有的长期元数据；IRouter
// 实例则只服务于一次请求或一次明确的 WebSocket 订阅。普通请求从 RouterInfo
// 的有界对象池取得实例，依次调用 Parse、Validation 和 Do，并在所有同步使用
// 以及事件快照完成后归还对象池。WebSocket 订阅使用独立实例，由 Hub 持有到退订、
// 断线或关闭，随后执行 Clean 并丢弃，不进入请求对象池。因此实现不得把当前请求、
// 用户、trace 或响应写入包级变量、RouterInfo 或其他跨请求共享对象。
//
// 基础实现只需要实现本接口。按需还可以实现以下可选接口：
//   - IRouterFactory：自定义请求实例的创建方式，常用于泛型 Manage Router。
//   - IRouterResettable：覆盖对象池默认反射重置，适合包含复杂内部状态的 Router。
//   - IRouterCleanable：归还对象池前清理敏感数据或请求级引用。
//   - IRouterHashKey：提供稳定的参数 hash，供当前缓存和 WebSocket 订阅使用。
//   - IWebSocketRouter：接收 WebSocket 订阅注册与注销回调。
//   - IWebSocketRouterNotice：过滤并转换发送给订阅者的 WebSocket 通知。
//   - IRouterResponse：为 OpenAPI 等描述场景提供响应示例。
//   - IPackRouterHook：让包装 Router 暴露用于推导包路径和类型的真实实例。
//
// router 包还会识别其 GetRouterPath 可选接口，用于覆盖默认路由前缀。
type IRouter interface {
	Parse(req IRequest) error             // 将请求绑定到当前请求级实例。
	Validation(req IRequest) error        // 校验业务调用条件；返回 nil 后才执行 Do。
	Do(req IRequest) (interface{}, error) // 执行业务逻辑，不负责传输层响应写入。
	RouterInfo() *RouterInfo              // 返回所属服务内该路由唯一的长期元数据。
}

// IRouterResponse 为 OpenAPI、管理界面等描述场景提供路由成功响应示例。
// 它不替代 IRequest.NewResponse，也不参与运行时错误响应构造。
type IRouterResponse interface {
	GetResponse() interface{}
}

// IRouterInfo 路由信息用于管理IRouter,IRouterInfo是IRouter的元数据
type IRouterInfo interface {
	New() IRouter                                   //创建IRouter空实例
	ParseNew(instance interface{}) (IRouter, error) //解析实例参数创建IRouter实例
	JsonNew(json string) (IRouter, error)           //解析josn参数创建IRouter实例
	Exec(req IRequest) IResponse                    //执行IRouter实例
	ExecDo(api IRouter, req IRequest) IResponse     //执行IRouter实例不执行Parse
	GetPath() string                                //获取路由路径
	GetServiceName() string                         //获取服务名称
	SetServiceName(name string)                     //设置服务名称
	GetPathType() ApiType                           //获取路由类型
}

// IRouterFactory 覆盖默认反射创建逻辑。
//
// 常用于泛型或包装 Router。New 必须返回一个独立、可执行的 IRouter，不得返回
// 正在被其他请求或订阅使用的共享实例。普通请求实例由 RouterInfo 对象池管理；
// WebSocket 订阅实例由 RouteWebSocketHub 独占并在订阅结束后清理丢弃。
type IRouterFactory interface {
	New(instance interface{}) IRouter
}

// IPackRouterHook 让泛型或包装 Router 暴露真实业务实例。
// 框架使用返回值推导包路径、结构名和默认路由，不会用它替代请求级 Router。
type IPackRouterHook interface {
	GetInstance() interface{}
}

// IWebSocketRouter 接收本节点外部客户端 WebSocket 订阅组的生命周期回调。
// 该接口不是内部服务通信协议；服务间调用应使用 TransportSelector，
// 内部事件和跨节点控制应使用 ServiceEventBridge/MQ。
// RegisterWebSocket 和 UnRegisterWebSocket 可能由并发连接触发，实现必须线程安全，
// WebSocket 订阅 Router 在订阅存续期内不会进入请求对象池；回调仍不得把 IRequest、
// Router 或其可变字段泄漏给订阅生命周期之外的 goroutine。
type IWebSocketRouter interface {
	RegisterWebSocket(client IWebSocket, req IRequest)
	UnRegisterWebSocket(client IWebSocket, req IRequest)
}

// IWebSocketUserIdentity 接收已通过认证的 WebSocket 会话身份。
// 身份只能由传输层注入，Router 不得从订阅 payload 读取用户字段。
// 所有需要认证的 WebSocket 订阅 Router 都必须实现本接口，否则框架拒绝订阅。
type IWebSocketUserIdentity interface {
	GetUserID() string
	SetUserID(userID, userName string)
}

// IWebSocketRouterNotice 过滤并转换发送给外部订阅者的 WebSocket 内容。
// 返回 false 表示当前订阅不接收该消息；返回数据应视为不可变发送快照。
// 实现不得修改共享 RouterInfo，也不应执行无界阻塞操作。
type IWebSocketRouterNotice interface {
	NoticeFiltersRouter(message interface{}, api IRouter) (bool, interface{})
}

// IRouterHashKey 为路由参数提供稳定 hash。
//
// 当前实现将它用于 WebSocket 订阅分组，并在未实现 IRouterCacheKey 时作为缓存键
// 的兼容回退。相同业务参数必须在进程生命周期内返回相同值，并包含隔离订阅所需的
// 全部业务维度；不得使用随机数、指针地址或随时间变化的数据。
type IRouterHashKey interface {
	GetHashKey() uint64
}

// IRouterCacheKey 为路由结果缓存提供稳定、无歧义的业务键。
// 多租户或用户隔离维度必须由实现显式纳入返回值。
type IRouterCacheKey interface {
	GetCacheKey() string
}
