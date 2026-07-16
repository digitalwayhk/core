package contract

const EventVersion = 1

const (
	EventProductChanged  = "shop.product.changed"
	EventSupplierChanged = "shop.supplier.changed"
	EventOrderChanged    = "shop.order.changed"
	EventPaymentChanged  = "shop.payment.changed"
)

const (
	SubjectProductChanged  = "shop.events.product.changed"
	SubjectSupplierChanged = "shop.events.supplier.changed"
	SubjectOrderChanged    = "shop.events.order.changed"
	SubjectPaymentChanged  = "shop.events.payment.changed"
)
