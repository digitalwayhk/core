// Package manage 是 07 供应商服务后台管理 API 的兼容门面。
package manage

import basedatamanage "github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/api/manage/basedata"

type (
	// SupplierManage 是供应商资料 Manage 别名。
	SupplierManage = basedatamanage.SupplierManage

	// ProductManage 是商品资料 Manage 别名。
	ProductManage = basedatamanage.ProductManage
)

var (
	// NewSupplierManage 创建供应商资料 Manage。
	NewSupplierManage = basedatamanage.NewSupplierManage

	// NewProductManage 创建商品资料 Manage。
	NewProductManage = basedatamanage.NewProductManage
)
