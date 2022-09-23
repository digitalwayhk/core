package types

import (
	"errors"

	"github.com/zeromicro/go-zero/core/service"
)

type Service struct {
	Name             string
	Routers          []IRouter `json:"-"`
	SubscribeRouters []*ObserveArgs
	AttachService    map[string]*ServiceAttach
	HttpServer       IRunServer   `json:"-"`
	internalServer   []IRunServer `json:"-"`
	Instance         interface{}  `json:"-"`
}

func (own *Service) CallService(payload *PayLoad) ([]byte, error) {

	if payload.TargetService == "" {
		return nil, errors.New("target service is empty")
	}
	if payload.TargetAddress == "" {
		if as, ok := own.AttachService[payload.TargetService]; ok {
			payload.TargetAddress = as.Address
			payload.TargetPort = as.Port
			payload.TargetSocketPort = as.SocketPort
		}
	}
	if payload.TargetAddress == "" {
		return nil, errors.New("target address is empty")
	}
	if payload.TargetPort == 0 && payload.TargetSocketPort == 0 {
		return nil, errors.New("target port is empty")
	}
	var err error
	var txt []byte
	if payload.TraceID != "" {
		if own.internalServer != nil && len(own.internalServer) > 0 {
			for _, server := range own.internalServer {
				txt, err = server.Send(payload)
				if err != nil {
					return nil, err
				}
				return txt, err
			}
		}
	}
	txt, err = own.HttpServer.Send(payload)
	return txt, err
}
func (own *Service) AddInternalServer(server IRunServer) {
	if own.internalServer == nil {
		own.internalServer = make([]IRunServer, 0)
	}
	own.internalServer = append(own.internalServer, server)
}
func (own *Service) GetInternalServers() []IRunServer {
	return own.internalServer
}

//ServiceAttach 附加引用的服务(通过订阅或CallService加载)
type ServiceAttach struct {
	service         *Service
	ServiceName     string
	ObserverRouters map[string]*ObserveArgs
	CallRouters     map[string]IRouter
	IsAttach        bool
	Address         string
	Port            int
	SocketPort      int
}

func NewServiceAttach(service *Service) *ServiceAttach {
	return &ServiceAttach{
		service: service,
	}
}

type IRunServer interface {
	service.Service
	RegisterHandlers(routers []*RouterInfo)
	Send(payload *PayLoad) ([]byte, error)
	GetIPandPort() (string, int)
}
type IAttachService interface {
	SetServiceAddress(name, address string, port int) error
}
