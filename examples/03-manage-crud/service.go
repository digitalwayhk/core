package managecrud

import (
	"github.com/digitalwayhk/core/examples/03-manage-crud/api/manage"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// CatalogService 演示管理端 CRUD 服务。
type CatalogService struct{}

func (own *CatalogService) ServiceName() string { return "catalog" }

func (own *CatalogService) Routers() []types.IRouter {
	return manage.NewProductManage().Routers()
}

func (own *CatalogService) SubscribeRouters() []*types.ObserveArgs { return nil }
