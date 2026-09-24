// 本文件验证 Core SystemManage 注册角色与权限控制面页面。
package server

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSystemManageRegistersRoleManagementPages(t *testing.T) {
	routes := (&SystemManage{}).Routers()
	paths := make([]string, 0, len(routes))
	for _, route := range routes {
		paths = append(paths, route.RouterInfo().GetPath())
	}

	require.Contains(t, paths, "/api/manage/server/managerolemanage/view")
	require.Contains(t, paths, "/api/manage/server/managerolemanage/search")
	require.Contains(t, paths, "/api/manage/server/managerolepermissionmanage/view")
	require.Contains(t, paths, "/api/manage/server/managerolepermissionmanage/bindmenu")
}
