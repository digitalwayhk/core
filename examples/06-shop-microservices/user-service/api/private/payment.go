package private

import (
	"errors"

	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	orderapi "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/public"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

type CreatePayment struct {
	OrderID       uint `json:"orderID"`
	PaymentTypeID uint `json:"paymentTypeID"`
}

func (own *CreatePayment) Parse(req servertypes.IRequest) error { return req.Bind(own) }
func (own *CreatePayment) Validation(req servertypes.IRequest) error {
	if own.OrderID == 0 || own.PaymentTypeID == 0 {
		return errors.New("订单和支付类型不能为空")
	}
	_, err := trustedUser(req, true)
	return err
}
func (own *CreatePayment) Do(req servertypes.IRequest) (interface{}, error) {
	user, err := trustedUser(req, true)
	if err != nil {
		return nil, err
	}
	response, err := req.CallService(&orderapi.CreatePayment{UserID: user.ID, OrderID: own.OrderID, PaymentTypeID: own.PaymentTypeID})
	if err != nil {
		return nil, err
	}
	if !response.GetSuccess() {
		return nil, response.GetError()
	}
	result := &orderdto.PaymentRecord{}
	response.GetData(result)
	return result, nil
}
func (*CreatePayment) GetResponse() interface{}                { return &orderdto.PaymentRecord{} }
func (own *CreatePayment) RouterInfo() *servertypes.RouterInfo { return router.DefaultRouterInfo(own) }
