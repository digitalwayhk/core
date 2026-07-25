package rest

import (
	"net/http"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// NewExternalRouterHandler 为已注册 Router 构造可挂到任意 http.ServeMux 的 Handler。
// 与 REST Server handers 对齐：方法校验、外部速率限制、安全 Header、IP 白名单、
// Parse/Validation/Exec、以及需认证时的 JWT 校验与 OnAuth 请求钩子。
// 不接受任意路径，也不绕过 Router 执行链。
func NewExternalRouterHandler(sc *router.ServiceContext, info *types.RouterInfo) http.Handler {
	if sc == nil || sc.Router == nil || info == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writePublicErrorContract(w, types.NewPublicError(types.ErrorKindUnavailable, 0, "", nil).PublicErrorContract())
		})
	}
	var handler http.Handler = http.HandlerFunc(RouteHandler(sc.Router))
	if info.GetAuth() {
		auth, authType := resolveRouteAuthPolicy(sc.Router, info.GetPath())
		handler = authRequestHandler(sc, info, authType, handler)
		handler = internalJWTAuthorize(auth.AccessSecret, authType, handler)
	}
	rateLimited := externalRateLimitHandler(sc, info, handler)
	methodChecked := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != info.GetMethod() {
			w.Header().Set("Allow", info.GetMethod())
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		rateLimited.ServeHTTP(w, r)
	})
	return securityHeaders(methodChecked)
}
