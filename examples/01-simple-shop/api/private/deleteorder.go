package private

import (
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/examples/01-simple-shop/api/dto"
	"github.com/digitalwayhk/core/examples/01-simple-shop/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// DeleteOrder 按订单 ID 物理删除当前用户自己的订单。
type DeleteOrder struct {
	ID uint `json:"id,string"`
}

// Parse 绑定字符串形式的订单 ID，并拒绝无效格式。
func (own *DeleteOrder) Parse(req servertypes.IRequest) error {
	value := strings.TrimSpace(req.GetValue("id"))
	if value == "" {
		return req.Bind(own)
	}
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return models.NewBusinessError("订单不存在或无权操作")
	}
	own.ID = uint(id)
	return nil
}

// Validation 校验订单 ID 和可信登录身份。
func (own *DeleteOrder) Validation(req servertypes.IRequest) error {
	userID, _ := req.GetUser()
	if own.ID == 0 || strings.TrimSpace(userID) == "" {
		return models.NewBusinessError("订单不存在或无权操作")
	}
	return nil
}

// Do 使用 ID 与 UserID 组合条件查询并删除，避免泄露其他用户订单。
func (own *DeleteOrder) Do(req servertypes.IRequest) (interface{}, error) {
	userID, _ := req.GetUser()
	order, err := models.NewOrder().FindOwned(own.ID, userID)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, models.NewBusinessError("订单不存在或无权操作")
	}
	if err := order.Delete(); err != nil {
		return nil, err
	}
	response := dto.NewOrderResponse(order)
	notifyOrderChange(response.WithAction("deleted"))
	return response, nil
}

// GetResponse 返回 OpenAPI 用的删除订单成功响应结构。
func (own *DeleteOrder) GetResponse() interface{} {
	return &dto.OrderResponse{}
}

// RouterInfo 将订单删除注册为需要认证的 POST 路由。
func (own *DeleteOrder) RouterInfo() *servertypes.RouterInfo {
	return router.DefaultRouterInfo(own)
}
