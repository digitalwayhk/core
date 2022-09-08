package public

import (
	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/router"

	"github.com/digitalwayhk/core/pkg/server/types"
)

type Attach struct {
	api.ServerArgs
}

func (own *Attach) Do(req types.IRequest) (interface{}, error) {
	sc := router.GetContext(req.ServiceName())
	return sc.Service.AttachService, nil
}

func (own *Attach) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfo(own)
}
