// 本文件提供当前服务 Manage API 的公共权限、限域和横切 Hook 能力。
package common

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// AdminOnly 执行本文件能力对应的业务操作。
func AdminOnly(req servertypes.IRequest) error {
	uid, _ := req.GetUser()
	if uid != contract.PlatformAdminUserID {
		return contract.ErrForbidden
	}
	return nil
}

// AdminSearch 执行本文件能力对应的业务操作。
func AdminSearch(req servertypes.IRequest) (interface{}, error, bool) {
	if err := AdminOnly(req); err != nil {
		return nil, err, true
	}
	return nil, nil, false
}
