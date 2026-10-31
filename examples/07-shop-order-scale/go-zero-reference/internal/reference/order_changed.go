// 本文件声明订单事件事实，保持事件 ID、订单版本和买家归属。
package reference

import "time"

type orderChanged struct {
	SchemaVersion     int       `json:"schemaVersion"`
	EventID           string    `json:"eventID"`
	EventType         string    `json:"eventType"`
	Subject           string    `json:"subject"`
	TraceID           string    `json:"traceID"`
	ServiceName       string    `json:"serviceName"`
	ServiceInstanceID string    `json:"serviceInstanceID"`
	OccurredAt        time.Time `json:"occurredAt"`
	OrderID           uint      `json:"orderID"`
	OrderRevision     uint64    `json:"orderRevision"`
	UserID            uint      `json:"userID"`
	SupplierID        uint      `json:"supplierID"`
	ProductID         uint      `json:"productID"`
	OrderStatus       string    `json:"orderStatus"`
	PaymentStatus     string    `json:"paymentStatus"`
}
