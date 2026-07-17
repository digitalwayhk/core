package schema

import (
	"sync"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/basedata"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/internal/store"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/transaction"
)

var (
	once       sync.Once
	storageErr error
)

func EnsureStorage() error {
	once.Do(func() {
		for _, model := range []interface{}{transaction.NewOrder(), basedata.NewPaymentType(), transaction.NewPaymentRecord(), transaction.NewOutbox()} {
			if err := store.EnsureModel(model); err != nil {
				storageErr = err
				return
			}
		}
	})
	return storageErr
}
