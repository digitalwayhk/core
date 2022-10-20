package types

type ObserveState int

const (
	//Do方法执行前，并验证通过
	ObserveRequest ObserveState = iota
	//Do方法执行完成,获取到Response
	ObserveResponse
	//Do方法执行,但是发生错误
	ObserveError
)

type ObserveArgs struct {
	Topic                string                       //订阅路由路径
	OwnAddress           string                       //订阅者地址
	OwnProt              int                          //订阅者端口
	OwnSocketProt        int                          //订阅者socket协议端口
	State                ObserveState                 //触发时机，0：接收到请求时，1：请求完成时，2：异常发生时
	ObserveTergetService string                       //订阅目标服务名称
	ReceiveService       string                       //接收者服务名称（订阅注册时，订阅者服务名称）
	CallBack             func(args *NotifyArgs) error `json:"-"` //回调函数
	Router               IRouter                      `json:"-"` //订阅路由
	IsOk                 bool                         //是否注册成功
	Error                error                        `json:"-"` //注册错误信息
	IsUnSub              bool                         `json:"-"` //是否取消订阅
}

// NewObserveArgs 创建对路由的订阅,router为订阅路由,state为订阅的触发时机，callback为回调函数
func NewObserveArgs(router IRouter, state ObserveState, callBack func(args *NotifyArgs) error) *ObserveArgs {
	info := router.RouterInfo()
	return &ObserveArgs{
		Topic:                info.GetPath(),
		ObserveTergetService: info.GetServiceName(),
		Router:               router,
		State:                state,
		CallBack:             callBack,
	}
}

type NotifyArgs struct {
	TraceID     string //跟踪ID
	SendService string
	Receive     *ServerInfo //接收服务器信息
	Topic       string
	Instance    interface{}
	Response    interface{}
	State       ObserveState //触发时机，0：接收到请求时，1：请求完成时，2：异常发生时
}

func (own *ObserveArgs) Notify(args *NotifyArgs) error {
	if own.CallBack != nil {
		return own.CallBack(args)
	}
	return nil
}

func (own *ObserveArgs) NewNotifyArgs(instance interface{}, resp IResponse) *NotifyArgs {
	args := &NotifyArgs{
		Receive: &ServerInfo{
			Address:    own.OwnAddress,
			Service:    own.ReceiveService,
			Port:       own.OwnProt,
			SocketPort: own.OwnSocketProt,
		},
		SendService: own.ObserveTergetService,
		Topic:       own.Topic,
		State:       own.State,
		Instance:    instance,
		Response:    resp,
	}
	return args
}
