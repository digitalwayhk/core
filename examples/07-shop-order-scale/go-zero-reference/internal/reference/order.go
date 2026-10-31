// 本文件独立声明与 07 既有表兼容的业务字段，不引用 Core 模型。
package reference

import (
	"github.com/shopspring/decimal"
	"time"
)

// Order 是原生对照服务直接交给 GORM 的记录。
type Order struct {
	Record
	ID                 uint            `gorm:"primaryKey;autoIncrement:false" json:"orderID"`
	RequestID          string          `gorm:"not null;index:idx_order_request,unique" json:"requestID"`
	RequestFingerprint string          `gorm:"not null" json:"requestFingerprint"`
	OrderRevision      uint64          `gorm:"not null" json:"orderRevision"`
	UserID             uint            `gorm:"not null;index:idx_order_request,unique;index" json:"userID"`
	SupplierID         uint            `gorm:"not null;index" json:"supplierID"`
	ProductID          uint            `gorm:"not null;index" json:"productID"`
	SupplierCode       string          `json:"supplierCode"`
	SupplierName       string          `json:"supplierName"`
	ProductCode        string          `json:"productCode"`
	ProductName        string          `json:"productName"`
	UnitPrice          decimal.Decimal `json:"unitPrice"`
	Quantity           int             `json:"quantity"`
	TotalAmount        decimal.Decimal `json:"totalAmount"`
	Recipient          string          `json:"recipient"`
	Phone              string          `json:"phone"`
	Region             string          `json:"region"`
	AddressDetail      string          `json:"addressDetail"`
	AddressID          uint            `json:"addressID"`
	PaymentStatus      string          `gorm:"index" json:"paymentStatus"`
	CurrentPaymentID   string          `gorm:"index" json:"currentPaymentID"`
	OrderStatus        string          `gorm:"index" json:"orderStatus"`
	AcceptedAt         time.Time       `json:"acceptedAt"`
	SyncedAt           *time.Time      `json:"syncedAt"`
}
