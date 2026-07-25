// Package public 提供 07 订单服务仅供内部服务调用的 Public API 路由能力。
package public

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

func orderPublicRoute(api interface{}, name, method string, callers ...string) *servertypes.RouterInfo {
	if len(callers) == 0 {
		callers = []string{contract.UserServiceName}
	}
	return router.DefaultRouterInfoWithOptions(api,
		router.WithServiceName(contract.OrderServiceName),
		router.WithPath("/api/"+contract.OrderServiceName+"/"+name),
		router.WithPathType(servertypes.PublicType),
		router.WithAuth(false),
		router.WithMethod(method),
		router.WithInternalCallers(callers...),
	)
}
