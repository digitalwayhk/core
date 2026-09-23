package adminui

import (
	"github.com/digitalwayhk/core/examples/08-admin-manage-ui/api/manage"
	publicapi "github.com/digitalwayhk/core/examples/08-admin-manage-ui/api/public"
	"github.com/digitalwayhk/core/examples/08-admin-manage-ui/contract"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// CatalogService 组装资料目录的管理路由和公开分类查询。
type CatalogService struct{}

// ServiceName 返回配置和路由共同使用的稳定服务名。
func (*CatalogService) ServiceName() string { return contract.ServiceName }

// GetTitle 返回管理后台目录的默认中文标题。
func (*CatalogService) GetTitle() string { return "资料目录" }

// GetLocaleTitle 返回管理后台目录的中英标题。
func (*CatalogService) GetLocaleTitle(locale string) string {
	if locale == "en-US" {
		return "Catalog"
	}
	return "资料目录"
}

// Routers 返回本示例全部路由。
func (*CatalogService) Routers() []types.IRouter {
	routers := make([]types.IRouter, 0, 20)
	routers = append(routers, manage.NewCategoryManage().Routers()...)
	routers = append(routers, manage.NewCatalogItemManage().Routers()...)
	routers = append(routers, &publicapi.GetCategories{})
	return routers
}
