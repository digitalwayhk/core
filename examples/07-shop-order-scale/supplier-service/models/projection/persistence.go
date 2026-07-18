// Package projection 提供 07 供应商订单投影持久化能力。
package projection

import "github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models/internal/store"

// ListSupplierOrders 读取指定供应商的订单投影。
func ListSupplierOrders(supplierID uint) ([]*SupplierOrder, error) {
	var items []*SupplierOrder
	query := store.NewSearch(NewSupplierOrder(), 1000)
	query.AddWhereN("SupplierID", supplierID)
	query.AddSortN("OrderID", false)
	err := store.Get().Load(query, &items)
	return items, err
}
