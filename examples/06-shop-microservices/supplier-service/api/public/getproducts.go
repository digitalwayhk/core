package public

import (
	"net/http"
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/business"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// GetProducts 查询供应商和商品均启用的可售商品。
type GetProducts struct{ Name, Code, SupplierID string }

// Parse 读取可选筛选条件，全部为空时返回全部可售商品。
func (g *GetProducts) Parse(req servertypes.IRequest) error {
	g.Name, g.Code, g.SupplierID = strings.TrimSpace(req.GetValue("name")), strings.TrimSpace(req.GetValue("code")), strings.TrimSpace(req.GetValue("supplierID"))
	return nil
}
func (*GetProducts) Validation(servertypes.IRequest) error { return nil }
func (g *GetProducts) Do(servertypes.IRequest) (interface{}, error) {
	return business.AvailableProducts(g.Name, g.Code, g.SupplierID)
}
func (*GetProducts) GetResponse() interface{} { return business.ProductListResponse() }
func (g *GetProducts) RouterInfo() *servertypes.RouterInfo {
	return router.DefaultRouterInfoWithOptions(g, router.WithServiceName(contract.SupplierServiceName), router.WithPath("/api/"+contract.SupplierServiceName+"/getproducts"), router.WithMethod(http.MethodGet))
}
