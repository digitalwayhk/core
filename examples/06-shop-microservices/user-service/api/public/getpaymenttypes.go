// 本文件提供当前服务供其他服务调用的 Public API 或入口 facade 能力。
package public

import (
	"net/http"
	"time"

	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	orderapi "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/public"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// GetPaymentTypes 是买家查询可用支付类型的 facade。
type GetPaymentTypes struct{}

// Parse 实现本类型在当前服务边界中的行为。
func (*GetPaymentTypes) Parse(servertypes.IRequest) error { return nil }

// Validation 实现本类型在当前服务边界中的行为。
func (*GetPaymentTypes) Validation(servertypes.IRequest) error { return nil }

// Do 实现本类型在当前服务边界中的行为。
func (*GetPaymentTypes) Do(req servertypes.IRequest) (interface{}, error) {
	response, err := req.CallService(&orderapi.GetPaymentTypes{})
	if err != nil {
		return nil, err
	}
	if !response.GetSuccess() {
		return nil, response.GetError()
	}
	items := []*orderdto.PaymentType{}
	response.GetData(&items)
	return items, nil
}

// GetResponse 实现本类型在当前服务边界中的行为。
func (*GetPaymentTypes) GetResponse() interface{} { return []*orderdto.PaymentType{} }

// RouterInfo 实现本类型在当前服务边界中的行为。
func (g *GetPaymentTypes) RouterInfo() *servertypes.RouterInfo {
	info := router.DefaultRouterInfoWithOptions(g, router.WithMethod(http.MethodGet))
	info.UseCache(30 * time.Second)
	return info
}
