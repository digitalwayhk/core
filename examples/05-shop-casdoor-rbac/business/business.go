// Package business 是示例 05 业务层的兼容门面。
//
// 实现按 basedata、transaction 和 identity 分包；根包保留稳定的构造函数，
// 供现有 API 和测试渐进迁移。
package business

import (
	basedatabusiness "github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/business/basedata"
	identitybusiness "github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/business/identity"
	transactionbusiness "github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/business/transaction"
)

type (
	ProductService       = basedatabusiness.ProductService
	SupplierService      = basedatabusiness.SupplierService
	PaymentTypeService   = basedatabusiness.PaymentTypeService
	OrderService         = transactionbusiness.OrderService
	PaymentService       = transactionbusiness.PaymentService
	OrderChange          = transactionbusiness.OrderChange
	PaymentChange        = transactionbusiness.PaymentChange
	IdentityEventService = identitybusiness.IdentityEventService
)

var (
	NewProductService       = basedatabusiness.NewProductService
	NewSupplierService      = basedatabusiness.NewSupplierService
	NewPaymentTypeService   = basedatabusiness.NewPaymentTypeService
	NewOrderService         = transactionbusiness.NewOrderService
	NewPaymentService       = transactionbusiness.NewPaymentService
	NewIdentityEventService = identitybusiness.NewIdentityEventService
)
