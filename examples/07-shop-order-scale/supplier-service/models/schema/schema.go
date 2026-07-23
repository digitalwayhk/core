// Package schema 集中声明 07 供应商服务本地权威库建表能力。
package schema

import (
	"sync"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models/basedata"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models/internal/store"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models/projection"
)

var (
	once       sync.Once
	storageErr error
)

// EnsureStorage 确保供应商、商品和订单投影表已创建。
func EnsureStorage() error {
	once.Do(func() {
		for _, model := range []interface{}{basedata.NewSupplier(), basedata.NewProduct(), projection.NewSupplierOrder()} {
			if err := store.EnsureModel(model); err != nil {
				storageErr = err
				return
			}
		}
	})
	return storageErr
}
