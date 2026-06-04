package public

import (
	"time"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// Ping 是一个 public API。目录 api/public 决定它无需登录即可访问。
type Ping struct {
	Name string `json:"name" desc:"调用者名称"`
}

// Parse 把请求 JSON 绑定到当前路由对象。
func (own *Ping) Parse(req types.IRequest) error {
	return req.Bind(own)
}

// Validation 在 Do 之前执行。这里给 name 提供一个默认值。
func (own *Ping) Validation(req types.IRequest) error {
	if own.Name == "" {
		own.Name = "core user"
	}
	return nil
}

// Do 执行业务逻辑。实际项目中应只写业务行为，不关心底层 HTTP 细节。
func (own *Ping) Do(req types.IRequest) (interface{}, error) {
	return map[string]interface{}{
		"message": "你好，" + own.Name,
		"service": req.ServiceName(),
		"time":    time.Now().Format(time.RFC3339),
	}, nil
}

// RouterInfo 使用默认路由信息，由包路径和结构名自动推导路由。
func (own *Ping) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfo(own)
}
