// 本文件定义当前服务交易事实、Outbox、Inbox 或投影模型能力。
package transaction

import (
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/common"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
)

// Order 定义本文件能力使用的核心结构。
type Order struct {
	*common.BusinessModel
	IdempotencyKey     string          `gorm:"not null;uniqueIndex" json:"idempotencyKey"`
	RequestFingerprint string          `gorm:"not null" json:"-"`
	OrderRevision      uint64          `gorm:"not null" json:"orderRevision"`
	UserID             uint            `gorm:"not null;index" json:"userID"`
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
	PaymentStatus      int             `json:"paymentStatus"`
	CurrentPaymentID   string          `gorm:"index" json:"currentPaymentID"`
	OrderStatus        int             `json:"orderStatus"`
}

// NewOrder 执行本文件能力对应的业务操作。
func NewOrder() *Order {
	return &Order{BusinessModel: common.NewBusinessModel(), OrderStatus: OrderStatusNormal, PaymentStatus: PaymentStatusUnpaid}
}

// NewModel 实现本类型在当前服务边界中的行为。
func (o *Order) NewModel() {
	if o.BusinessModel == nil || o.OrderServiceModel == nil || o.Model == nil {
		o.BusinessModel = common.NewBusinessModel()
	}
}

// GetHash 实现本类型在当前服务边界中的行为。
func (o *Order) GetHash() string { return utils.HashCodes(strings.TrimSpace(o.IdempotencyKey)) }
