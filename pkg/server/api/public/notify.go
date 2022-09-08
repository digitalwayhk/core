package public

import (
	"errors"

	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

type Notify struct {
	*types.NotifyArgs
}

func (own *Notify) Parse(req types.IRequest) error {
	notify := &types.NotifyArgs{}
	req.Bind(notify)
	own.NotifyArgs = notify
	return nil
}
func (own *Notify) Validation(req types.IRequest) error {
	if !utils.HasLocalIPAddr(req.GetClientIP()) {
		if !req.Authorized() {
			return errors.New("Notify接口只能在本地机访问!")
		}
	}
	return nil
}
func (own *Notify) Do(req types.IRequest) (interface{}, error) {
	sc := router.GetContext(own.ReceiveService)
	if sc == nil {
		return nil, errors.New("没有找到该服务的注册信息!" + own.ReceiveService)
	}
	receive := sc.Service.AttachService[own.SendService]
	if receive == nil {
		return nil, errors.New("没有找到合并中的发送服务：" + own.SendService)
	}
	if topic, ok := receive.ObserverRouters[own.Topic]; ok {
		if topic.CallBack != nil {
			err := topic.CallBack(own.NotifyArgs)
			return nil, err
		}
	} else {
		return nil, errors.New(own.ReceiveService + "服务中未监听" + own.SendService + "服务的" + own.Topic + "路由!")
	}
	return nil, nil
}

func (own *Notify) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfo(own)
}
