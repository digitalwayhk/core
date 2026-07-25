package private

import (
	"strings"

	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/api/dto"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/business"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// DeleteOrder 物理删除当前用户未支付或支付失败订单。
type DeleteOrder struct {
	ID uint `json:"id,string"`
}

// Parse 绑定订单 ID。
func (own *DeleteOrder) Parse(req servertypes.IRequest) error { return req.Bind(own) }

// Validation 校验订单 ID 和认证身份。
func (own *DeleteOrder) Validation(req servertypes.IRequest) error {
	userID, _ := req.GetUser()
	if own.ID == 0 || strings.TrimSpace(userID) == "" {
		return models.NewBusinessError("订单不存在或无权操作")
	}
	return nil
}

// Do 调用订单业务服务并在删除后通知。
func (own *DeleteOrder) Do(req servertypes.IRequest) (interface{}, error) {
	userID, _ := req.GetUser()
	change, err := business.NewOrderService().DeleteUnpaidOrder(userID, own.ID)
	if err != nil {
		return nil, err
	}
	response := NotifyOrderChange(change.Action, change.Order)
	return response, nil
}

// GetResponse 返回 OpenAPI 订单结构。
func (own *DeleteOrder) GetResponse() interface{} { return &dto.OrderResponse{} }

// RouterInfo 注册认证 POST 路由。
func (own *DeleteOrder) RouterInfo() *servertypes.RouterInfo { return router.DefaultRouterInfo(own) }
