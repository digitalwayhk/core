package types

import (
	"context"
	"errors"

	"github.com/zeromicro/go-zero/core/service"
)

// GRPCServerLifecycle 是 ServiceContext 管理 gRPC 服务端所需的完整生命周期契约。
// BeginShutdown 必须同步发布 NOT_SERVING，但不得等待在途 RPC 结束。
type GRPCServerLifecycle interface {
	service.Service
	Ready() <-chan struct{}
	Done() <-chan struct{}
	BeginShutdown()
	StopContext(context.Context) error
	Err() error
}

type Service struct {
	Name             string
	Routers          []IRouter `json:"-"`
	SubscribeRouters []*ObserveArgs
	AttachService    map[string]*ServiceAttach
	HttpServer       IRunServer  `json:"-"`
	Instance         interface{} `json:"-"`
}

func (own *Service) CallService(payload *PayLoad) ([]byte, error) {
	if payload.TargetService == "" {
		return nil, errors.New("target service is empty")
	}
	if payload.TargetAddress == "" {
		if as, ok := own.AttachService[payload.TargetService]; ok {
			payload.TargetAddress = as.Address
			payload.TargetPort = as.Port
		}
	}
	if payload.TargetAddress == "" {
		return nil, errors.New("target address is empty")
	}
	if payload.TargetPort == 0 {
		return nil, errors.New("target port is empty")
	}
	var err error
	var txt []byte
	txt, err = own.HttpServer.Send(payload)
	return txt, err
}

// ServiceAttach 附加引用的服务(通过订阅或CallService加载)
type ServiceAttach struct {
	service         *Service
	ServiceName     string
	ObserverRouters map[string]*ObserveArgs
	CallRouters     map[string]IRouter
	IsAttach        bool
	Address         string
	Port            int
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
