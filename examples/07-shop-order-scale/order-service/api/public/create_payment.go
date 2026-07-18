// Package public 提供 07 订单服务支付内部 Public API。
package public

import (
	"errors"
	"net/http"
	"strings"

	orderdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/order"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// CreatePayment 是 user-service 调用 order-service 的支付入口。
type CreatePayment struct {
	UserID        uint   `json:"userID"`
	OrderID       uint   `json:"orderID"`
	PaymentTypeID uint   `json:"paymentTypeID"`
	PaymentID     string `json:"paymentID"`
}

// Parse 绑定支付请求。
func (own *CreatePayment) Parse(req servertypes.IRequest) error { return req.Bind(own) }

// Validation 校验支付请求。
func (own *CreatePayment) Validation(servertypes.IRequest) error {
	if own.UserID == 0 || own.OrderID == 0 || own.PaymentTypeID == 0 {
		return errors.New("支付参数不完整")
	}
	return nil
}

// Do 在远程权威库标记订单支付成功。
func (own *CreatePayment) Do(req servertypes.IRequest) (interface{}, error) {
	paymentID := strings.TrimSpace(own.PaymentID)
	if paymentID == "" {
		paymentID = req.GetTraceId()
	}
	item, err := business.PayOrder(own.OrderID, own.UserID, paymentID)
	if err != nil {
		return nil, err
	}
	return orderToDTO(item), nil
}

// GetResponse 返回支付响应 DTO 类型。
func (*CreatePayment) GetResponse() interface{} { return &orderdto.Order{} }

// RouterInfo 返回支付内部 Public 路由信息。
func (own *CreatePayment) RouterInfo() *servertypes.RouterInfo {
	return orderPublicRoute(own, "createpayment", http.MethodPost)
}
