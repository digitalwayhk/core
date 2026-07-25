package rest

import (
	"errors"
	"net/http"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// NewExternalRouterHandler 将已注册 Router 的原始 REST 安全链挂到其他 http.ServeMux。
// 它只接受 ServiceRouter 中真实存在的路径，不创建第二套认证或执行逻辑。
func NewExternalRouterHandler(
	service *router.ServiceRouter,
	info *types.RouterInfo,
) (http.Handler, error) {
	if service == nil || service.Service == nil || service.Service.Config == nil || info == nil {
		return nil, errors.New("外部 Router 上下文无效")
	}
	path := strings.TrimSpace(info.GetPath())
	registered := service.GetRouter(path)
	if path == "" || registered == nil {
		return nil, errors.New("外部 Router 未注册")
	}
	info = registered

	var handler http.Handler = http.HandlerFunc(RouteHandler(service))
	if info.GetAuth() {
		auth, authType := resolveRouteAuthPolicy(service, path)
		handler = authRequestHandler(service.Service, info, authType, handler)
		handler = internalJWTAuthorize(auth.AccessSecret, authType, handler)
	}
	handler = externalRateLimitHandler(service.Service, info, handler)
	methodChecked := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != info.GetMethod() {
			w.Header().Set("Allow", info.GetMethod())
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		handler.ServeHTTP(w, request)
	})
	return securityHeaders(methodChecked), nil
}
