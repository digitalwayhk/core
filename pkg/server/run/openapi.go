package run

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/digitalwayhk/core/pkg/server/internal/openapidoc"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/zeromicro/go-zero/rest/httpx"
)

//go:embed swagger
var swagger embed.FS

func SwaggerHandler() (string, http.FileSystem) {
	sfsys, _ := fs.Sub(swagger, "swagger")
	return "/swagger/", http.FS(sfsys)
}

func OpenAPIHandler(service ...*router.ServiceRouter) (string, http.Handler) {
	return "/api/openapi", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpx.OkJson(w, GetOpenApi(r, service...))
	})
}

// GetOpenApi 返回可匿名访问的业务文档，不包含内部专用 Public 路由和调用方白名单。
func GetOpenApi(req *http.Request, services ...*router.ServiceRouter) interface{} {
	return openapidoc.Generate(req, openapidoc.ExternalAudience, services...)
}

// GetInternalOpenApi 返回完整业务文档，供内部兼容性检查和受保护的内部端点使用。
func GetInternalOpenApi(req *http.Request, services ...*router.ServiceRouter) interface{} {
	return openapidoc.Generate(req, openapidoc.InternalAudience, services...)
}
