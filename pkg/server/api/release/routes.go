package release

import (
	"github.com/digitalwayhk/core/pkg/server/api/private"
	"github.com/digitalwayhk/core/pkg/server/api/public"
	"github.com/digitalwayhk/core/pkg/server/types"
)

func Routers() []types.IRouter {
	return []types.IRouter{
		&public.TestToken{},
		&public.QueryConfig{},
		&public.QueryRouters{},
		&public.Observe{},
		&public.Notify{},
		&public.Attach{},
		&public.IpWhiteList{},
		&private.ModifyConfig{},
		&private.SetServiceAddress{},
		// &ppv.ModifyRemoteDBConfig{},
		// &ppu.GetRemoteDBConfig{},
		&public.QueryService{},
		&public.GetMenu{},
		&public.Health{},
		&public.Callback{},
		&public.Casdoor{},
		&public.Statistics{},
	}
}
