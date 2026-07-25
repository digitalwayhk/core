// 本文件提供当前服务供其他服务调用的 Public API 或入口 facade 能力。
package public

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

func orderPublicRoute(api interface{}, name, method string) *servertypes.RouterInfo {
	return router.DefaultRouterInfoWithOptions(api,
		router.WithServiceName(contract.OrderServiceName),
		router.WithPath("/api/"+contract.OrderServiceName+"/"+name),
		router.WithPathType(servertypes.PublicType),
		router.WithAuth(false),
		router.WithMethod(method),
		router.WithInternalCallers(contract.UserServiceName),
	)
}
