package rest

import (
	"net/http"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// NewExternalRouterHandler 为已注册 Router 构造可挂到任意 http.ServeMux 的 Handler。
// 与 REST Server handers 对齐：方法校验、外部速率限制、安全 Header、IP 白名单、
// Parse/Validation/Exec、以及需认证时的 JWT 校验与 OnAuth 请求钩子。
// 不接受任意路径，也不绕过 Router 执行链。
func NewExternalRouterHandler(sc *router.ServiceContext, info *types.RouterInfo) http.Handler {
	return newExternalRouterHandler(sc, info, nil, nil, "")
}

// NewExternalRouterHandlerWithAuthPolicy 为受信任的统一入口显式指定认证策略。
// Router 的方法校验、限流、OnAuth 与执行链保持不变；仅 JWT 的 secret 和认证类型
// 由入口策略提供。普通服务端口应继续使用 NewExternalRouterHandler。
func NewExternalRouterHandlerWithAuthPolicy(
	sc *router.ServiceContext,
	info *types.RouterInfo,
	authAuthority *router.ServiceContext,
	auth config.AuthSecret,
	authType types.AuthType,
) http.Handler {
	return newExternalRouterHandler(sc, info, authAuthority, &auth, authType)
}

func newExternalRouterHandler(
	sc *router.ServiceContext,
	info *types.RouterInfo,
	authAuthority *router.ServiceContext,
	authOverride *config.AuthSecret,
	authTypeOverride types.AuthType,
) http.Handler {
	if sc == nil || sc.Router == nil || info == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writePublicErrorContract(w, types.NewPublicError(types.ErrorKindUnavailable, 0, "", nil).PublicErrorContract())
		})
	}
	var routeAuthType *types.AuthType
	if authOverride != nil {
		routeAuthType = &authTypeOverride
	}
	var handler http.Handler = http.HandlerFunc(routeHandler(sc.Router, routeAuthType))
	if info.GetAuth() {
		auth, authType := resolveRouteAuthPolicy(sc.Router, info.GetPath())
		if authOverride != nil {
			auth = *authOverride
			authType = authTypeOverride
		}
		if authAuthority != nil {
			handler = authRequestHandlerWithAuthority(sc, authAuthority, info, authType, handler)
		} else {
			handler = authRequestHandler(sc, info, authType, handler)
		}
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
	serviceName := sc.Config.Name
	if sc.Service != nil && sc.Service.Name != "" {
		serviceName = sc.Service.Name
	}
	return runtimeHTTPMetrics(serviceName, info.GetPath(), securityHeaders(methodChecked))
}
