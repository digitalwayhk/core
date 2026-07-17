// Package manage 是 Supplier Service 后台管理 API 的兼容门面。
package manage

import (
	basedatamanage "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/api/manage/basedata"
	transactionmanage "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/api/manage/transaction"
)

type (
	SupplierManage     = basedatamanage.SupplierManage
	ProductManage      = basedatamanage.ProductManage
	SetSupplierEnabled = basedatamanage.SetSupplierEnabled
	SetProductEnabled  = basedatamanage.SetProductEnabled
	OrderManage        = transactionmanage.OrderManage
)

var (
	NewSupplierManage     = basedatamanage.NewSupplierManage
	NewProductManage      = basedatamanage.NewProductManage
	NewSetSupplierEnabled = basedatamanage.NewSetSupplierEnabled
	NewSetProductEnabled  = basedatamanage.NewSetProductEnabled
	NewOrderManage        = transactionmanage.NewOrderManage
)
