// Package common 提供 07 订单服务 Manage API 的管理员权限能力。
package common

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// AdminOnly 校验当前请求是否来自平台管理员。
func AdminOnly(req servertypes.IRequest) error {
	uid, _ := req.GetUser()
	if uid != contract.PlatformAdminUserID {
		return contract.ErrForbidden
	}
	return nil
}
