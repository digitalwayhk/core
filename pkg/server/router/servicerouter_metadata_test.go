package router

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestServiceRouterLookupRejectsFrozenMetadataMutation(t *testing.T) {
	const path = "/api/test/frozen"
	info := &types.RouterInfo{
		Path:        path,
		ServiceName: "test",
		Method:      "POST",
		PathType:    types.PublicType,
	}
	info.Freeze("test")
	router := &ServiceRouter{allAPI: map[string]*types.RouterInfo{path: info}}
	info.Auth = true

	require.PanicsWithValue(t, "router metadata changed after registration", func() {
		router.GetRouter(path)
	})
}

func TestServiceRouterEnumerationRejectsFrozenMetadataMutation(t *testing.T) {
	const path = "/api/test/frozen"
	newRouter := func() (*ServiceRouter, *types.RouterInfo) {
		info := &types.RouterInfo{
			Path:        path,
			ServiceName: "test",
			Method:      "POST",
			PathType:    types.PublicType,
		}
		info.Freeze("test")
		return &ServiceRouter{
			allAPI:    map[string]*types.RouterInfo{path: info},
			publicAPI: map[string]*types.RouterInfo{path: info},
		}, info
	}

	allRouter, allInfo := newRouter()
	allInfo.Method = "GET"
	require.PanicsWithValue(t, "router metadata changed after registration", func() {
		allRouter.GetRouters()
	})

	typeRouter, typeInfo := newRouter()
	typeInfo.ServiceName = "changed"
	require.PanicsWithValue(t, "router metadata changed after registration", func() {
		typeRouter.GetTypeRouters(types.PublicType)
	})
}
