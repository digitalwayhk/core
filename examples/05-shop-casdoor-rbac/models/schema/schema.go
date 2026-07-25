// Package schema 统一组装示例 05 的持久化模型。
// 各业务子包不得反向依赖 schema，避免模型层形成循环引用。
package schema

import (
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/basedata"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/identity"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/internal/store"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/transaction"
)

// EnsureStorage 确保基础资料、交易数据和身份审计表均已初始化。
func EnsureStorage() error {
	models := []interface{}{
		basedata.NewSupplier(),
		basedata.NewProduct(),
		basedata.NewPaymentType(),
		transaction.NewOrder(),
		transaction.NewPaymentRecord(),
		identity.NewIdentityEventRecord(),
	}
	for _, model := range models {
		if err := store.EnsureModel(model); err != nil {
			return err
		}
	}
	return nil
}
