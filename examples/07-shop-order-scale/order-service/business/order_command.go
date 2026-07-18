// Package business 定义 07 订单服务本地接单命令。
package business

import (
	"errors"
	"strings"

	"github.com/shopspring/decimal"
)

// CreateOrderCommand 表达 order 实例本地可靠接收订单所需的最小业务事实。
type CreateOrderCommand struct {
	OrderID            uint
	UserID             uint
	RequestID          string
	RequestFingerprint string
	SupplierID         uint
	ProductID          uint
	SupplierCode       string
	SupplierName       string
	ProductCode        string
	ProductName        string
	UnitPrice          decimal.Decimal
	Quantity           int
	Recipient          string
	Phone              string
	Region             string
	AddressDetail      string
	AddressID          uint
	TraceID            string
	ServiceName        string
	ServiceInstanceID  string
	ServiceInstanceIP  string
}

func (c CreateOrderCommand) validate() error {
	if c.OrderID == 0 || c.UserID == 0 || c.SupplierID == 0 || c.ProductID == 0 || strings.TrimSpace(c.RequestID) == "" || strings.TrimSpace(c.RequestFingerprint) == "" || c.Quantity <= 0 {
		return errors.New("订单命令参数不完整")
	}
	if !c.UnitPrice.IsPositive() {
		return errors.New("商品单价必须大于 0")
	}
	return nil
}
