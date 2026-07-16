package public

import (
	"net/http"
	"strings"
	"time"

	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	supplierapi "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/api/public"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

// GetProducts 是 User Service 面向买家的商品查询 facade。
type GetProducts struct{ Name, Code, SupplierID string }

func (g *GetProducts) Parse(req servertypes.IRequest) error {
	g.Name = strings.TrimSpace(req.GetValue("name"))
	g.Code = strings.TrimSpace(req.GetValue("code"))
	g.SupplierID = strings.TrimSpace(req.GetValue("supplierID"))
	return nil
}
func (*GetProducts) Validation(servertypes.IRequest) error { return nil }
func (g *GetProducts) Do(req servertypes.IRequest) (interface{}, error) {
	res, err := req.CallService(&supplierapi.GetProducts{Name: g.Name, Code: g.Code, SupplierID: g.SupplierID})
	if err != nil {
		return nil, err
	}
	if !res.GetSuccess() {
		return nil, res.GetError()
	}
	items := []*supplierdto.Product{}
	res.GetData(&items)
	return items, nil
}
func (*GetProducts) GetResponse() interface{} { return []*supplierdto.Product{} }
func (g *GetProducts) GetCacheKey() string {
	return utils.HashCodes(strings.ToLower(g.Name), strings.ToLower(g.Code), g.SupplierID)
}
func (g *GetProducts) RouterInfo() *servertypes.RouterInfo {
	info := router.DefaultRouterInfoWithOptions(g, router.WithMethod(http.MethodGet))
	info.UseCache(30 * time.Second)
	return info
}
