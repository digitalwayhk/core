// Package public 提供 07 订单服务撤单内部 Public API。
package public

import (
	"errors"
	"net/http"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	orderdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/order"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// CancelOrder 是 user-service 调用 order-service 的撤单入口。
type CancelOrder struct {
	UserID  uint `json:"userID"`
	OrderID uint `json:"orderID"`
	store   models.OrderWriteAccess
}

// NewCancelOrder 创建绑定当前实例订单 runtime 的撤单路由。
func NewCancelOrder(store models.OrderWriteAccess) *CancelOrder { return &CancelOrder{store: store} }

// New 为请求池创建保留实例依赖的新路由。
func (own *CancelOrder) New(interface{}) servertypes.IRouter { return NewCancelOrder(own.store) }

// Parse 绑定撤单请求。
func (own *CancelOrder) Parse(req servertypes.IRequest) error { return req.Bind(own) }

// Validation 校验撤单请求。
func (own *CancelOrder) Validation(servertypes.IRequest) error {
	if own.UserID == 0 || own.OrderID == 0 {
		return errors.New("撤单参数不完整")
	}
	return nil
}

// Do 在远程权威库撤销订单并发布订单状态变化事件。
func (own *CancelOrder) Do(req servertypes.IRequest) (interface{}, error) {
	item, err := business.CancelOrder(own.store, own.OrderID, own.UserID, req.GetTraceId())
	if err != nil {
		return nil, err
	}
	if sc := router.GetContext(contract.OrderServiceName); sc != nil {
		sc.NotifyOutbox()
	}
	return orderToDTO(item), nil
}

// GetResponse 返回撤单响应 DTO 类型。
func (*CancelOrder) GetResponse() interface{} { return &orderdto.Order{} }

// RouterInfo 返回撤单内部 Public 路由信息。
func (own *CancelOrder) RouterInfo() *servertypes.RouterInfo {
	return orderPublicRoute(own, "cancelorder", http.MethodPost)
}

// Reset 清理请求字段并保留实例级订单 store。
func (own *CancelOrder) Reset() {
	store := own.store
	*own = CancelOrder{store: store}
}
