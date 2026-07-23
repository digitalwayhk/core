// Package business 提供 07 供应商服务供应商资料业务能力。
package business

import "github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models"

// ListSuppliers 读取供应商资料列表。
func ListSuppliers(enabledOnly bool) ([]*models.Supplier, error) {
	if err := models.EnsureStorage(); err != nil {
		return nil, err
	}
	return models.ListSuppliers(enabledOnly)
}
