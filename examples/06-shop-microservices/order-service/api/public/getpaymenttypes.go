// 本文件提供当前服务供其他服务调用的 Public API 或入口 facade 能力。
package public

import (
	"net/http"

	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/business"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// GetPaymentTypes 返回已启用的支付类型。
type GetPaymentTypes struct{}

// Parse 实现本类型在当前服务边界中的行为。
func (*GetPaymentTypes) Parse(servertypes.IRequest) error { return nil }

// Validation 实现本类型在当前服务边界中的行为。
func (*GetPaymentTypes) Validation(servertypes.IRequest) error { return nil }

// Do 实现本类型在当前服务边界中的行为。
func (*GetPaymentTypes) Do(servertypes.IRequest) (interface{}, error) {
	return business.EnabledPaymentTypes()
}

// GetResponse 实现本类型在当前服务边界中的行为。
func (*GetPaymentTypes) GetResponse() interface{} { return []*orderdto.PaymentType{} }

// RouterInfo 实现本类型在当前服务边界中的行为。
func (g *GetPaymentTypes) RouterInfo() *servertypes.RouterInfo {
	return orderPublicRoute(g, "getpaymenttypes", http.MethodGet)
}
