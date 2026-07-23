// 本文件定义 06 微服务示例订单域对外传递的 DTO 能力。
package order

import (
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/shopspring/decimal"
)

// SupplierOrder 定义本文件能力使用的核心结构。
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
