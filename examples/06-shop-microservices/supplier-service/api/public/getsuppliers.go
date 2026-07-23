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

// GetSuppliers 定义本文件能力使用的核心结构。
type GetSuppliers struct {
	ID   uint   `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// Parse 实现本类型在当前服务边界中的行为。
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

// Validation 实现本类型在当前服务边界中的行为。
func (*GetSuppliers) Validation(servertypes.IRequest) error { return nil }

// Do 实现本类型在当前服务边界中的行为。
func (g *GetSuppliers) Do(servertypes.IRequest) (interface{}, error) {
	return business.AvailableSuppliers(g.ID, g.Code, g.Name)
}

// GetResponse 实现本类型在当前服务边界中的行为。
func (*GetSuppliers) GetResponse() interface{} { return business.SupplierListResponse() }

// RouterInfo 实现本类型在当前服务边界中的行为。
func (g *GetSuppliers) RouterInfo() *servertypes.RouterInfo {
	return router.DefaultRouterInfoWithOptions(g,
		router.WithServiceName(contract.SupplierServiceName),
		router.WithPath("/api/"+contract.SupplierServiceName+"/getsuppliers"),
		router.WithMethod(http.MethodGet),
		router.WithInternalCallers(contract.UserServiceName),
	)
}
