package private

import (
	"errors"
	"strconv"

	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/api/public"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type SetServiceAddress struct {
	api.ServerArgs
	ServiceName   string
	ServerAddress string
	ServicePort   int
}

func (own *SetServiceAddress) Parse(req types.IRequest) error {
	own.ServiceName = req.GetValue("name")
	own.ServerAddress = req.GetValue("address")
	own.ServicePort, _ = strconv.Atoi(req.GetValue("port"))
	return nil
}
func (own *SetServiceAddress) Validation(req types.IRequest) error {
	err := own.ServerArgs.Validation(req)
	if err != nil {
		return err
	}
	if own.ServiceName == "" {
		return errors.New("服务名称不能为空！")
	}
	if own.ServerAddress == "" {
		return errors.New("服务地址不能为空！")
	}
	if own.ServicePort == 0 {
		return errors.New("服务端口不能为0！")
	}
	return nil
}
func (own *SetServiceAddress) Do(req types.IRequest) (interface{}, error) {
	qc := &public.QueryConfig{}
	con, err := qc.GetConfigPath(own.ServerAddress, own.ServicePort)
	if err != nil {
		return nil, err
	}
	sc := router.GetContext(req.ServiceName())
	sc.Config.SetAttachService(own.ServiceName, own.ServerAddress, own.ServicePort, con.SocketPort)
	err = sc.Config.Save()
	if err != nil {
		return nil, err
	}
	sc.SetAttachServiceAddress(own.ServiceName)
	err = sc.RegisterObserve(&public.Observe{})
	if err != nil {
		return nil, err
	}
	sc.Service.AttachService[own.ServiceName].IsAttach = true
	return sc.Service.AttachService, err
}

func (own *SetServiceAddress) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfo(own)
}
