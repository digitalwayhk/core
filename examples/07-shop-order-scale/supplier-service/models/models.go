// Package models 是 07 供应商服务模型层的兼容门面。
package models

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models/basedata"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models/internal/store"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models/projection"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models/schema"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

type (
	// Supplier 是供应商模型别名。
	Supplier = basedata.Supplier

	// Product 是商品模型别名。
	Product = basedata.Product

	// SupplierOrder 是供应商订单投影模型别名。
	SupplierOrder = projection.SupplierOrder
)

var (
	// NewSupplier 创建供应商模型。
	NewSupplier = basedata.NewSupplier

	// NewProduct 创建商品模型。
	NewProduct = basedata.NewProduct

	// NewSupplierOrder 创建供应商订单投影模型。
	NewSupplierOrder = projection.NewSupplierOrder

	// ListSuppliers 读取供应商列表。
	ListSuppliers = basedata.ListSuppliers

	// FindSupplierByID 按业务 ID 读取供应商资料。
	FindSupplierByID = basedata.FindSupplierByID

	// ListProducts 读取商品列表。
	ListProducts = basedata.ListProducts

	// ListSupplierOrders 读取供应商订单投影。
	ListSupplierOrders = projection.ListSupplierOrders
)

// EnsureStorage 确保供应商服务本地权威库完成建表。
func EnsureStorage() error { return schema.EnsureStorage() }

// RunTransaction 在供应商服务本地权威库执行事务。
func RunTransaction(operation func(persistencetypes.IDataAction) error) error {
	return store.RunTransaction(schema.EnsureStorage, operation)
}
