// 本文件提供当前服务供其他服务调用的 Public API 或入口 facade 能力。
package public

import (
	"errors"
	"net/http"
	"strconv"

	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/business"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// CreatePayment 定义本文件能力使用的核心结构。
type CreatePayment struct {
	UserID        uint `json:"userID"`
	OrderID       uint `json:"orderID"`
	PaymentTypeID uint `json:"paymentTypeID"`
}

// Parse 实现本类型在当前服务边界中的行为。
func (own *CreatePayment) Parse(req servertypes.IRequest) error { return req.Bind(own) }

// Validation 实现本类型在当前服务边界中的行为。
func (own *CreatePayment) Validation(servertypes.IRequest) error {
	if own.UserID == 0 || own.OrderID == 0 || own.PaymentTypeID == 0 {
		return errors.New("支付参数不完整")
	}
	return nil
}

// Do 实现本类型在当前服务边界中的行为。
func (own *CreatePayment) Do(req servertypes.IRequest) (interface{}, error) {
	return business.CreatePayment(own.UserID, own.OrderID, own.PaymentTypeID, strconv.FormatUint(uint64(req.NewID()), 10), req.GetTraceId(), strconv.FormatUint(uint64(req.NewID()), 10))
}

// GetResponse 实现本类型在当前服务边界中的行为。
func (*CreatePayment) GetResponse() interface{} { return &orderdto.PaymentRecord{} }

// RouterInfo 实现本类型在当前服务边界中的行为。
func (own *CreatePayment) RouterInfo() *servertypes.RouterInfo {
	return orderPublicRoute(own, "createpayment", http.MethodPost)
}
