package types

type IServerRouter interface {
	AddRoutes(routers ...IRouter)
	AddServerManagerRouters(routers ...IRouter)
	GetRouterInfos() []IRouterInfo
	GetRouterInfo(path string) IRouterInfo
	GetTypeRouterInfos(t ApiType) []IRouterInfo
	HasTypeRouter(path string, t ApiType) bool
	HasRouter(path string) bool
	RouterExecuteHandler(path string) (interface{}, error)
}
