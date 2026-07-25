package casdoorrbacshop

import (
	"testing"

	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/contract"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShopServiceRegistersCompleteInheritanceExample(t *testing.T) {
	service := &ShopService{}

	assert.Equal(t, contract.ServiceName, service.ServiceName())
	assert.Len(t, service.Routers(), 38)
}

func TestShopServiceRegistersStableRoutesAfterPackageSplit(t *testing.T) {
	cfg := config.NewServiceDefaultConfig(contract.ServiceName, 38085)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	cfg.Transport.Internal = ""
	cfg.Transport.Fallback = nil

	ctx := router.NewServiceContextWithConfig(&ShopService{}, cfg)
	require.NotNil(t, ctx)
	ctx.SetRunState(true)
	t.Cleanup(func() { ctx.SetRunState(false) })

	routes := ctx.Router.GetRouters()
	require.Len(t, routes, 38)
	registered := make(map[string]*types.RouterInfo, len(routes))
	for _, info := range routes {
		require.NotNil(t, info)
		assert.Equal(t, contract.ServiceName, info.GetServiceName())
		if previous := registered[info.GetPath()]; previous != nil {
			t.Fatalf("路由路径重复: %s", info.GetPath())
		}
		registered[info.GetPath()] = info
	}

	assertRoute := func(path string, pathType types.ApiType, auth bool) {
		t.Helper()
		info := registered[path]
		require.NotNil(t, info, "未注册路由 %s", path)
		assert.Equal(t, pathType, info.GetPathType())
		assert.Equal(t, auth, info.GetAuth())
	}
	assertRoute("/api/manage/casdoorrbacshop/productmanage/add", types.ManageType, true)
	assertRoute("/api/manage/casdoorrbacshop/paymentrecordmanage/confirmpayment", types.ManageType, true)
	assertRoute("/api/manage/casdoorrbacshop/identityeventmanage/search", types.ManageType, true)
	assertRoute("/api/casdoorrbacshop/getproducts", types.PublicType, false)
	assertRoute("/api/casdoorrbacshop/getorders", types.PrivateType, true)
}
