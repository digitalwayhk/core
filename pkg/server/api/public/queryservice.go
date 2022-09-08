package public

import (
	"errors"
	"fmt"

	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type QueryService struct {
	api.ServerArgs
	Name string
}

type ServiceData struct {
	*types.Service
	ViewManage string `json:"viewmanage"`
}

func (own *QueryService) Parse(req types.IRequest) error {
	own.Name = req.GetValue("name")
	return nil
}
func (own *QueryService) Do(req types.IRequest) (interface{}, error) {
	items := make([]*ServiceData, 0)
	qr := &QueryRouters{}
	info := qr.RouterInfo()
	path := info.Path + "/%s" + "?apitype=3"
	if own.Name != "" {
		sc := router.GetContext(own.Name)
		if sc == nil {
			return nil, errors.New(own.Name + "服务不存在！")
		}
		items = append(items, toData(sc, path))
	} else {
		for _, sr := range router.GetContexts() {
			items = append(items, toData(sr, path))
		}
	}
	return items, nil
}

func (own *QueryService) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfo(own)
}
func toData(sr *router.ServiceContext, path string) *ServiceData {
	return &ServiceData{
		Service:    sr.Service,
		ViewManage: fmt.Sprintf(path, sr.Service.Name),
	}
}
