package manage

import (
	"strings"

	"github.com/digitalwayhk/core/pkg/server/router"
	st "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

func RouterInfo(own interface{}) *st.RouterInfo {
	pack, tname := router.GetRouterPackAndTypeName(own)
	name := strings.ToLower(utils.GetTypeName(own))
	index := strings.Index(name, "[")
	if index > 0 {
		name = name[:index]
	}
	ioh := own.(st.IPackRouterHook)
	manageName := strings.ToLower(utils.GetTypeName(ioh.GetInstance()))
	return router.NewRouterInfoWithOptions(own, pack, tname,
		router.WithPathResolver(func(info *st.RouterInfo) string {
			return "/api/manage/" + info.GetServiceName() + "/" + manageName + "/" + name
		}),
		router.WithAuth(true),
		router.WithPathType(st.ManageType),
	)
}
