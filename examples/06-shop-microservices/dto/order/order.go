package order

import (
	"time"

	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/shopspring/decimal"
)

type Order struct {
	ID               uint                        `json:"id"`
	OrderRevision    uint64                      `json:"orderRevision"`
	UserID           uint                        `json:"userID"`
	SupplierID       uint                        `json:"supplierID"`
	ProductID        uint                        `json:"productID"`
	Product          supplierdto.ProductSnapshot `json:"product"`
	SupplierCode     string                      `json:"supplierCode"`
	SupplierName     string                      `json:"supplierName"`
	ProductCode      string                      `json:"productCode"`
	ProductName      string                      `json:"productName"`
	UnitPrice        decimal.Decimal             `json:"unitPrice"`
	Quantity         int                         `json:"quantity"`
	TotalAmount      decimal.Decimal             `json:"totalAmount"`
	PaymentStatus    int                         `json:"paymentStatus"`
	OrderStatus      int                         `json:"orderStatus"`
	CurrentPaymentID string                      `json:"currentPaymentID,omitempty"`
	CurrentPayment   string                      `json:"currentPayment,omitempty"`
	Address          userdto.AddressSnapshot     `json:"address"`
	CreatedAt        time.Time                   `json:"createdAt"`
	UpdatedAt        time.Time                   `json:"updatedAt"`
}
