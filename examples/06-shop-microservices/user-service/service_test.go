// 本文件验证当前服务启动配置、路由注册和服务边界能力。
package userservice

import (
	"testing"

	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

// TestUserServiceRouteInventoryHasManageFacadesAndBuyerCommands 验证当前场景的业务闭环和边界行为。
func TestUserServiceRouteInventoryHasManageFacadesAndBuyerCommands(t *testing.T) {
	routers := (&Service{}).Routers()
	counts := map[servertypes.ApiType]int{}
	for _, api := range routers {
		counts[api.RouterInfo().GetPathType()]++
	}
	require.Equal(t, 3, counts[servertypes.PublicType])
	require.Equal(t, 4, counts[servertypes.PrivateType])
	require.GreaterOrEqual(t, counts[servertypes.ManageType], 1)
	for _, api := range routers {
		if api.RouterInfo().GetPathType() == servertypes.PrivateType {
			require.NotContains(t, api.RouterInfo().GetPath(), "address")
		}
	}
}
