package server

import (
	pm "github.com/digitalwayhk/core/pkg/persistence/api/manage"
	"github.com/digitalwayhk/core/pkg/server/api/manage"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type SystemManage struct {
}

func (own *SystemManage) ServiceName() string {
	return "server"
}

func (own *SystemManage) Routers() []types.IRouter {
	items := make([]types.IRouter, 0)
	sm := manage.NewServiceManage()
	items = append(items, sm.Routers()...)
	//演示发布其他服务中的路由在本服务提供
	dbc := pm.NewRemoteDBManage()
	items = append(items, dbc.Routers()...)

	items = append(items, manage.NewDirectoryManage().Routers()...)
	items = append(items, manage.NewMenuManage().Routers()...)
	return items
}
func (own *SystemManage) SubscribeRouters() []*types.ObserveArgs {
	return []*types.ObserveArgs{}
}
func (own *SystemManage) IsCloseServerManage() bool {
	return true
}
