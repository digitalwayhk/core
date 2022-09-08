package public

import (
	"strconv"

	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type QueryRouters struct {
	api.ServerArgs
	ApiType int
}

func (own *QueryRouters) Parse(req types.IRequest) error {
	ats := req.GetValue("apitype")
	if ats != "" {
		at, err := strconv.Atoi(ats)
		if err != nil {
			return err
		}
		own.ApiType = at
	}
	return nil
}
func (own *QueryRouters) Do(req types.IRequest) (interface{}, error) {
	sc := router.GetContext(req.ServiceName())
	apitype := ""
	if own.ApiType == 1 {
		apitype = string(types.PublicType)
	}
	if own.ApiType == 2 {
		apitype = string(types.PrivateType)
	}
	if own.ApiType == 3 {
		apitype = string(types.ManageType)
	}
	if apitype == "" {
		return sc.Router.GetRouters(), nil
	} else {
		return sc.Router.GetTypeRouters(types.ApiType(apitype)), nil
	}
}

func (own *QueryRouters) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfo(own)
}
