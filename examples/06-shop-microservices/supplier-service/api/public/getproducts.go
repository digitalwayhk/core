package public

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/business"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

type GetProducts struct {
	ID         uint   `json:"id"`
	Name       string `json:"name"`
	Code       string `json:"code"`
	SupplierID uint   `json:"supplierID"`
}

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
func (*GetProducts) Validation(servertypes.IRequest) error { return nil }
func (g *GetProducts) Do(servertypes.IRequest) (interface{}, error) {
	return business.AvailableProducts(g.ID, g.Name, g.Code, g.SupplierID)
}
func (*GetProducts) GetResponse() interface{} { return business.ProductListResponse() }
func (g *GetProducts) GetCacheKey() string {
	return utils.HashCodes(strconv.FormatUint(uint64(g.ID), 10), strings.ToLower(g.Name), strings.ToLower(g.Code), strconv.FormatUint(uint64(g.SupplierID), 10))
}
func (g *GetProducts) RouterInfo() *servertypes.RouterInfo {
	info := router.DefaultRouterInfoWithOptions(g,
		router.WithServiceName(contract.SupplierServiceName),
		router.WithPath("/api/"+contract.SupplierServiceName+"/getproducts"),
		router.WithMethod(http.MethodGet),
		router.WithInternalCallers(contract.UserServiceName, contract.OrderServiceName),
	)
	info.UseCache(30 * time.Second)
	return info
}
