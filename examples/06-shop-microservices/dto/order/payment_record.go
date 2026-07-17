package order

import (
	"time"

	"github.com/shopspring/decimal"
)

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
