package private

import (
	"strings"

	"github.com/digitalwayhk/core/examples/04-shop-performance/api/dto"
	"github.com/digitalwayhk/core/examples/04-shop-performance/business"
	"github.com/digitalwayhk/core/examples/04-shop-performance/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// CancelOrder 为当前用户已支付订单申请撤销退款。
type CancelOrder struct {
	ID      uint `json:"id,string"`
	service *business.OrderService
}

// NewCancelOrder 创建绑定实例级订单服务的撤销路由。
func NewCancelOrder(service *business.OrderService) *CancelOrder {
	return &CancelOrder{service: service}
}

// New 为请求池创建保留实例依赖的新路由。
func (own *CancelOrder) New(interface{}) servertypes.IRouter { return NewCancelOrder(own.service) }

// Parse 绑定订单 ID。
func (own *CancelOrder) Parse(req servertypes.IRequest) error { return req.Bind(own) }

// Validation 校验认证身份和订单 ID。
func (own *CancelOrder) Validation(req servertypes.IRequest) error {
	userID, _ := req.GetUser()
	if own.ID == 0 || strings.TrimSpace(userID) == "" {
		return models.NewBusinessError("订单不存在或无权操作")
	}
	return nil
}

// Do 申请撤销并发送退款中通知。
func (own *CancelOrder) Do(req servertypes.IRequest) (interface{}, error) {
	userID, _ := req.GetUser()
	change, err := own.service.RequestCancellation(userID, own.ID)
	if err != nil {
		return nil, err
	}
	response := NotifyOrderChange(change.Action, change.Order)
	return response, nil
}

// GetResponse 返回 OpenAPI 订单结构。
func (own *CancelOrder) GetResponse() interface{} { return &dto.OrderResponse{} }

// RouterInfo 注册认证 POST 路由。
func (own *CancelOrder) RouterInfo() *servertypes.RouterInfo { return router.DefaultRouterInfo(own) }

// Reset 清理请求字段并保留实例级订单服务。
func (own *CancelOrder) Reset() { own.ID = 0 }
