package models

import (
	"strconv"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
)

// Order 保存用户、商品、供应商和价格快照。
type Order struct {
	*BusinessModel
	ProductID     uint            `json:"productID" desc:"商品 ID"`
	ProductCode   string          `json:"productCode" desc:"商品编码快照"`
	ProductName   string          `json:"productName" desc:"商品名称快照"`
	SupplierID    uint            `json:"supplierID" desc:"供应商 ID"`
	SupplierCode  string          `json:"supplierCode" desc:"供应商编码快照"`
	SupplierName  string          `json:"supplierName" desc:"供应商名称快照"`
	UnitPrice     decimal.Decimal `json:"unitPrice" desc:"商品单价快照"`
	Quantity      int             `json:"quantity" desc:"购买数量"`
	UserID        string          `json:"userID" desc:"用户 ID"`
	PaymentStatus PaymentStatus   `json:"paymentStatus" desc:"支付状态"`
	PaymentID     uint            `json:"paymentID" desc:"当前支付流水 ID"`
}

// NewOrder 创建正常且未支付的订单。
func NewOrder() *Order {
	return &Order{BusinessModel: NewBusinessModel(int(OrderStatusNormal)), PaymentStatus: PaymentStatusUnpaid}
}

// NewModel 供 ModelList 反射创建订单时初始化完整继承链。
func (own *Order) NewModel() {
	if own.BusinessModel == nil || own.ShopModel == nil || own.Model == nil {
		own.BusinessModel = NewBusinessModel(int(OrderStatusNormal))
	}
}

// OrderStatus 返回强类型订单状态。
func (own *Order) OrderStatus() OrderStatus { return OrderStatus(own.Status) }

// GetHash 使用可信用户摘要、商品和 UTC 秒构造可按用户扫描的唯一键。
func (own *Order) GetHash() string {
	if own.Model == nil || own.CreatedAt == nil || strings.TrimSpace(own.UserID) == "" || own.ProductID == 0 {
		if own.Model != nil {
			return own.Hashcode
		}
		return ""
	}
	createdAt := own.CreatedAt.UTC().Truncate(time.Second).Unix()
	return orderUserPrefix(own.UserID) + strconv.FormatUint(uint64(own.ProductID), 10) + ":" + strconv.FormatInt(createdAt, 10)
}

// IsSyncAfterDelete 声明订单同步到 SQLite 后自动删除 Badger 本地副本。
func (own *Order) IsSyncAfterDelete() bool { return true }

func orderUserPrefix(userID string) string {
	return utils.HashCodes(strings.TrimSpace(userID)) + ":"
}

// TotalAmount 返回订单价格快照计算出的金额。
func (own *Order) TotalAmount() decimal.Decimal {
	return own.UnitPrice.Mul(decimal.NewFromInt(int64(own.Quantity)))
}
