// 本文件提供当前服务供其他服务调用的 Public API 或入口 facade 能力。
package public

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/business"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// GetProducts 定义本文件能力使用的核心结构。
type GetProducts struct {
	ID         uint   `json:"id"`
	Name       string `json:"name"`
	Code       string `json:"code"`
	SupplierID uint   `json:"supplierID"`
}

// Parse 实现本类型在当前服务边界中的行为。
func (g *GetProducts) Parse(req servertypes.IRequest) error {
	g.Name, g.Code = strings.TrimSpace(req.GetValue("name")), strings.TrimSpace(req.GetValue("code"))
	for value, target := range map[string]*uint{"id": &g.ID, "supplierID": &g.SupplierID} {
		text := strings.TrimSpace(req.GetValue(value))
		if text == "" {
			continue
		}
		parsed, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return err
		}
		*target = uint(parsed)
	}
	return nil
}

// Validation 实现本类型在当前服务边界中的行为。
func (*GetProducts) Validation(servertypes.IRequest) error { return nil }

// Do 实现本类型在当前服务边界中的行为。
func (g *GetProducts) Do(servertypes.IRequest) (interface{}, error) {
	return business.AvailableProducts(g.ID, g.Name, g.Code, g.SupplierID)
}

// GetResponse 实现本类型在当前服务边界中的行为。
func (*GetProducts) GetResponse() interface{} { return business.ProductListResponse() }

// RouterInfo 实现本类型在当前服务边界中的行为。
func (g *GetProducts) RouterInfo() *servertypes.RouterInfo {
	return router.DefaultRouterInfoWithOptions(g,
		router.WithServiceName(contract.SupplierServiceName),
		router.WithPath("/api/"+contract.SupplierServiceName+"/getproducts"),
		router.WithMethod(http.MethodGet),
		router.WithInternalCallers(contract.UserServiceName, contract.OrderServiceName),
	)
}
