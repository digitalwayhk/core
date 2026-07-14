package responsemodel

import (
	"time"

	"github.com/digitalwayhk/core/examples/01-simple-shop/models"
)

// Order 是 Private API 和 WebSocket 对外暴露的订单响应模型。
type Order struct {
	ID          uint   `json:"id,string" desc:"订单 ID"`
	ProductID   uint   `json:"productID" desc:"商品 ID"`
	ProductName string `json:"productName" desc:"商品名称快照"`
	UnitPrice   string `json:"unitPrice" desc:"商品单价快照"`
	Quantity    int    `json:"quantity" desc:"购买数量"`
	UserID      string `json:"userID" desc:"用户 ID"`
	CreatedAt   string `json:"createdAt" desc:"秒级下单时间"`
}

// NewOrder 从持久化订单创建不含深层基础模型的秒级响应快照。
func NewOrder(model *models.Order) *Order {
	if model == nil {
		return nil
	}
	createdAt := ""
	if model.CreatedAt != nil {
		createdAt = model.CreatedAt.UTC().Truncate(time.Second).Format(time.RFC3339)
	}
	return &Order{
		ID:          model.ID,
		ProductID:   model.ProductID,
		ProductName: model.ProductName,
		UnitPrice:   model.UnitPrice.String(),
		Quantity:    model.Quantity,
		UserID:      model.UserID,
		CreatedAt:   createdAt,
	}
}

// Orders 将订单持久化列表转换为对外响应列表。
func Orders(modelsList []*models.Order) []*Order {
	result := make([]*Order, 0, len(modelsList))
	for _, model := range modelsList {
		if response := NewOrder(model); response != nil {
			result = append(result, response)
		}
	}
	return result
}
