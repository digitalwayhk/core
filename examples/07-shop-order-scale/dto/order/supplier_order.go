// Package order 定义 07 供应商侧订单投影 DTO。
package order

import "github.com/shopspring/decimal"

// SupplierOrder 定义供应商服务本地订单投影快照。
type SupplierOrder struct {
	OrderID       uint            `json:"orderID"`
	SupplierID    uint            `json:"supplierID"`
	ProductID     uint            `json:"productID"`
	UserID        uint            `json:"userID"`
	Quantity      int             `json:"quantity"`
	TotalAmount   decimal.Decimal `json:"totalAmount"`
	OrderStatus   string          `json:"orderStatus"`
	PaymentStatus string          `json:"paymentStatus"`
	TraceID       string          `json:"traceID"`
}
