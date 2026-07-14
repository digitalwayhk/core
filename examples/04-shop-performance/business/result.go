package business

import "github.com/digitalwayhk/core/examples/04-shop-performance/models"

// OrderChange 是业务层返回的订单观察事件，不依赖 API DTO。
type OrderChange struct {
	Action string
	Order  *models.Order
}

// PaymentChange 同时返回订单变化和本次支付流水。
type PaymentChange struct {
	OrderChange
	Payment *models.PaymentRecord
}
