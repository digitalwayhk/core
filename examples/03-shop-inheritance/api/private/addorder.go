package private

import (
	"strings"

	"github.com/digitalwayhk/core/examples/03-shop-inheritance/api/dto"
	"github.com/digitalwayhk/core/examples/03-shop-inheritance/business"
	"github.com/digitalwayhk/core/examples/03-shop-inheritance/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// AddOrder 创建当前用户的未支付订单。
type AddOrder struct {
	ProductID uint `json:"productID"`
	Quantity  int  `json:"quantity"`
}

// Parse 绑定商品和数量，不接受用户字段。
func (own *AddOrder) Parse(req servertypes.IRequest) error { return req.Bind(own) }

// Validation 校验身份和基础参数。
func (own *AddOrder) Validation(req servertypes.IRequest) error {
	userID, _ := req.GetUser()
	if strings.TrimSpace(userID) == "" {
		return models.NewBusinessError("用户身份无效")
	}
	if own.ProductID == 0 || own.Quantity <= 0 {
		return models.NewValidationError("商品 ID 和正数数量不能为空")
	}
	return nil
}

// Do 调用订单业务服务并在提交后通知。
func (own *AddOrder) Do(req servertypes.IRequest) (interface{}, error) {
	userID, _ := req.GetUser()
	change, err := business.NewOrderService().CreateOrder(userID, own.ProductID, own.Quantity, req.NewID())
	if err != nil {
		return nil, err
	}
	response := NotifyOrderChange(change.Action, change.Order)
	return response, nil
}

// GetResponse 返回 OpenAPI 订单结构。
func (own *AddOrder) GetResponse() interface{} { return &dto.OrderResponse{} }

// RouterInfo 注册认证 POST 路由。
func (own *AddOrder) RouterInfo() *servertypes.RouterInfo { return router.DefaultRouterInfo(own) }
