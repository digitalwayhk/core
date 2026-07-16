package public

import (
	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/business"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"net/http"
)

// GetPaymentTypes 返回已启用的支付类型。
type GetPaymentTypes struct{}

func (*GetPaymentTypes) Parse(servertypes.IRequest) error      { return nil }
func (*GetPaymentTypes) Validation(servertypes.IRequest) error { return nil }
func (*GetPaymentTypes) Do(servertypes.IRequest) (interface{}, error) {
	return business.EnabledPaymentTypes()
}
func (*GetPaymentTypes) GetResponse() interface{} { return []*orderdto.PaymentType{} }
func (g *GetPaymentTypes) RouterInfo() *servertypes.RouterInfo {
	return router.DefaultRouterInfoWithOptions(g, router.WithMethod(http.MethodGet))
}
