// Package models 是 Order Service 模型层的兼容门面。
package models

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/basedata"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/common"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/internal/store"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/schema"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/transaction"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

type (
	OrderServiceModel = common.OrderServiceModel
	BaseDataModel     = common.BaseDataModel
	BusinessModel     = common.BusinessModel
	Order             = transaction.Order
	PaymentRecord     = transaction.PaymentRecord
	Outbox            = transaction.Outbox
	OutboxStore       = transaction.OutboxStore
	PaymentType       = basedata.PaymentType
)

const (
	OrderStatusNormal     = transaction.OrderStatusNormal
	OrderStatusCancelling = transaction.OrderStatusCancelling
	OrderStatusCancelled  = transaction.OrderStatusCancelled

	PaymentStatusUnpaid     = transaction.PaymentStatusUnpaid
	PaymentStatusProcessing = transaction.PaymentStatusProcessing
	PaymentStatusPaid       = transaction.PaymentStatusPaid
	PaymentStatusFailed     = transaction.PaymentStatusFailed
	PaymentStatusRefunding  = transaction.PaymentStatusRefunding
	PaymentStatusRefunded   = transaction.PaymentStatusRefunded
)

var (
	NewOrderServiceModel       = common.NewOrderServiceModel
	NewBaseDataModel           = common.NewBaseDataModel
	NewBusinessModel           = common.NewBusinessModel
	NewOrder                   = transaction.NewOrder
	FindByIdempotency          = transaction.FindByIdempotency
	FindByIdempotencyWith      = transaction.FindByIdempotencyWith
	FindOrder                  = transaction.FindOrder
	FindOrderWith              = transaction.FindOrderWith
	ListOrders                 = transaction.ListOrders
	NewPaymentRecord           = transaction.NewPaymentRecord
	ListPaymentRecords         = transaction.ListPaymentRecords
	ListPaymentRecordsWith     = transaction.ListPaymentRecordsWith
	FindPaymentRecord          = transaction.FindPaymentRecord
	FindPaymentByPaymentID     = transaction.FindPaymentByPaymentID
	FindPaymentByPaymentIDWith = transaction.FindPaymentByPaymentIDWith
	NewOutbox                  = transaction.NewOutbox
	NewOutboxRecord            = transaction.NewOutboxRecord
	PendingOutbox              = transaction.PendingOutbox
	MarkOutboxPublished        = transaction.MarkOutboxPublished
	ToDTO                      = transaction.ToDTO
	PaymentToDTO               = transaction.PaymentToDTO
	ChangeEvent                = transaction.ChangeEvent
	NewPaymentType             = basedata.NewPaymentType
	ListPaymentTypes           = basedata.ListPaymentTypes
	FindPaymentType            = basedata.FindPaymentType
	FindPaymentTypeWith        = basedata.FindPaymentTypeWith
	PaymentTypeInUse           = basedata.PaymentTypeInUse
	PaymentTypeInUseWith       = basedata.PaymentTypeInUseWith
	SavePaymentType            = basedata.SavePaymentType
	DeletePaymentType          = basedata.DeletePaymentType
)

func EnsureStorage() error { return schema.EnsureStorage() }

func RunTransaction(operation func(persistencetypes.IDataAction) error) error {
	return store.RunInTransaction(schema.EnsureStorage, operation)
}
