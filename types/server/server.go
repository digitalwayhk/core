package types

import "io/fs"

type ServerOption struct {
	IsWebSocket bool         //是否启用websocket
	IsCors      bool         //是否开启跨域
	OriginCors  []string     //支持的跨域域名
	Demo        *DemoOption  //静态前端演示包
	Trans       *TransOption //内部服务间传输方案
	Quic        *QuicOption  //quic配置-http3 upd + tls
}
type TransOption struct {
	IsRest     bool //是否启用默认失败后的rest传输
	RetryCount int  //重试次数
}
type DemoOption struct {
	Pattern string //路由前缀
	File    fs.FS  //静态文件目录
}
type QuicOption struct {
	IsQuic   bool   //是否启用quic
	CertFile string //证书文件
	KeyFile  string //私钥文件
}
type IService interface {
	ServiceName() string              //服务名称
	Routers() []IRouter               //服务中的业务路由，用于发布api服务
	SubscribeRouters() []*ObserveArgs //服务中订阅的路由，用于订阅其他服务的api服务
}
type IStartService interface {
	Start() //启动服务完成时调用
}
type IStopService interface {
	Stop() //停止服务完成时调用
}

// IApplicationServer
type IApplicationServer interface {
	AddIService(service IService, option ...*ServerOption)
	Start()
	Stop()
	GetServerContexts() []IServerContext
	GetServerContext(name string) IServerContext
}
