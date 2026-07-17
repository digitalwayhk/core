// Package event 定义跨服务可靠控制事件载荷。
package event

import (
	"time"

	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/shopspring/decimal"
)

type Metadata struct {
	EventID       string    `json:"eventID"`
	SchemaVersion int       `json:"schemaVersion"`
	EventType     string    `json:"eventType"`
	OccurredAt    time.Time `json:"occurredAt"`
	SourceService string    `json:"sourceService"`
	AggregateID   string    `json:"aggregateID"`
	// Version 仅用于旧示例源码迁移，事件输出统一使用 SchemaVersion。
	Version int `json:"-"`
}

type ProductChanged struct {
	Metadata
	SupplierID uint   `json:"supplierID"`
	ProductID  uint   `json:"productID"`
	Action     string `json:"action"`
}

type SupplierChanged struct {
	Metadata
	SupplierID uint   `json:"supplierID"`
	Action     string `json:"action"`
}

// OrderChanged 是订单创建、订单状态和支付状态事件共享的完整快照。
type OrderChanged struct {
	Metadata
	OrderRevision uint64                  `json:"orderRevision"`
	OrderID       uint                    `json:"orderID"`
	UserID        uint                    `json:"userID"`
	SupplierID    uint                    `json:"supplierID"`
	ProductID     uint                    `json:"productID"`
	SupplierCode  string                  `json:"supplierCode"`
	SupplierName  string                  `json:"supplierName"`
	ProductCode   string                  `json:"productCode"`
	ProductName   string                  `json:"productName"`
	UnitPrice     decimal.Decimal         `json:"unitPrice"`
	Quantity      int                     `json:"quantity"`
	TotalAmount   decimal.Decimal         `json:"totalAmount"`
	PaymentStatus int                     `json:"paymentStatus"`
	OrderStatus   int                     `json:"orderStatus"`
	Address       userdto.AddressSnapshot `json:"address"`
	CreatedAt     time.Time               `json:"createdAt"`
	UpdatedAt     time.Time               `json:"updatedAt"`
	Action        string                  `json:"action"`
}
