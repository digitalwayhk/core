// Package order 定义 07 订单水平扩展示例订单变化事件 DTO。
package order

import eventdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/event"

// OrderChanged 定义订单接收、同步、创建、状态和支付变化事件的载荷。
type OrderChanged struct {
	eventdto.Metadata
	OrderID       uint   `json:"orderID"`
	OrderRevision uint64 `json:"orderRevision"`
	UserID        uint   `json:"userID"`
	SupplierID    uint   `json:"supplierID"`
	ProductID     uint   `json:"productID"`
	OrderStatus   string `json:"orderStatus"`
	PaymentStatus string `json:"paymentStatus"`
}
