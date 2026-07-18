// Package models 是 Supplier Service 模型层的兼容门面。
package models

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/basedata"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/common"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/internal/store"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/schema"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/transaction"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

type (
	SupplierServiceModel = common.SupplierServiceModel
	BaseDataModel        = common.BaseDataModel
	BusinessModel        = common.BusinessModel
	Supplier             = basedata.Supplier
	Product              = basedata.Product
	SupplierOrder        = transaction.SupplierOrder
	Outbox               = transaction.Outbox
	OutboxStore          = transaction.OutboxStore
	Inbox                = transaction.Inbox
)

var (
	NewSupplierServiceModel = common.NewSupplierServiceModel
	NewBaseDataModel        = common.NewBaseDataModel
	NewBusinessModel        = common.NewBusinessModel
	NewSupplier             = basedata.NewSupplier
	NewProduct              = basedata.NewProduct
	FindSupplier            = basedata.FindSupplier
	FindSupplierByID        = basedata.FindSupplierByID
	ListSuppliers           = basedata.ListSuppliers
	ListProducts            = basedata.ListProducts
	FindProduct             = basedata.FindProduct
	ProductChangedPayload   = basedata.ProductChangedPayload
	SupplierChangedPayload  = basedata.SupplierChangedPayload
	EventID                 = basedata.EventID
	NewSupplierOrder        = transaction.NewSupplierOrder
	ApplyOrderEvent         = transaction.ApplyOrderEvent
	FindSupplierOrder       = transaction.FindSupplierOrder
	DeleteProduct           = transaction.DeleteProduct
	DeleteSupplier          = transaction.DeleteSupplier
	NewOutbox               = transaction.NewOutbox
	NewProductOutbox        = transaction.NewProductOutbox
	PendingOutbox           = transaction.PendingOutbox
	MarkOutboxPublished     = transaction.MarkOutboxPublished
	NewInbox                = transaction.NewInbox
	ProcessInbox            = transaction.ProcessInbox
)

func EnsureStorage() error { return schema.EnsureStorage() }

func RunTransaction(operation func(persistencetypes.IDataAction) error) error {
	return store.RunInTransaction(schema.EnsureStorage, operation)
}

func dataAction() persistencetypes.IDataAction { return store.Get() }

func search(model interface{}, size int) *persistencetypes.SearchItem {
	return store.NewSearch(model, size)
}
