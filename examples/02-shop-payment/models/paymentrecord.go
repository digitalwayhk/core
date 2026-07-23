package models

import (
	"strconv"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
)

// PaymentRecord 保存一次支付尝试及支付类型快照。
type PaymentRecord struct {
	*entity.Model
	OrderID         uint            `json:"orderID" desc:"订单 ID"`
	UserID          string          `json:"userID" desc:"用户 ID"`
	PaymentTypeID   uint            `json:"paymentTypeID" desc:"支付类型 ID"`
	PaymentTypeCode string          `json:"paymentTypeCode" desc:"支付类型编码快照"`
	PaymentTypeName string          `json:"paymentTypeName" desc:"支付类型名称快照"`
	Amount          decimal.Decimal `json:"amount" desc:"支付金额"`
	Attempt         int             `json:"attempt" desc:"支付尝试次数"`
	Status          PaymentStatus   `json:"status" desc:"支付状态"`
	PaidAt          *time.Time      `json:"paidAt,omitempty" desc:"支付完成时间"`
	RefundedAt      *time.Time      `json:"refundedAt,omitempty" desc:"退款完成时间"`
}

// NewPaymentRecord 创建支付中的新流水。
func NewPaymentRecord() *PaymentRecord {
	return &PaymentRecord{Model: entity.NewModel(), Status: PaymentStatusPending}
}

// NewModel 供 ModelList 反射创建支付流水时初始化基础模型。
func (own *PaymentRecord) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}

// GetHash 使用订单 ID 和支付尝试次数保证每次尝试唯一。
func (own *PaymentRecord) GetHash() string {
	if own.OrderID == 0 || own.Attempt <= 0 {
		if own.Model != nil {
			return own.Hashcode
		}
		return ""
	}
	return utils.HashCodes(strconv.FormatUint(uint64(own.OrderID), 10) + ":" + strconv.Itoa(own.Attempt))
}

// NormalizeUserID 清理支付流水中的可信用户标识。
func (own *PaymentRecord) NormalizeUserID() {
	own.UserID = strings.TrimSpace(own.UserID)
}
