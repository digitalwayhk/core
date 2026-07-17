package schema

import (
	"sync"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/basedata"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/internal/store"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/transaction"
)

var (
	once       sync.Once
	storageErr error
)

func EnsureStorage() error {
	once.Do(func() {
		for _, model := range []interface{}{basedata.NewSupplier(), basedata.NewProduct(), transaction.NewSupplierOrder(), transaction.NewOutbox(), transaction.NewInbox()} {
			if err := store.EnsureModel(model); err != nil {
				storageErr = err
				return
			}
		}
	})
	return storageErr
}
