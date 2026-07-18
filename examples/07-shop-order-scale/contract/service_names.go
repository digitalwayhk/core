// Package contract 定义 07 订单水平扩展示例的稳定跨服务契约。
//
// 该包只能保存常量和错误，不能引用 API、DTO、business、models 或框架包。
package contract

const (
	// UserServiceName 是普通用户入口服务的稳定服务名。
	UserServiceName = "shop-user"

	// SupplierServiceName 是供应商权威服务的稳定服务名。
	SupplierServiceName = "shop-supplier"

	// OrderServiceName 是订单权威服务的稳定服务名；水平扩展副本共享该逻辑名称。
	OrderServiceName = "shop-order"
)
