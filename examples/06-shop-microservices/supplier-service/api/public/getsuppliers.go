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

type GetSuppliers struct {
	ID   uint   `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

func (g *GetSuppliers) Parse(req servertypes.IRequest) error {
	g.Code = strings.TrimSpace(req.GetValue("code"))
	g.Name = strings.TrimSpace(req.GetValue("name"))
	if value := strings.TrimSpace(req.GetValue("id")); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return err
		}
		g.ID = uint(parsed)
	}
	return nil
}
func (*GetSuppliers) Validation(servertypes.IRequest) error { return nil }
func (g *GetSuppliers) Do(servertypes.IRequest) (interface{}, error) {
	return business.AvailableSuppliers(g.ID, g.Code, g.Name)
}
func (*GetSuppliers) GetResponse() interface{} { return business.SupplierListResponse() }
func (g *GetSuppliers) GetCacheKey() string {
	return utils.HashCodes(strconv.FormatUint(uint64(g.ID), 10), strings.ToLower(g.Code), strings.ToLower(g.Name))
}
func (g *GetSuppliers) RouterInfo() *servertypes.RouterInfo {
	info := router.DefaultRouterInfoWithOptions(g,
		router.WithServiceName(contract.SupplierServiceName),
		router.WithPath("/api/"+contract.SupplierServiceName+"/getsuppliers"),
		router.WithMethod(http.MethodGet),
		router.WithInternalCallers(contract.UserServiceName),
	)
	info.UseCache(30 * time.Second)
	return info
}
