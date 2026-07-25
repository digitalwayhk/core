package private

import (
	"strings"

	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/api/dto"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/business"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// CancelOrder 为当前用户已支付订单申请撤销退款。
type CancelOrder struct {
	ID uint `json:"id,string"`
}

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
	change, err := business.NewOrderService().RequestCancellation(userID, own.ID)
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
