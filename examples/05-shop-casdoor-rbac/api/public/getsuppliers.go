package public

import (
	"net/http"
	"strings"

	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/api/dto"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/business"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// GetSuppliers 查询公开可见的启用供应商。
type GetSuppliers struct {
	ID   uint
	Code string
	Name string
}

// Parse 读取可选供应商筛选条件。
func (own *GetSuppliers) Parse(req servertypes.IRequest) error {
	id, err := parseOptionalID(req.GetValue("id"), "供应商 ID")
	if err != nil {
		return err
	}
	own.ID = id
	own.Code = strings.TrimSpace(req.GetValue("code"))
	own.Name = strings.TrimSpace(req.GetValue("name"))
	return nil
}

// Validation 允许全部筛选条件为空。
func (own *GetSuppliers) Validation(servertypes.IRequest) error { return nil }

// Do 查询启用供应商并转换为公开 DTO。
func (own *GetSuppliers) Do(servertypes.IRequest) (interface{}, error) {
	items, err := business.NewSupplierService().ListEnabled(own.ID, own.Code, own.Name)
	if err != nil {
		return nil, err
	}
	return dto.SupplierResponses(items), nil
}

// GetResponse 返回 OpenAPI 供应商列表结构。
func (own *GetSuppliers) GetResponse() interface{} { return []*dto.SupplierResponse{} }

// RouterInfo 注册公开 GET 路由。
func (own *GetSuppliers) RouterInfo() *servertypes.RouterInfo {
	return router.DefaultRouterInfoWithOptions(own, router.WithMethod(http.MethodGet))
}
