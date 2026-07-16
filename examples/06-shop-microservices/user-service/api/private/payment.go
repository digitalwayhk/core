package private

import (
	"errors"
	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	orderapi "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/private"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// CreatePayment 转发当前用户的支付请求，Order Service 仍会再次校验订单所有权。
type CreatePayment struct {
	OrderID       uint `json:"orderID"`
	PaymentTypeID uint `json:"paymentTypeID"`
}

func (c *CreatePayment) Parse(req servertypes.IRequest) error { return req.Bind(c) }
func (c *CreatePayment) Validation(req servertypes.IRequest) error {
	if c.OrderID == 0 || c.PaymentTypeID == 0 {
		return errors.New("订单和支付类型不能为空")
	}
	_, err := trustedUser(req)
	return err
}
func (c *CreatePayment) Do(req servertypes.IRequest) (interface{}, error) {
	response, err := req.CallService(&orderapi.CreatePayment{OrderID: c.OrderID, PaymentTypeID: c.PaymentTypeID})
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
func (*CreatePayment) GetResponse() interface{}              { return &orderdto.PaymentRecord{} }
func (c *CreatePayment) RouterInfo() *servertypes.RouterInfo { return router.DefaultRouterInfo(c) }
