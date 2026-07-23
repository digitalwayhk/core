package public

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/internal/openapidoc"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// InternalOpenAPI 提供需要 ServerManage 身份的完整业务接口文档。
type InternalOpenAPI struct {
	api.ServerArgs
	Service string `json:"service"`
}

func (own *InternalOpenAPI) Parse(req types.IRequest) error {
	if err := own.ServerArgs.Parse(req); err != nil {
		return err
	}
	own.Service = strings.TrimSpace(req.GetValue("service"))
	return nil
}

func (own *InternalOpenAPI) Do(req types.IRequest) (interface{}, error) {
	httpRequest, ok := req.(types.IRequestHttp)
	if !ok {
		return nil, errors.New("OpenAPI 仅支持 HTTP 请求")
	}

	serviceRouters, err := selectOpenAPIServiceRouters(own.Service)
	if err != nil {
		return nil, err
	}
	return openapidoc.Generate(httpRequest.GetHttpRequest(), openapidoc.InternalAudience, serviceRouters...), nil
}

func (own *InternalOpenAPI) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfoWithOptions(
		own,
		router.WithPath("/api/internal/openapi"),
		router.WithPathType(types.ServerManagerType),
		router.WithAuth(true),
		withSystemEndpointRateLimit(),
		router.WithResponseHandler(internalOpenAPIResponse),
	)
}

func selectOpenAPIServiceRouters(serviceName string) ([]*router.ServiceRouter, error) {
	contexts := router.GetContexts()
	if serviceName != "" {
		context := contexts[serviceName]
		if context == nil || context.Router == nil {
			return nil, errors.New("未找到指定服务：" + serviceName)
		}
		return []*router.ServiceRouter{context.Router}, nil
	}

	names := make([]string, 0, len(contexts))
	for name, context := range contexts {
		if context != nil && context.Router != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	serviceRouters := make([]*router.ServiceRouter, 0, len(names))
	for _, name := range names {
		serviceRouters = append(serviceRouters, contexts[name].Router)
	}
	return serviceRouters, nil
}

func internalOpenAPIResponse(w http.ResponseWriter, _ *http.Request, res types.IResponse) {
	w.Header().Set("Cache-Control", "private, no-store")
	if res.GetSuccess() {
		httpx.OkJson(w, res.GetData())
		return
	}
	httpx.OkJson(w, res)
}
