// Package manage 是示例 05 后台管理 API 的兼容门面。
//
// 实现分布在 common、basedata、transaction 和 audit 子包中，根包保持
// 路由注册与旧测试的稳定入口。
package manage

import (
	auditmanage "github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/api/manage/audit"
	basedatamanage "github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/api/manage/basedata"
	commonmanage "github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/api/manage/common"
	transactionmanage "github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/api/manage/transaction"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

type (
	IDoBefore[T persistencetypes.IModel]      = commonmanage.IDoBefore[T]
	IDoAfter[T persistencetypes.IModel]       = commonmanage.IDoAfter[T]
	ShopManage[T persistencetypes.IModel]     = commonmanage.ShopManage[T]
	BaseDataManage[T persistencetypes.IModel] = basedatamanage.BaseDataManage[T]
	BusinessManage[T persistencetypes.IModel] = transactionmanage.BusinessManage[T]

	ProductManage       = basedatamanage.ProductManage
	SupplierManage      = basedatamanage.SupplierManage
	PaymentTypeManage   = basedatamanage.PaymentTypeManage
	OrderManage         = transactionmanage.OrderManage
	PaymentRecordManage = transactionmanage.PaymentRecordManage
	IdentityEventManage = auditmanage.IdentityEventManage
)

const (
	shopManageMaxPageSize          = commonmanage.ShopManageMaxPageSize
	businessManageMaxPageSize      = transactionmanage.BusinessManageMaxPageSize
	orderManageMaxPageSize         = transactionmanage.OrderManageMaxPageSize
	paymentRecordManageMaxPageSize = transactionmanage.PaymentRecordManageMaxPageSize
	identityEventMaxPageSize       = auditmanage.IdentityEventMaxPageSize
)

// NewShopManage 创建绑定最终 owner 的服务级 Manage。
func NewShopManage[T persistencetypes.IModel](owner interface{}) *ShopManage[T] {
	return commonmanage.NewShopManage[T](owner)
}

var (
	NewProductManage       = basedatamanage.NewProductManage
	NewSupplierManage      = basedatamanage.NewSupplierManage
	NewPaymentTypeManage   = basedatamanage.NewPaymentTypeManage
	NewOrderManage         = transactionmanage.NewOrderManage
	NewPaymentRecordManage = transactionmanage.NewPaymentRecordManage
	NewIdentityEventManage = auditmanage.NewIdentityEventManage
)
