package public

import (
	"errors"
	"strconv"

	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/internal/menumetadata"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type QueryRouters struct {
	api.ServerArgs
	ApiType int  `json:"apiType"`
	ForMenu bool `json:"forMenu"`
}

func (own *QueryRouters) Parse(req types.IRequest) error {
	if httpReq, ok := req.(types.IRequestHttp); ok && httpReq.GetHttpRequest() != nil {
		if err := req.Bind(own); err != nil {
			return err
		}
	}
	ats := req.GetValue("apitype")
	if ats != "" {
		at, err := strconv.Atoi(ats)
		if err != nil {
			return err
		}
		own.ApiType = at
	}
	if value := req.GetValue("formenu"); value != "" {
		forMenu, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		own.ForMenu = forMenu
	}
	return nil
}

func (own *QueryRouters) Validation(req types.IRequest) error {
	if own.ForMenu {
		if callerReq, ok := req.(types.IRequestInternalCaller); ok {
			if _, trusted := callerReq.TrustedInternalCaller(); trusted {
				return nil
			}
		}
	}
	return own.ServerArgs.Validation(req)
}
func (own *QueryRouters) Do(req types.IRequest) (interface{}, error) {
	sc := router.GetContext(req.ServiceName())
	if sc == nil || sc.Router == nil {
		return nil, errors.New("query routers service context unavailable")
	}
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
	var routers []*types.RouterInfo
	if apitype == "" {
		routers = sc.Router.GetRouters()
	} else {
		routers = sc.Router.GetTypeRouters(types.ApiType(apitype))
	}
	if own.ForMenu {
		return NewMenuServiceSnapshot(sc), nil
	}
	return routers, nil
}

func (own *QueryRouters) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfoWithOptions(own, withSystemEndpointRateLimit())
}

// NewMenuServiceSnapshot 从目标服务自己的进程内注册表生成菜单发现快照。
func NewMenuServiceSnapshot(sc *router.ServiceContext) *smodels.MenuServiceSnapshot {
	if sc == nil {
		return menumetadata.New(nil, nil)
	}
	var routes []*types.RouterInfo
	if sc.Router != nil {
		routes = sc.Router.GetTypeRouters(types.ManageType)
	}
	return menumetadata.New(sc.Service, routes)
}
