// Package business 提供 07 订单服务同步器默认远程权威库适配器。
package business

import (
	"context"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
)

// ModelRemoteOrderStore 使用 models 远程事务实现订单 upsert。
type ModelRemoteOrderStore struct{}

// Upsert 将订单事实幂等写入共享远程权威库。
func (ModelRemoteOrderStore) Upsert(_ context.Context, order *models.Order) (*models.Order, error) {
	var stored *models.Order
	err := models.RunRemoteTransaction(func(action models.DataAction) error {
		var err error
		stored, err = models.UpsertRemoteOrderWith(action, order)
		return err
	})
	return stored, err
}
