// 本文件集中声明当前服务本地数据库建表 schema 能力。
package schema

import (
	"sync"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models/basedata"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models/internal/store"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models/transaction"
)

var (
	once       sync.Once
	storageErr error
)

// EnsureStorage 执行本文件能力对应的业务操作。
func EnsureStorage() error {
	once.Do(func() {
		for _, model := range []interface{}{basedata.NewUser(), basedata.NewAddress(), transaction.NewInbox()} {
			if err := store.EnsureModel(model); err != nil {
				storageErr = err
				return
			}
		}
	})
	return storageErr
}
