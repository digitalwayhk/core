// 本文件定义 06 微服务示例跨服务共享的稳定契约常量和错误能力。
package contract

// EventSchemaVersion 提供本文件能力需要的导出定义。
const EventSchemaVersion = 1

// EventVersion 为旧代码迁移保留；新事件统一使用 EventSchemaVersion。
const EventVersion = EventSchemaVersion

const (
	EventProductChanged     = "shop.product.changed"
	EventSupplierChanged    = "shop.supplier.changed"
	EventOrderCreated       = "shop.order.created"
	EventOrderStatusChanged = "shop.order.status.changed"
	EventPaymentChanged     = "shop.payment.changed"
	EventPaymentTypeChanged = "shop.payment-type.changed"
)

const (
	SubjectProductChanged     = "shop.events.product.changed"
	SubjectSupplierChanged    = "shop.events.supplier.changed"
	SubjectOrderCreated       = "shop.events.order.created"
	SubjectOrderStatusChanged = "shop.events.order.status.changed"
	SubjectPaymentChanged     = "shop.events.payment.changed"
	SubjectPaymentTypeChanged = "shop.events.payment-type.changed"
)

// 旧订单事件名仅用于后续服务迁移期间保持源码可编译。
const (
	EventOrderChanged   = EventOrderStatusChanged
	SubjectOrderChanged = SubjectOrderStatusChanged
)
