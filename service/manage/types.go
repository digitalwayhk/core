package manage

import (
	"strings"

	"github.com/digitalwayhk/core/pkg/server/router"
	st "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

func RouterInfo(own interface{}) *st.RouterInfo {
	pack, tname := router.GetRouterPackAndTypeName(own)
	info := router.NewRouterInfo(own, pack, tname)
	name := strings.ToLower(utils.GetTypeName(own))
	index := strings.Index(name, "[")
	if index > 0 {
		name = name[:index]
	}
	ioh := own.(st.IPackRouterHook)
	info.Path = "/api/manage/" + info.ServiceName + "/" + strings.ToLower(utils.GetTypeName(ioh.GetInstance())) + "/" + name
	info.Auth = true
	info.PathType = st.ManageType
	return info
}
