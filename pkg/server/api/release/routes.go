// 本文件集中声明随服务自动注册的系统级发布路由。
package release

import (
	"github.com/digitalwayhk/core/pkg/server/api/private"
	"github.com/digitalwayhk/core/pkg/server/api/public"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// Routers 返回当前版本自动注册的系统级路由实例。
func Routers() []types.IRouter {
	return []types.IRouter{
		&public.TestToken{},
		&public.QueryConfig{},
		&public.TransportStats{},
		&public.QueryRouters{},
		&public.IpWhiteList{},
		&private.ModifyConfig{},
		// &ppv.ModifyRemoteDBConfig{},
		// &ppu.GetRemoteDBConfig{},
		&public.QueryService{},
		&public.GetMenu{},
		&public.Health{},
		&public.CasdoorCallback{},
		&public.CasdoorWebhook{},
		&public.Refresh{},
		&public.CasdoorConfig{},
		//&public.Statistics{},
		&public.RuntimeTopology{},
		&public.RuntimeService{},
		&public.AIProvider{},
		&public.SaveAIProvider{},
		&public.InternalOpenAPI{},
	}
}
