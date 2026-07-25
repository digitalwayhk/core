package inheritanceshop

import (
	"github.com/digitalwayhk/core/examples/03-shop-inheritance/api/manage"
	privateapi "github.com/digitalwayhk/core/examples/03-shop-inheritance/api/private"
	publicapi "github.com/digitalwayhk/core/examples/03-shop-inheritance/api/public"
	"github.com/digitalwayhk/core/examples/03-shop-inheritance/contract"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// ShopService 组装模型与 Manage 继承能力完整示例。
type ShopService struct{}

// ServiceName 返回继承商城的稳定服务名。
func (own *ShopService) ServiceName() string { return contract.ServiceName }

// Routers 返回继承商城的 Manage、Public 和 Private 路由。
func (own *ShopService) Routers() []types.IRouter {
	routers := make([]types.IRouter, 0, 36)
	routers = append(routers, manage.NewProductManage().Routers()...)
	routers = append(routers, manage.NewSupplierManage().Routers()...)
	routers = append(routers, manage.NewPaymentTypeManage().Routers()...)
	routers = append(routers, manage.NewOrderManage().Routers()...)
	routers = append(routers, manage.NewPaymentRecordManage().Routers()...)
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
