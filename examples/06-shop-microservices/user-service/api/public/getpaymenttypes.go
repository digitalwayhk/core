package public

import (
	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	orderapi "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/public"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"net/http"
)

// GetPaymentTypes 是买家查询可用支付类型的 facade。
type GetPaymentTypes struct{}

func (*GetPaymentTypes) Parse(servertypes.IRequest) error      { return nil }
func (*GetPaymentTypes) Validation(servertypes.IRequest) error { return nil }
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
func (*GetPaymentTypes) GetResponse() interface{} { return []*orderdto.PaymentType{} }
func (g *GetPaymentTypes) RouterInfo() *servertypes.RouterInfo {
	return router.DefaultRouterInfoWithOptions(g, router.WithMethod(http.MethodGet))
}
