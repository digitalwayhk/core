package private

import (
	"strings"

	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/api/dto"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/business"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// CreatePayment 为当前用户订单创建新的支付尝试。
type CreatePayment struct {
	OrderID       uint `json:"orderID,string"`
	PaymentTypeID uint `json:"paymentTypeID,string"`
}

// Parse 绑定订单和支付类型 ID。
func (own *CreatePayment) Parse(req servertypes.IRequest) error { return req.Bind(own) }

// Validation 校验身份和必填 ID。
func (own *CreatePayment) Validation(req servertypes.IRequest) error {
	userID, _ := req.GetUser()
	if strings.TrimSpace(userID) == "" || own.OrderID == 0 || own.PaymentTypeID == 0 {
		return models.NewValidationError("订单和支付类型不能为空")
	}
	return nil
}

// Do 创建支付流水、更新订单并发送支付中通知。
func (own *CreatePayment) Do(req servertypes.IRequest) (interface{}, error) {
	userID, _ := req.GetUser()
	change, err := business.NewPaymentService().CreatePayment(userID, own.OrderID, own.PaymentTypeID, req.NewID())
	if err != nil {
		return nil, err
	}
	response := NotifyOrderChange(change.Action, change.Order)
	return response, nil
}

// GetResponse 返回 OpenAPI 订单结构。
func (own *CreatePayment) GetResponse() interface{} { return &dto.OrderResponse{} }

// RouterInfo 注册认证 POST 路由。
func (own *CreatePayment) RouterInfo() *servertypes.RouterInfo { return router.DefaultRouterInfo(own) }
