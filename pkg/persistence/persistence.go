package persistence

import (
	"github.com/digitalwayhk/core/pkg/persistence/api/manage"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type PersistenceService struct {
}

func (own *PersistenceService) ServiceName() string {
	return "persistence"
}

func (own *PersistenceService) Routers() []types.IRouter {
	items := make([]types.IRouter, 0)
	dbc := manage.NewRemoteDBManage()
	items = append(items, dbc.Routers()...)
	return items
}
func (own *PersistenceService) SubscribeRouters() []*types.ObserveArgs {
	return []*types.ObserveArgs{}
}
func (own *PersistenceService) IsCloseServerManage() bool {
	return true
}
