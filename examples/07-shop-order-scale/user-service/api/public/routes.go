// Package public 提供 07 用户入口服务对外 facade Public API 路由能力。
package public

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

func userPublicRoute(api interface{}, name, method string) *servertypes.RouterInfo {
	return router.DefaultRouterInfoWithOptions(api,
		router.WithServiceName(contract.UserServiceName),
		router.WithPath("/api/"+contract.UserServiceName+"/"+name),
		router.WithPathType(servertypes.PublicType),
		router.WithAuth(false),
		router.WithMethod(method),
	)
}
