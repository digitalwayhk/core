// Package public 提供 07 订单服务支付内部 Public API。
package public

import (
	"errors"
	"net/http"
	"strings"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	orderdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/order"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// CreatePayment 是 user-service 调用 order-service 的支付入口。
type CreatePayment struct {
	UserID        uint   `json:"userID"`
	OrderID       uint   `json:"orderID"`
	PaymentTypeID uint   `json:"paymentTypeID"`
	PaymentID     string `json:"paymentID"`
	store         models.OrderWriteAccess
}

// NewCreatePayment 创建绑定当前实例订单 runtime 的支付路由。
func NewCreatePayment(store models.OrderWriteAccess) *CreatePayment {
	return &CreatePayment{store: store}
}

// New 为请求池创建保留实例依赖的新路由。
func (own *CreatePayment) New(interface{}) servertypes.IRouter { return NewCreatePayment(own.store) }

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
	item, err := business.PayOrder(own.store, own.OrderID, own.UserID, paymentID, req.GetTraceId())
	if err != nil {
		return nil, err
	}
	if sc := router.GetContext(contract.OrderServiceName); sc != nil {
		sc.NotifyOutbox()
	}
	return orderToDTO(item), nil
}

// GetResponse 返回支付响应 DTO 类型。
func (*CreatePayment) GetResponse() interface{} { return &orderdto.Order{} }

// RouterInfo 返回支付内部 Public 路由信息。
func (own *CreatePayment) RouterInfo() *servertypes.RouterInfo {
	return orderPublicRoute(own, "createpayment", http.MethodPost)
}

// Reset 清理请求字段并保留实例级订单 store。
func (own *CreatePayment) Reset() {
	store := own.store
	*own = CreatePayment{store: store}
}
