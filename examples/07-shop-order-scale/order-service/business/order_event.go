// Package business 提供 07 订单服务订单变化事件的组装与 Outbox 写入能力。
package business

import (
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	eventdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/event"
	orderdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/order"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
)

// BuildOrderChangedEvent 将远程订单事实转换成跨服务订单变化事件 DTO。
func BuildOrderChangedEvent(order *models.Order, eventID, eventType string) orderdto.OrderChanged {
	occurredAt := time.Now().UTC()
	if order != nil && order.CreatedAt != nil {
		occurredAt = *order.CreatedAt
	}
	payload := orderdto.OrderChanged{
		Metadata: eventdto.Metadata{
			SchemaVersion: contract.EventSchemaVersion,
			EventID:       strings.TrimSpace(eventID),
			EventType:     strings.TrimSpace(eventType),
			Subject:       contract.SubjectOrderChanged,
			OccurredAt:    occurredAt,
		},
	}
	if order == nil {
		return payload
	}
	payload.TraceID = strings.TrimSpace(order.TraceID)
	payload.ServiceName = strings.TrimSpace(order.ServiceName)
	payload.ServiceInstanceID = strings.TrimSpace(order.ServiceInstanceID)
	payload.OrderID = order.ID
	payload.OrderRevision = order.OrderRevision
	payload.UserID = order.UserID
	payload.SupplierID = order.SupplierID
	payload.ProductID = order.ProductID
	payload.OrderStatus = order.OrderStatus
	payload.PaymentStatus = order.PaymentStatus
	return payload
}
