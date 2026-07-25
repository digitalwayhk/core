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

// Order 表示用户订单，并保存下单时的商品名称与价格快照。
type Order struct {
	*entity.Model
	ProductID   uint            `json:"productID" desc:"商品 ID"`
	Product     *Product        `json:"product,omitempty" gorm:"-"`
	ProductName string          `json:"productName" desc:"商品名称快照"`
	UnitPrice   decimal.Decimal `json:"unitPrice" desc:"商品单价快照"`
	Quantity    int             `json:"quantity" desc:"购买数量"`
	UserID      string          `json:"userID" desc:"用户 ID"`
}

// NewOrder 创建已初始化基础模型的订单。
func NewOrder() *Order {
	return &Order{Model: entity.NewModel()}
}

// NewModel 供 ModelList 在反射创建订单时初始化基础模型。
func (own *Order) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}

// GetHash 以用户、商品和创建时间的 UTC 秒级值生成订单唯一哈希。
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

// NewValidationError 创建可以安全返回给调用方的中文校验错误。
func NewValidationError(message string) error {
	return servertypes.NewPublicError(servertypes.ErrorKindValidation, servertypes.PublicCodeValidation, message, errors.New(message))
}

// NewBusinessError 创建可以安全返回给调用方的中文业务错误。
func NewBusinessError(message string) error {
	return servertypes.NewPublicError(servertypes.ErrorKindBusiness, servertypes.PublicCodeBusiness, message, errors.New(message))
}
