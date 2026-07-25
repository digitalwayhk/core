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
	Name           string
	Routers        []IRouter    `json:"-"`
	HttpServer     IRunServer   `json:"-"`
	internalServer []IRunServer `json:"-"`
	Instance       interface{}  `json:"-"`
}

func (own *Service) CallService(payload *PayLoad) ([]byte, error) {
	if payload.TargetService == "" {
		return nil, errors.New("target service is empty")
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

// AddInternalServer 注册由 ServiceContext 统一管理的协议扩展服务。
// 自定义实现必须遵守 IRunServer 生命周期；该入口不代表已删除的 Socket 传输。
func (own *Service) AddInternalServer(server IRunServer) {
	if own.internalServer == nil {
		own.internalServer = make([]IRunServer, 0)
	}
	own.internalServer = append(own.internalServer, server)
}

// GetInternalServers 返回已注册的协议扩展服务。
func (own *Service) GetInternalServers() []IRunServer {
	return own.internalServer
}

type IRunServer interface {
	service.Service
	RegisterHandlers(routers []*RouterInfo)
	Send(payload *PayLoad) ([]byte, error)
	GetIPandPort() (string, int)
}
