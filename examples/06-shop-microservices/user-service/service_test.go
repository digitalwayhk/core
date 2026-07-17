package userservice

import (
	"testing"

	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

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
