// Package private 提供 07 用户入口服务买家支付 API。
package private

import (
	"errors"
	"net/http"
	"strconv"

	orderdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/order"
	orderapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/public"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// CreatePayment 是买家支付本人订单入口。
type CreatePayment struct {
	OrderID       uint   `json:"orderID"`
	PaymentTypeID uint   `json:"paymentTypeID"`
	PaymentID     string `json:"paymentID"`
}

// Parse 绑定支付请求。
func (own *CreatePayment) Parse(req servertypes.IRequest) error { return req.Bind(own) }

// Validation 校验支付请求。
func (own *CreatePayment) Validation(servertypes.IRequest) error {
	if own.OrderID == 0 || own.PaymentTypeID == 0 {
		return errors.New("支付参数不完整")
	}
	return nil
}

// Do 调用订单服务内部支付 API。
func (own *CreatePayment) Do(req servertypes.IRequest) (interface{}, error) {
	uid, _ := req.GetUser()
	userID64, err := strconv.ParseUint(uid, 10, 64)
	if err != nil || userID64 == 0 {
		return nil, errors.New("用户身份无效")
	}
	response, err := req.CallService(&orderapi.CreatePayment{UserID: uint(userID64), OrderID: own.OrderID, PaymentTypeID: own.PaymentTypeID, PaymentID: own.PaymentID})
	if err != nil || !response.GetSuccess() {
		if err != nil {
			return nil, err
		}
		return nil, response.GetError()
	}
	var order orderdto.Order
	response.GetData(&order)
	return &order, nil
}

// GetResponse 返回支付响应 DTO 类型。
func (*CreatePayment) GetResponse() interface{} { return &orderdto.Order{} }

// RouterInfo 返回买家支付 Private 路由信息。
func (own *CreatePayment) RouterInfo() *servertypes.RouterInfo {
	return userPrivateRoute(own, "createpayment", http.MethodPost)
}
