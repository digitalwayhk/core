// Package contract 定义多服务商城的稳定跨服务契约。
// 该包不得引用业务包或框架包，避免服务间循环依赖。
package contract

const (
	UserServiceName     = "shop-user"
	SupplierServiceName = "shop-supplier"
	OrderServiceName    = "shop-order"
)

const PlatformAdminUserID = "platform-admin"
