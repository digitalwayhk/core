// Package schema 集中声明 07 订单服务 MySQL 远程权威库的建表能力。
package schema

import (
	"sync"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/basedata"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/internal/store"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/transaction"
)

var (
	ensureOnce sync.Once
	ensureErr  error
)

// EnsureStorage 确保远程 MySQL 权威库完成建表。
func EnsureStorage() error {
	ensureOnce.Do(func() {
		for _, model := range []interface{}{transaction.NewOrder(), transaction.NewOutbox(), basedata.NewOrderRule(), basedata.NewPaymentType()} {
			if err := store.EnsureRemoteModel(model); err != nil {
				ensureErr = err
				return
			}
		}
	})
	return ensureErr
}
