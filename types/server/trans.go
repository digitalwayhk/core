package types

type ITransServer interface {
	Start()
	Stop()
	RegisterHandlers(router IServerRouter)
}
type ITransClient interface {
	Send(targetServer *ServerInfo, data []byte) ([]byte, error)
}
