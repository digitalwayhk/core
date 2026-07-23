// Package business 提供 07 供应商服务商品资料业务能力。
package business

import "github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models"

// ListProducts 读取商品资料列表。
func ListProducts(id uint, enabledOnly bool) ([]*models.Product, error) {
	if err := models.EnsureStorage(); err != nil {
		return nil, err
	}
	return models.ListProducts(id, enabledOnly)
}
