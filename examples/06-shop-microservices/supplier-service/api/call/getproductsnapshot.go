// Package call 保存 Supplier Service 供其他服务构造的已注册目标 API。
// 该包不保存地址、连接或重试策略，传输仍由 ServiceResolver 和 TransportSelector 完成。
package call

import (
	"errors"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/business"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// GetProductSnapshot 是 Supplier Service 注册的跨服务目标 API。
type GetProductSnapshot struct {
	ProductID uint `json:"productID"`
}

func (g *GetProductSnapshot) Parse(req servertypes.IRequest) error { return req.Bind(g) }
func (g *GetProductSnapshot) Validation(servertypes.IRequest) error {
	if g.ProductID == 0 {
		return errors.New("商品 ID 不能为空")
	}
	return nil
}
func (g *GetProductSnapshot) Do(servertypes.IRequest) (interface{}, error) {
	return business.ProductSnapshot(g.ProductID)
}
func (*GetProductSnapshot) GetResponse() interface{} { return &supplierdto.ProductSnapshot{} }
func (g *GetProductSnapshot) RouterInfo() *servertypes.RouterInfo {
	return router.DefaultRouterInfoWithOptions(g, router.WithServiceName(contract.SupplierServiceName), router.WithPath("/api/"+contract.SupplierServiceName+"/getproductsnapshot"), router.WithPathType(servertypes.PrivateType), router.WithAuth(true))
}
