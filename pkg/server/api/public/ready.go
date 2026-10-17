// 本文件提供独立就绪检查，存活检查仍由 /api/health 负责。
package public

import (
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"net/http"
	"time"
)

// Ready 只检查本进程各服务自身准入，不递归查询下游依赖。
type Ready struct{}

// Parse 就绪检查不接受业务输入。
func (*Ready) Parse(types.IRequest) error { return nil }

// Validation 健康探针不依赖业务身份。
func (*Ready) Validation(types.IRequest) error { return nil }

// Do 对未就绪稳定实例返回安全的 HTTP 503 公共错误。
func (*Ready) Do(req types.IRequest) (interface{}, error) {
	if gate, ok := req.(interface{ CheckServiceAdmission() error }); ok {
		if err := gate.CheckServiceAdmission(); err != nil {
			return nil, err
		}
	}
	if err := router.CheckProcessAdmission(); err != nil {
		return nil, types.NewPublicError(types.ErrorKindUnavailable, types.PublicCodeUnavailable, "service unavailable", err)
	}
	return &HealthResponse{Status: "ready", Timestamp: time.Now().Format(time.RFC3339)}, nil
}

// RouterInfo 注册与 liveness 分离的只读探针。
func (r *Ready) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(r, router.WithMethod(http.MethodGet), router.WithPath("/api/ready"), withHealthEndpointRateLimit())
}
