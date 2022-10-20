package types

// IRequest
type IRequest interface {
	GetTraceId() string
	GetUser() (uint, string)                                                        //获取用户ID和name，获取设置在token中的userid和name，name可能为空
	GetClientIP() string                                                            //获取客户端IP
	NewID() uint                                                                    //生成新的ID
	Authorized() bool                                                               //该请求是否授权路由
	CallService(router IRouter, callback ...func(res IResponse)) (IResponse, error) //调用其它服务,当callback为空时，表示该请求是同步请求，否则是异步请求，当有1个callback，网络异常请求未完成不会回调，第2个callback是网络异常时的回调(已经尝试了重试方案依然异常时)
	CallTargetService(router IRouter, info *ServerInfo, callback ...func(res IResponse)) (IResponse, error)
	GetValue(key string) string               //获取请求参数
	Bind(v interface{}) error                 //Json绑定传输数据
	GoZeroBind(v interface{}) error           //GoZero方式绑定传输数据,用于支持GoZero的标签
	NewResponse(interface{}, error) IResponse //创建一个响应
	GetPath() string                          //获取当前请求路径
	GetClaims(key string) interface{}         //获取用户自定义在token中的信息
	ServiceName() string                      //获取服务名称
}

// IRequestClear
type IRequestClear interface {
	ClearTraceId()
	SetPath(path string)
}
