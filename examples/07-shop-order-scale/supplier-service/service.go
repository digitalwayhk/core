// Package supplierservice 组装 07 供应商服务路由和生命周期能力。
package supplierservice

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	manageapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/api/manage"
	publicapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/api/public"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// Service 是供应商和商品资料的权威服务。
type Service struct{}

// ServiceName 返回供应商服务稳定逻辑名。
func (*Service) ServiceName() string { return contract.SupplierServiceName }

// Routers 注册供应商服务内部 Public API。
func (*Service) Routers() []servertypes.IRouter {
	routers := []servertypes.IRouter{&publicapi.GetSuppliers{}, &publicapi.GetProducts{}}
	routers = append(routers, manageapi.NewSupplierManage().Routers()...)
	routers = append(routers, manageapi.NewProductManage().Routers()...)
	return routers
}
