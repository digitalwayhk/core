package public

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/examples/03-shop-inheritance/api/dto"
	"github.com/digitalwayhk/core/examples/03-shop-inheritance/business"
	"github.com/digitalwayhk/core/examples/03-shop-inheritance/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// GetProducts 查询商品和供应商均启用的可下单商品。
type GetProducts struct {
	ID           uint
	Code         string
	Name         string
	SupplierID   uint
	SupplierCode string
}

// Parse 读取全部可选商品筛选条件。
func (own *GetProducts) Parse(req servertypes.IRequest) error {
	var err error
	own.ID, err = parseOptionalID(req.GetValue("id"), "商品 ID")
	if err != nil {
		return err
	}
	own.SupplierID, err = parseOptionalID(req.GetValue("supplierID"), "供应商 ID")
	if err != nil {
		return err
	}
	own.Code = strings.TrimSpace(req.GetValue("code"))
	own.Name = strings.TrimSpace(req.GetValue("name"))
	own.SupplierCode = strings.TrimSpace(req.GetValue("supplierCode"))
	return nil
}

// Validation 允许全部筛选条件为空。
func (own *GetProducts) Validation(servertypes.IRequest) error { return nil }

// Do 查询有效商品并转换为公开 DTO。
func (own *GetProducts) Do(servertypes.IRequest) (interface{}, error) {
	items, err := business.NewProductService().ListAvailable(own.ID, own.Code, own.Name, own.SupplierID, own.SupplierCode)
	if err != nil {
		return nil, err
	}
	return dto.ProductResponses(items), nil
}

// GetResponse 返回 OpenAPI 商品列表结构。
func (own *GetProducts) GetResponse() interface{} { return []*dto.ProductResponse{} }

// RouterInfo 注册公开 GET 路由。
func (own *GetProducts) RouterInfo() *servertypes.RouterInfo {
	return router.DefaultRouterInfoWithOptions(own, router.WithMethod(http.MethodGet))
}

func parseOptionalID(value, title string) (uint, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, models.NewValidationError(title + " 格式错误")
	}
	return uint(id), nil
}
