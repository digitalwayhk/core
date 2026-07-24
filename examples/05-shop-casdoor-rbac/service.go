package casdoorrbacshop

import (
	"sync"

	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/api/manage"
	privateapi "github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/api/private"
	publicapi "github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/api/public"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/business"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/contract"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// ShopService 组装模型继承、双域认证和三类认证 Hook。
type ShopService struct {
	identityEventsOnce sync.Once
	identityEvents     *business.IdentityEventService
}

// ServiceName 返回继承商城的稳定服务名。
func (own *ShopService) ServiceName() string { return contract.ServiceName }

// Routers 返回继承商城的 Manage、Public 和 Private 路由。
func (own *ShopService) Routers() []types.IRouter {
	routers := make([]types.IRouter, 0, 38)
	routers = append(routers, manage.NewProductManage().Routers()...)
	routers = append(routers, manage.NewSupplierManage().Routers()...)
	routers = append(routers, manage.NewPaymentTypeManage().Routers()...)
	routers = append(routers, manage.NewOrderManage().Routers()...)
	routers = append(routers, manage.NewPaymentRecordManage().Routers()...)
	routers = append(routers, manage.NewIdentityEventManage().Routers()...)
	routers = append(routers,
		&publicapi.GetProducts{},
		&publicapi.GetSuppliers{},
		&publicapi.GetPaymentTypes{},
		&privateapi.AddOrder{},
		&privateapi.GetOrders{},
		&privateapi.DeleteOrder{},
		&privateapi.CreatePayment{},
		&privateapi.CancelOrder{},
	)
	return routers
}
