// 本文件实现使用 ServerManage 认证域保护的内部 OpenAPI 文档路由。
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

// Parse 解析可选的 service 文档筛选条件。
func (own *InternalOpenAPI) Parse(req types.IRequest) error {
	if err := own.ServerArgs.Parse(req); err != nil {
		return err
	}
	own.Service = strings.TrimSpace(req.GetValue("service"))
	return nil
}

// Do 生成全部服务或指定服务的内部 OpenAPI 文档。
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

// RouterInfo 将内部文档注册为需要 ServerManage 身份的固定路由。
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

// selectOpenAPIServiceRouters 按稳定服务名顺序选择文档生成所需的路由表。
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

// internalOpenAPIResponse 成功时输出原始文档，失败时保持统一公共错误契约。
func internalOpenAPIResponse(w http.ResponseWriter, _ *http.Request, res types.IResponse) {
	w.Header().Set("Cache-Control", "private, no-store")
	if res.GetSuccess() {
		httpx.OkJson(w, res.GetData())
		return
	}
	contract := types.ResolvePublicError(res.GetError())
	if setter, ok := res.(types.ISetPublicError); ok {
		setter.SetPublicError(contract.Code, contract.Message)
	}
	httpx.WriteJson(w, contract.HTTPStatus, res)
}
