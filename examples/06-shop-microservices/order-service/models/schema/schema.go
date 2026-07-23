// 本文件集中声明当前服务本地数据库建表 schema 能力。
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

// EnsureStorage 执行本文件能力对应的业务操作。
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
