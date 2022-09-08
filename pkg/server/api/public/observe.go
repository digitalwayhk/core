package public

import (
	"errors"

	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

type Observe struct {
	*types.ObserveArgs
}

func (own *Observe) Parse(req types.IRequest) error {
	args := &types.ObserveArgs{}
	err := req.Bind(args)
	if err != nil {
		return err
	}
	own.ObserveArgs = args
	return nil
}
func (own *Observe) Validation(req types.IRequest) error {
	if !req.Authorized() {
		if !utils.HasLocalIPAddr(req.GetClientIP()) {
			return errors.New("Observe接口只能在本地机访问!")
		}
	}
	return nil
}
func (own *Observe) Do(req types.IRequest) (interface{}, error) {
	sc := router.GetContext(req.ServiceName())
	info := sc.Router.GetRouter(own.Topic)
	if info == nil {
		return nil, errors.New("没有找到该topic的路由信息!" + own.Topic)
	}
	own.CallBack = func(args *types.NotifyArgs) error {
		notify := &Notify{}
		return router.SendNotify(notify, args)
	}
	own.ObserveArgs.IsOk = true
	err := info.Subscribe(own.ObserveArgs)
	return info, err
}

func (own *Observe) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfo(own)
}
