// 本文件定义 06 微服务示例订单域对外传递的 DTO 能力。
package order

import (
	"time"

	"github.com/shopspring/decimal"
)

// PaymentRecord 定义本文件能力使用的核心结构。
type PaymentRecord struct {
	ID            uint            `json:"id"`
	PaymentID     string          `json:"paymentID"`
	OrderID       uint            `json:"orderID"`
	PaymentTypeID uint            `json:"paymentTypeID"`
	Amount        decimal.Decimal `json:"amount"`
	Status        int             `json:"status"`
	Attempt       uint            `json:"attempt"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}
