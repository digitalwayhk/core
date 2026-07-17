// Package manage 是 User Service 后台管理 API 的兼容门面。
//
// 实现分布在 common、basedata 和 transaction 子包中，根包保持路由注册入口。
package manage

import basedatamanage "github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/api/manage/basedata"

type (
	UserManage     = basedatamanage.UserManage
	SetUserEnabled = basedatamanage.SetUserEnabled
	AddressManage  = basedatamanage.AddressManage
)

var (
	NewUserManage     = basedatamanage.NewUserManage
	NewSetUserEnabled = basedatamanage.NewSetUserEnabled
	NewAddressManage  = basedatamanage.NewAddressManage
)
