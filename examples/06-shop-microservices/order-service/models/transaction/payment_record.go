// 本文件定义当前服务交易事实、Outbox、Inbox 或投影模型能力。
package transaction

import (
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/common"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
)

// PaymentRecord 定义本文件能力使用的核心结构。
type PaymentRecord struct {
	*common.BusinessModel
	OrderID       uint            `gorm:"not null;index" json:"orderID"`
	PaymentTypeID uint            `gorm:"not null;index" json:"paymentTypeID"`
	Attempt       uint            `gorm:"not null;uniqueIndex:idx_payment_attempt" json:"attempt"`
	PaymentID     string          `gorm:"not null;uniqueIndex;uniqueIndex:idx_payment_attempt" json:"paymentID"`
	Amount        decimal.Decimal `json:"amount"`
	Status        int             `json:"status"`
}

// NewPaymentRecord 执行本文件能力对应的业务操作。
func NewPaymentRecord() *PaymentRecord {
	return &PaymentRecord{BusinessModel: common.NewBusinessModel()}
}

// NewModel 实现本类型在当前服务边界中的行为。
func (p *PaymentRecord) NewModel() {
	if p.BusinessModel == nil || p.OrderServiceModel == nil || p.Model == nil {
		p.BusinessModel = common.NewBusinessModel()
	}
}

// GetHash 实现本类型在当前服务边界中的行为。
func (p *PaymentRecord) GetHash() string {
	return utils.HashCodes(strconv.FormatUint(uint64(p.OrderID), 10), strconv.FormatUint(uint64(p.Attempt), 10), strings.TrimSpace(p.PaymentID))
}
