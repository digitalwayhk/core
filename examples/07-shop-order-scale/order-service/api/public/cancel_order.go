// Package public 提供 07 订单服务撤单内部 Public API。
package public

import (
	"errors"
	"net/http"

	orderdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/order"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// CancelOrder 是 user-service 调用 order-service 的撤单入口。
type CancelOrder struct {
	UserID  uint `json:"userID"`
	OrderID uint `json:"orderID"`
}

// Parse 绑定撤单请求。
func (own *CancelOrder) Parse(req servertypes.IRequest) error { return req.Bind(own) }

// Validation 校验撤单请求。
func (own *CancelOrder) Validation(servertypes.IRequest) error {
	if own.UserID == 0 || own.OrderID == 0 {
		return errors.New("撤单参数不完整")
	}
	return nil
}

// Do 在远程权威库撤销订单。
func (own *CancelOrder) Do(servertypes.IRequest) (interface{}, error) {
	item, err := business.CancelOrder(own.OrderID, own.UserID)
	if err != nil {
		return nil, err
	}
	return orderToDTO(item), nil
}

// GetResponse 返回撤单响应 DTO 类型。
func (*CancelOrder) GetResponse() interface{} { return &orderdto.Order{} }

// RouterInfo 返回撤单内部 Public 路由信息。
func (own *CancelOrder) RouterInfo() *servertypes.RouterInfo {
	return orderPublicRoute(own, "cancelorder", http.MethodPost)
}
