package types

//IServer
type IServer interface {
	NewID() uint
	SendNotify(args *NotifyArgs) error
}
type IService interface {
	ServiceName() string              //服务名称
	Routers() []IRouter               //服务中的业务路由，用于发布api服务
	SubscribeRouters() []*ObserveArgs //服务中订阅的路由，用于订阅其他服务的api服务
}

//关闭服务中的servermanage路由
type ICloseServerManage interface {
	IsCloseServerManage() bool
}
type IStartService interface {
	Start() //启动服务完成时调用
}
type IStopService interface {
	Stop() //停止服务完成时调用
}

//IRequest
type IRequest interface {
	GetTraceId() string
	GetUser() (uint, string)                                                        //获取用户ID和name，获取设置在token中的userid和name，name可能为空
	GetClientIP() string                                                            //获取客户端IP
	NewID() uint                                                                    //生成新的ID
	Authorized() bool                                                               //该请求是否授权路由
	CallService(router IRouter, callback ...func(res IResponse)) (IResponse, error) //调用其它服务,当callback为空时，表示该请求是同步请求，否则是异步请求，当有1个callback，网络异常请求未完成不会回调，第2个callback是网络异常时的回调(已经尝试了重试方案依然异常时)
	GetValue(key string) string                                                     //获取请求参数
	Bind(v interface{}) error                                                       //Json绑定传输数据
	GoZeroBind(v interface{}) error                                                 //GoZero方式绑定传输数据,用于支持GoZero的标签
	NewResponse(interface{}, error) IResponse                                       //创建一个响应
	GetPath() string                                                                //获取当前请求路径
	GetClaims(key string) interface{}                                               //获取用户自定义在token中的信息
	ServiceName() string                                                            //获取服务名称
}
type IRequestClear interface {
	ClearTraceId()
	SetPath(path string)
}

//IResponse
type IResponse interface {
	GetSuccess() bool                                //是否成功
	GetMessage() string                              //获取消息
	GetData(instanceType ...interface{}) interface{} //获取数据,参数为实例类型，如果为空，则返回map[string]interface{}
	GetError() error                                 //获取错误
}

//IRouter 标准业务路由接口，所有业务功能应通过该接口提供对外调用服务
type IRouter interface {
	Parse(req IRequest) error             //解析业务参数
	Validation(req IRequest) error        //验证业务允许调用,该方法返回nil，Do方法将被调用
	Do(req IRequest) (interface{}, error) //执行业务逻辑
	RouterInfo() *RouterInfo              //路由注册信息
}

//IRouterFactory 业务路由工厂接口，用于创建业务路由实例
type IRouterFactory interface {
	New(instance interface{}) IRouter //创建业务路由实例
}

//IPackRouterHook 包装路由获取实例接口
type IPackRouterHook interface {
	GetInstance() interface{}
}

//IWebSocket 客户端WebSocket接口
type IWebSocket interface {
	Send(event, channel string, data interface{})
	IsClosed() bool
}

type ITypeInfo interface {
	//返回类型的code和name
	GetInfo() (string, string)
}
