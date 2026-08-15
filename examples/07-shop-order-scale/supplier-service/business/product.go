// Package business 提供 07 供应商服务商品资料业务能力。
package business

import (
	supplierdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/supplier"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models"
)

// ListProducts 读取供应商有效的可下单商品，并补齐供应商快照。
func ListProducts(id uint, enabledOnly bool) ([]*supplierdto.Product, error) {
	if err := models.EnsureStorage(); err != nil {
		return nil, err
	}
	items, err := models.ListProducts(id, enabledOnly)
	if err != nil {
		return nil, err
	}
	result := make([]*supplierdto.Product, 0, len(items))
	for _, item := range items {
		if item == nil || item.SupplierID == 0 {
			continue
		}
		supplier, findErr := models.FindSupplierByID(item.SupplierID)
		if findErr != nil || supplier == nil || !supplier.Enabled {
			continue
		}
		result = append(result, &supplierdto.Product{
			ID:           item.ID,
			SupplierID:   item.SupplierID,
			SupplierCode: supplier.Code,
			SupplierName: supplier.Name,
			Code:         item.Code,
			Name:         item.Name,
			Price:        item.Price,
			Enabled:      item.Enabled,
			TraceID:      item.TraceID,
		})
	}
	return result, nil
}
