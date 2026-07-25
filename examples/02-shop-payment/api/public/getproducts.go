package public

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/examples/02-shop-payment/api/dto"
	"github.com/digitalwayhk/core/examples/02-shop-payment/business"
	"github.com/digitalwayhk/core/examples/02-shop-payment/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// GetProducts 按可选 ID 和名称查询商品。
type GetProducts struct {
	ID   uint
	Name string
}

// Parse 读取可选商品筛选条件。
func (own *GetProducts) Parse(req servertypes.IRequest) error {
	own.Name = strings.TrimSpace(req.GetValue("name"))
	value := strings.TrimSpace(req.GetValue("id"))
	if value == "" {
		return nil
	}
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return models.NewBusinessError("商品 ID 格式错误")
	}
	own.ID = uint(id)
	return nil
}

// Validation 允许空筛选条件。
func (own *GetProducts) Validation(servertypes.IRequest) error { return nil }

// Do 调用商品业务服务并转换 DTO。
func (own *GetProducts) Do(servertypes.IRequest) (interface{}, error) {
	items, err := business.NewProductService().Query(own.ID, own.Name)
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
