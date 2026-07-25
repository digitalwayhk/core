package transaction

import "github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models"

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
