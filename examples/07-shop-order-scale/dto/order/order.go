// Package order 定义 07 订单水平扩展示例订单域对外传递的 DTO。
package order

import (
	"time"

	supplierdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/supplier"
	userdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/user"
	"github.com/shopspring/decimal"
)

// Order 定义跨服务返回的订单快照，不嵌入持久化模型。
type Order struct {
	OrderID           uint                        `json:"orderID"`
	OrderRevision     uint64                      `json:"orderRevision"`
	UserID            uint                        `json:"userID"`
	SupplierID        uint                        `json:"supplierID"`
	ProductID         uint                        `json:"productID"`
	Product           supplierdto.ProductSnapshot `json:"product"`
	SupplierCode      string                      `json:"supplierCode"`
	SupplierName      string                      `json:"supplierName"`
	ProductCode       string                      `json:"productCode"`
	ProductName       string                      `json:"productName"`
	UnitPrice         decimal.Decimal             `json:"unitPrice"`
	Quantity          int                         `json:"quantity"`
	TotalAmount       decimal.Decimal             `json:"totalAmount"`
	OrderStatus       string                      `json:"orderStatus"`
	PaymentStatus     string                      `json:"paymentStatus"`
	CurrentPaymentID  string                      `json:"currentPaymentID,omitempty"`
	CurrentPayment    string                      `json:"currentPayment,omitempty"`
	Address           userdto.AddressSnapshot     `json:"address"`
	TraceID           string                      `json:"traceID"`
	ServiceName       string                      `json:"serviceName"`
	ServiceInstanceID string                      `json:"serviceInstanceID"`
	AcceptedAt        time.Time                   `json:"acceptedAt"`
	SyncedAt          *time.Time                  `json:"syncedAt,omitempty"`
	CreatedAt         time.Time                   `json:"createdAt"`
	UpdatedAt         time.Time                   `json:"updatedAt"`
}
