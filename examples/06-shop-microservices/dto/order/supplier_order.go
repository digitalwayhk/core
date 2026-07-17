package order

import (
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/shopspring/decimal"
)

type SupplierOrder struct {
	OrderID       uint                    `json:"orderID"`
	OrderRevision uint64                  `json:"orderRevision"`
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
}
