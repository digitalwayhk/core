// Package private 提供 07 用户入口服务买家 Private API 路由能力。
package private

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

func userPrivateRoute(api interface{}, name, method string) *servertypes.RouterInfo {
	return router.DefaultRouterInfoWithOptions(api,
		router.WithServiceName(contract.UserServiceName),
		router.WithPath("/api/"+contract.UserServiceName+"/"+name),
		router.WithPathType(servertypes.PrivateType),
		router.WithAuth(true),
		router.WithMethod(method),
	)
}
