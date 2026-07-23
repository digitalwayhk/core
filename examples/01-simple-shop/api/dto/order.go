package dto

import (
	"time"

	"github.com/digitalwayhk/core/examples/01-simple-shop/models"
)

// OrderResponse 是 Private API 和 WebSocket 对外暴露的订单 DTO。
type OrderResponse struct {
	Action      string `json:"action,omitempty" desc:"订单变更动作"`
	ID          uint   `json:"id,string" desc:"订单 ID"`
	ProductID   uint   `json:"productID" desc:"商品 ID"`
	ProductName string `json:"productName" desc:"商品名称快照"`
	UnitPrice   string `json:"unitPrice" desc:"商品单价快照"`
	Quantity    int    `json:"quantity" desc:"购买数量"`
	UserID      string `json:"userID" desc:"用户 ID"`
	CreatedAt   string `json:"createdAt" desc:"秒级下单时间"`
}

// WithAction 创建用于 WebSocket 通知的独立副本，不修改 HTTP 响应 DTO。
func (own *OrderResponse) WithAction(action string) *OrderResponse {
	if own == nil {
		return nil
	}
	result := *own
	result.Action = action
	return &result
}

// NewOrderResponse 从持久化订单创建不含深层基础模型的秒级响应 DTO。
func NewOrderResponse(model *models.Order) *OrderResponse {
	if model == nil {
		return nil
	}
	createdAt := ""
	if model.CreatedAt != nil {
		createdAt = model.CreatedAt.UTC().Truncate(time.Second).Format(time.RFC3339)
	}
	return &OrderResponse{
		ID:          model.ID,
		ProductID:   model.ProductID,
		ProductName: model.ProductName,
		UnitPrice:   model.UnitPrice.String(),
		Quantity:    model.Quantity,
		UserID:      model.UserID,
		CreatedAt:   createdAt,
	}
}

// OrderResponses 将订单持久化列表转换为对外响应 DTO 列表。
func OrderResponses(modelsList []*models.Order) []*OrderResponse {
	result := make([]*OrderResponse, 0, len(modelsList))
	for _, model := range modelsList {
		if response := NewOrderResponse(model); response != nil {
			result = append(result, response)
		}
	}
	return result
}
