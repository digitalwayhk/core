package dto

import (
	"time"

	"github.com/digitalwayhk/core/examples/03-shop-inheritance/models"
)

// OrderResponse 是 Private API 与 WebSocket 共用的订单 DTO。
type OrderResponse struct {
	Action            string               `json:"action,omitempty" desc:"订单变更动作"`
	ID                uint                 `json:"id,string" desc:"订单 ID"`
	ProductID         uint                 `json:"productID" desc:"商品 ID"`
	ProductCode       string               `json:"productCode" desc:"商品编码快照"`
	ProductName       string               `json:"productName" desc:"商品名称快照"`
	SupplierID        uint                 `json:"supplierID" desc:"供应商 ID"`
	SupplierCode      string               `json:"supplierCode" desc:"供应商编码快照"`
	SupplierName      string               `json:"supplierName" desc:"供应商名称快照"`
	UnitPrice         string               `json:"unitPrice" desc:"商品单价快照"`
	Quantity          int                  `json:"quantity" desc:"购买数量"`
	Amount            string               `json:"amount" desc:"订单总金额"`
	UserID            string               `json:"userID" desc:"用户 ID"`
	Status            models.OrderStatus   `json:"status" desc:"订单状态"`
	StatusName        string               `json:"statusName" desc:"订单状态名称"`
	PaymentStatus     models.PaymentStatus `json:"paymentStatus" desc:"支付状态"`
	PaymentStatusName string               `json:"paymentStatusName" desc:"支付状态名称"`
	PaymentID         uint                 `json:"paymentID,string" desc:"当前支付流水 ID"`
	CreatedAt         string               `json:"createdAt" desc:"秒级下单时间"`
}

// WithAction 创建通知副本，避免修改 HTTP 响应对象。
func (own *OrderResponse) WithAction(action string) *OrderResponse {
	if own == nil {
		return nil
	}
	result := *own
	result.Action = action
	return &result
}

// NewOrderResponse 从订单持久化模型创建公开 DTO。
func NewOrderResponse(model *models.Order) *OrderResponse {
	if model == nil {
		return nil
	}
	createdAt := ""
	if model.CreatedAt != nil {
		createdAt = model.CreatedAt.UTC().Truncate(time.Second).Format(time.RFC3339)
	}
	return &OrderResponse{
		ID: model.ID, ProductID: model.ProductID, ProductCode: model.ProductCode, ProductName: model.ProductName,
		SupplierID: model.SupplierID, SupplierCode: model.SupplierCode, SupplierName: model.SupplierName,
		UnitPrice: model.UnitPrice.String(), Quantity: model.Quantity, Amount: model.TotalAmount().String(),
		UserID: model.UserID, Status: model.OrderStatus(), StatusName: model.OrderStatus().String(),
		PaymentStatus: model.PaymentStatus, PaymentStatusName: model.PaymentStatus.String(),
		PaymentID: model.PaymentID, CreatedAt: createdAt,
	}
}

// OrderResponses 转换订单列表。
func OrderResponses(items []*models.Order) []*OrderResponse {
	result := make([]*OrderResponse, 0, len(items))
	for _, item := range items {
		if response := NewOrderResponse(item); response != nil {
			result = append(result, response)
		}
	}
	return result
}
