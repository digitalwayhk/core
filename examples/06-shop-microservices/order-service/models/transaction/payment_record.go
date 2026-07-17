package transaction

import (
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/common"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
)

type PaymentRecord struct {
	*common.BusinessModel
	OrderID       uint            `gorm:"not null;index" json:"orderID"`
	PaymentTypeID uint            `gorm:"not null;index" json:"paymentTypeID"`
	Attempt       uint            `gorm:"not null;uniqueIndex:idx_payment_attempt" json:"attempt"`
	PaymentID     string          `gorm:"not null;uniqueIndex;uniqueIndex:idx_payment_attempt" json:"paymentID"`
	Amount        decimal.Decimal `json:"amount"`
	Status        int             `json:"status"`
}

func NewPaymentRecord() *PaymentRecord {
	return &PaymentRecord{BusinessModel: common.NewBusinessModel()}
}

func (p *PaymentRecord) NewModel() {
	if p.BusinessModel == nil || p.OrderServiceModel == nil || p.Model == nil {
		p.BusinessModel = common.NewBusinessModel()
	}
}

func (p *PaymentRecord) GetHash() string {
	return utils.HashCodes(strconv.FormatUint(uint64(p.OrderID), 10), strconv.FormatUint(uint64(p.Attempt), 10), strings.TrimSpace(p.PaymentID))
}
