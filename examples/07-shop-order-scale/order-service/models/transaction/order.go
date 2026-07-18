// Package transaction 定义 07 订单服务远程权威交易事实模型。
package transaction

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/common"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
)

// Order 保存所有 order 实例最终收敛到同一远程权威库的订单事实。
type Order struct {
	*common.RuntimeStampedModel
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

// NewOrder 创建默认订单事实模型。
func NewOrder() *Order {
	return &Order{
		RuntimeStampedModel: common.NewRuntimeStampedModel(),
		OrderRevision:       1,
		OrderStatus:         OrderStatusAccepted,
		PaymentStatus:       PaymentStatusUnpaid,
		AcceptedAt:          time.Now().UTC(),
	}
}

// NewModel 初始化持久化框架需要的嵌入模型。
func (o *Order) NewModel() {
	if o.RuntimeStampedModel == nil || o.ServiceBaseModel == nil || o.Model == nil {
		o.RuntimeStampedModel = common.NewRuntimeStampedModel()
	}
}

// GetHash 返回订单远程幂等唯一散列。
func (o *Order) GetHash() string {
	return utils.HashCodes(strings.TrimSpace(o.RequestID), strconv.FormatUint(uint64(o.UserID), 10))
}

// InsertWith 将订单事实写入指定事务。
func (o *Order) InsertWith(action persistencetypes.IDataAction) error {
	if err := o.validate(); err != nil {
		return err
	}
	o.SetHashcode(o.GetHash())
	return action.Insert(o)
}

// UpdateWith 更新指定事务中的订单事实。
func (o *Order) UpdateWith(action persistencetypes.IDataAction) error {
	if err := o.validate(); err != nil {
		return err
	}
	o.SetUpdatedAt(time.Now().UTC())
	o.SetHashcode(o.GetHash())
	return action.Update(o)
}

func (o *Order) validate() error {
	if o.UserID == 0 || o.SupplierID == 0 || o.ProductID == 0 || strings.TrimSpace(o.RequestID) == "" || strings.TrimSpace(o.RequestFingerprint) == "" || o.Quantity <= 0 {
		return errors.New("订单参数不完整")
	}
	if !o.UnitPrice.IsPositive() || !o.TotalAmount.IsPositive() {
		return errors.New("订单金额必须大于 0")
	}
	return nil
}
