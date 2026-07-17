// Package order 保存 Order Service 的稳定传输结构。
package order

import (
	"time"

	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/shopspring/decimal"
)

type Order struct {
	ID             uint                        `json:"id"`
	OrderRevision  uint64                      `json:"orderRevision"`
	UserID         uint                        `json:"userID"`
	SupplierID     uint                        `json:"supplierID"`
	ProductID      uint                        `json:"productID"`
	Product        supplierdto.ProductSnapshot `json:"product"`
	Address        userdto.AddressSnapshot     `json:"address"`
	Quantity       int                         `json:"quantity"`
	TotalAmount    decimal.Decimal             `json:"totalAmount"`
	PaymentStatus  int                         `json:"paymentStatus"`
	CurrentPayment string                      `json:"currentPaymentID,omitempty"`
	OrderStatus    int                         `json:"orderStatus"`
	CreatedAt      time.Time                   `json:"createdAt"`
	UpdatedAt      time.Time                   `json:"updatedAt"`
}

// SupplierOrder 是 Supplier Service 的永久只读履约投影 DTO。
type SupplierOrder struct {
	OrderID        uint                    `json:"orderID"`
	OrderRevision  uint64                  `json:"orderRevision"`
	SupplierID     uint                    `json:"supplierID"`
	ProductID      uint                    `json:"productID"`
	SupplierCode   string                  `json:"supplierCode"`
	SupplierName   string                  `json:"supplierName"`
	ProductCode    string                  `json:"productCode"`
	ProductName    string                  `json:"productName"`
	UnitPrice      decimal.Decimal         `json:"unitPrice"`
	Quantity       int                     `json:"quantity"`
	TotalAmount    decimal.Decimal         `json:"totalAmount"`
	PaymentStatus  int                     `json:"paymentStatus"`
	OrderStatus    int                     `json:"orderStatus"`
	Address        userdto.AddressSnapshot `json:"address"`
	OrderCreatedAt time.Time               `json:"orderCreatedAt"`
	OrderUpdatedAt time.Time               `json:"orderUpdatedAt"`
}

type PaymentType struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Code    string `json:"code"`
	Enabled bool   `json:"enabled"`
}

type PaymentRecord struct {
	ID            uint            `json:"id"`
	OrderID       uint            `json:"orderID"`
	PaymentTypeID uint            `json:"paymentTypeID"`
	Attempt       uint            `json:"attempt"`
	PaymentID     string          `json:"paymentID"`
	Amount        decimal.Decimal `json:"amount"`
	Status        int             `json:"status"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}
