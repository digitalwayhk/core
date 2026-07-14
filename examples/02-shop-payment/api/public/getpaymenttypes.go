package public

import (
	"net/http"
	"strings"

	"github.com/digitalwayhk/core/examples/02-shop-payment/api/dto"
	"github.com/digitalwayhk/core/examples/02-shop-payment/business"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// GetPaymentTypes 查询启用的支付类型。
type GetPaymentTypes struct {
	Code string
	Name string
}

// Parse 读取可选编码和名称条件。
func (own *GetPaymentTypes) Parse(req servertypes.IRequest) error {
	own.Code = strings.TrimSpace(req.GetValue("code"))
	own.Name = strings.TrimSpace(req.GetValue("name"))
	return nil
}

// Validation 允许空筛选条件。
func (own *GetPaymentTypes) Validation(servertypes.IRequest) error { return nil }

// Do 查询启用支付类型并转换 DTO。
func (own *GetPaymentTypes) Do(servertypes.IRequest) (interface{}, error) {
	items, err := business.NewPaymentTypeService().ListEnabled(own.Code, own.Name)
	if err != nil {
		return nil, err
	}
	return dto.PaymentTypeResponses(items), nil
}

// GetResponse 返回 OpenAPI 支付类型列表结构。
func (own *GetPaymentTypes) GetResponse() interface{} { return []*dto.PaymentTypeResponse{} }

// RouterInfo 注册公开 GET 路由。
func (own *GetPaymentTypes) RouterInfo() *servertypes.RouterInfo {
	return router.DefaultRouterInfoWithOptions(own, router.WithMethod(http.MethodGet))
}
