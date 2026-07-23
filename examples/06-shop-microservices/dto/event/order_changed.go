// 本文件定义 06 微服务示例事件通道使用的跨服务消息 DTO 能力。
package event

import (
	"time"

	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/shopspring/decimal"
)

// OrderChanged 定义本文件能力使用的核心结构。
type OrderChanged struct {
	Metadata
	Action        string                  `json:"action,omitempty"`
	OrderID       uint                    `json:"orderID"`
	OrderRevision uint64                  `json:"orderRevision"`
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
	PaymentID     string                  `json:"paymentID,omitempty"`
	Address       userdto.AddressSnapshot `json:"address"`
	CreatedAt     time.Time               `json:"createdAt"`
	UpdatedAt     time.Time               `json:"updatedAt"`
}
