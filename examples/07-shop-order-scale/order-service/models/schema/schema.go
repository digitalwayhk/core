// Package schema 集中声明 07 订单服务本地库和远程权威库的建表能力。
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

// EnsureStorage 确保本地 pending/outbox 和远程订单权威表都已创建。
func EnsureStorage() error {
	ensureOnce.Do(func() {
		for _, model := range []interface{}{transaction.NewLocalPendingOrder(), transaction.NewOutbox()} {
			if err := store.EnsureLocalModel(model); err != nil {
				ensureErr = err
				return
			}
		}
		for _, model := range []interface{}{transaction.NewOrder(), basedata.NewOrderRule(), basedata.NewPaymentType()} {
			if err := store.EnsureRemoteModel(model); err != nil {
				ensureErr = err
				return
			}
		}
	})
	return ensureErr
}
