// Package order 保存 Order Service 的稳定传输结构。
package order

import (
	"time"

	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/shopspring/decimal"
)

type Order struct {
	ID            uint                        `json:"id"`
	UserID        string                      `json:"userID"`
	Product       supplierdto.ProductSnapshot `json:"product"`
	Address       userdto.AddressSnapshot     `json:"address"`
	Quantity      int                         `json:"quantity"`
	TotalAmount   decimal.Decimal             `json:"totalAmount"`
	PaymentStatus int                         `json:"paymentStatus"`
	Status        int                         `json:"status"`
	CreatedAt     time.Time                   `json:"createdAt"`
}

// SupplierOrder 只暴露供应商履约所需字段，不包含完整收货地址。
type SupplierOrder struct {
	ID            uint            `json:"id"`
	ProductID     uint            `json:"productID"`
	ProductName   string          `json:"productName"`
	Quantity      int             `json:"quantity"`
	TotalAmount   decimal.Decimal `json:"totalAmount"`
	PaymentStatus int             `json:"paymentStatus"`
	Status        int             `json:"status"`
	CreatedAt     time.Time       `json:"createdAt"`
}

type PaymentType struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Code    string `json:"code"`
	Enabled bool   `json:"enabled"`
}

type PaymentRecord struct {
	ID        uint            `json:"id"`
	OrderID   uint            `json:"orderID"`
	Amount    decimal.Decimal `json:"amount"`
	Status    int             `json:"status"`
	CreatedAt time.Time       `json:"createdAt"`
}
