package models

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
)

// Order 保存用户、商品和价格快照，并记录当前支付与撤销状态。
type Order struct {
	*entity.Model
	ProductID     uint            `json:"productID" desc:"商品 ID"`
	ProductName   string          `json:"productName" desc:"商品名称快照"`
	UnitPrice     decimal.Decimal `json:"unitPrice" desc:"商品单价快照"`
	Quantity      int             `json:"quantity" desc:"购买数量"`
	UserID        string          `json:"userID" desc:"用户 ID"`
	Status        OrderStatus     `json:"status" desc:"订单状态"`
	PaymentStatus PaymentStatus   `json:"paymentStatus" desc:"支付状态"`
	PaymentID     uint            `json:"paymentID" desc:"当前支付流水 ID"`
}

// NewOrder 创建未支付的正常订单。
func NewOrder() *Order {
	return &Order{Model: entity.NewModel(), Status: OrderStatusNormal, PaymentStatus: PaymentStatusUnpaid}
}

// NewModel 供 ModelList 反射创建订单时初始化基础模型。
func (own *Order) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}

// GetHash 以用户、商品和 UTC 秒级创建时间生成唯一哈希。
func (own *Order) GetHash() string {
	if own.Model == nil || own.CreatedAt == nil || strings.TrimSpace(own.UserID) == "" || own.ProductID == 0 {
		if own.Model != nil {
			return own.Hashcode
		}
		return ""
	}
	createdAt := own.CreatedAt.UTC().Truncate(time.Second).Format(time.RFC3339)
	key := strings.TrimSpace(own.UserID) + ":" + strconv.FormatUint(uint64(own.ProductID), 10) + ":" + createdAt
	return utils.HashCodes(key)
}

// TotalAmount 返回订单价格快照计算出的应付金额。
func (own *Order) TotalAmount() decimal.Decimal {
	return own.UnitPrice.Mul(decimal.NewFromInt(int64(own.Quantity)))
}

// NewValidationError 创建可安全返回给调用方的中文校验错误。
func NewValidationError(message string) error {
	return servertypes.NewPublicError(servertypes.ErrorKindValidation, servertypes.PublicCodeValidation, message, errors.New(message))
}

// NewBusinessError 创建可安全返回给调用方的中文业务错误。
func NewBusinessError(message string) error {
	return servertypes.NewPublicError(servertypes.ErrorKindBusiness, servertypes.PublicCodeBusiness, message, errors.New(message))
}
