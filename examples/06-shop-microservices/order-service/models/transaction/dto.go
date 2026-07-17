package transaction

import (
	"strconv"
	"time"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	eventdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
)

func modelTimes(model *entity.Model) (time.Time, time.Time) {
	created, updated := time.Time{}, time.Time{}
	if model != nil && model.CreatedAt != nil {
		created = *model.CreatedAt
	}
	if model != nil && model.UpdatedAt != nil {
		updated = *model.UpdatedAt
	}
	if updated.IsZero() {
		updated = created
	}
	return created, updated
}

func ToDTO(order *Order) *orderdto.Order {
	if order == nil {
		return nil
	}
	created, updated := modelTimes(order.Model)
	return &orderdto.Order{
		ID: order.ID, OrderRevision: order.OrderRevision, UserID: order.UserID, SupplierID: order.SupplierID, ProductID: order.ProductID,
		Product:  supplierdto.ProductSnapshot{ProductID: order.ProductID, SupplierID: order.SupplierID, SupplierCode: order.SupplierCode, SupplierName: order.SupplierName, ProductCode: order.ProductCode, ProductName: order.ProductName, UnitPrice: order.UnitPrice},
		Address:  userdto.AddressSnapshot{AddressID: order.AddressID, Recipient: order.Recipient, Phone: order.Phone, Region: order.Region, Detail: order.AddressDetail},
		Quantity: order.Quantity, TotalAmount: order.TotalAmount, PaymentStatus: order.PaymentStatus, CurrentPayment: order.CurrentPaymentID,
		OrderStatus: order.OrderStatus, CreatedAt: created, UpdatedAt: updated,
	}
}

func PaymentToDTO(record *PaymentRecord) *orderdto.PaymentRecord {
	if record == nil {
		return nil
	}
	created, updated := modelTimes(record.Model)
	return &orderdto.PaymentRecord{ID: record.ID, OrderID: record.OrderID, PaymentTypeID: record.PaymentTypeID, Attempt: record.Attempt, PaymentID: record.PaymentID, Amount: record.Amount, Status: record.Status, CreatedAt: created, UpdatedAt: updated}
}

func ChangeEvent(eventID, eventType, action string, order *Order) eventdto.OrderChanged {
	created, updated := modelTimes(order.Model)
	return eventdto.OrderChanged{
		Metadata:      eventdto.Metadata{EventID: eventID, SchemaVersion: contract.EventSchemaVersion, EventType: eventType, OccurredAt: time.Now().UTC(), SourceService: contract.OrderServiceName, AggregateID: strconv.FormatUint(uint64(order.ID), 10)},
		OrderRevision: order.OrderRevision, OrderID: order.ID, UserID: order.UserID, SupplierID: order.SupplierID, ProductID: order.ProductID,
		SupplierCode: order.SupplierCode, SupplierName: order.SupplierName, ProductCode: order.ProductCode, ProductName: order.ProductName,
		UnitPrice: order.UnitPrice, Quantity: order.Quantity, TotalAmount: order.TotalAmount, PaymentStatus: order.PaymentStatus, OrderStatus: order.OrderStatus,
		Address:   userdto.AddressSnapshot{AddressID: order.AddressID, Recipient: order.Recipient, Phone: order.Phone, Region: order.Region, Detail: order.AddressDetail},
		CreatedAt: created, UpdatedAt: updated, Action: action,
	}
}
