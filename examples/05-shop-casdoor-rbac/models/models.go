// Package models 是示例 05 模型层的兼容门面。
//
// 新代码应按语义依赖 common、basedata、transaction 或 identity 子包；
// 根包继续导出旧名称，让 API 和外部示例可以渐进迁移。
package models

import (
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/basedata"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/common"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/identity"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/internal/store"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/schema"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/transaction"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

type (
	ShopModel           = common.ShopModel
	BaseDataModel       = common.BaseDataModel
	IBaseDataModel      = common.IBaseDataModel
	BusinessModel       = common.BusinessModel
	Supplier            = basedata.Supplier
	Product             = basedata.Product
	PaymentType         = basedata.PaymentType
	Order               = transaction.Order
	PaymentRecord       = transaction.PaymentRecord
	IdentityEventRecord = identity.IdentityEventRecord
	OrderStatus         = transaction.OrderStatus
	PaymentStatus       = transaction.PaymentStatus
)

const (
	OrderStatusNormal     = transaction.OrderStatusNormal
	OrderStatusCancelling = transaction.OrderStatusCancelling
	OrderStatusCancelled  = transaction.OrderStatusCancelled

	PaymentStatusUnpaid    = transaction.PaymentStatusUnpaid
	PaymentStatusPending   = transaction.PaymentStatusPending
	PaymentStatusPaid      = transaction.PaymentStatusPaid
	PaymentStatusFailed    = transaction.PaymentStatusFailed
	PaymentStatusRefunding = transaction.PaymentStatusRefunding
	PaymentStatusRefunded  = transaction.PaymentStatusRefunded
)

var (
	NewShopModel           = common.NewShopModel
	NewBaseDataModel       = common.NewBaseDataModel
	NewBusinessModel       = common.NewBusinessModel
	NewSupplier            = basedata.NewSupplier
	NewProduct             = basedata.NewProduct
	NewPaymentType         = basedata.NewPaymentType
	NewOrder               = transaction.NewOrder
	NewPaymentRecord       = transaction.NewPaymentRecord
	NewIdentityEventRecord = identity.NewIdentityEventRecord
	NewValidationError     = common.NewValidationError
	NewBusinessError       = common.NewBusinessError
)

// EnsureStorage 初始化本示例的全部模型表。
func EnsureStorage() error { return schema.EnsureStorage() }

// RunInTransaction 以独立的数据操作器执行一次业务事务。
func RunInTransaction(operation func(action persistencetypes.IDataAction) error) error {
	return store.RunInTransaction(schema.EnsureStorage, operation)
}
