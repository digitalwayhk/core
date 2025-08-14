package types

// IRouter 标准业务路由接口，所有业务功能应通过该接口提供对外调用服务
type IRouter interface {
	Parse(req IRequest) error             //解析业务参数
	Validation(req IRequest) error        //验证业务允许调用,该方法返回nil，Do方法将被调用
	Do(req IRequest) (interface{}, error) //执行业务逻辑
	RouterInfo() *RouterInfo              //路由注册信息
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

// IRouterFactory 业务路由工厂接口，用于创建管理路由实例
type IRouterFactory interface {
	New(instance interface{}) IRouter //创建业务路由实例
}

// IPackRouterHook 包装路由获取管理路由实例接口
type IPackRouterHook interface {
	GetInstance() interface{}
}

type IWebSocketRouter interface {
	RegisterWebSocket(client IWebSocket, req IRequest)
	UnRegisterWebSocket(client IWebSocket, req IRequest)
}

type IWebSocketRouterNotice interface {
	NoticeFiltersRouter(message interface{}, api IRouter) (bool, interface{})
}

type IRouterHashKey interface {
	GetHashKey() int
}
