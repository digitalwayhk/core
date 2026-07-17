package orderservice

import (
	"context"
	"testing"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	publicapi "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/public"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestOrderServiceExposesOnlyConstrainedPublicAndManageRoutes(t *testing.T) {
	routers := (&Service{}).Routers()
	require.NotEmpty(t, routers)
	publicPaths := map[string]bool{}
	for _, api := range routers {
		info := api.RouterInfo()
		require.NotEqual(t, servertypes.PrivateType, info.GetPathType(), info.GetPath())
		if info.GetPathType() == servertypes.PublicType {
			require.Equal(t, []string{contract.UserServiceName}, info.GetInternalCallers(), info.GetPath())
			publicPaths[info.GetPath()] = true
		}
	}
	for _, api := range []servertypes.IRouter{&publicapi.CreateOrder{}, &publicapi.CancelOrder{}, &publicapi.CreatePayment{}, &publicapi.GetOrders{}, &publicapi.GetPaymentTypes{}} {
		require.True(t, publicPaths[api.RouterInfo().GetPath()], api.RouterInfo().GetPath())
	}
	require.Len(t, publicPaths, 5)
}

func TestOrderManageAuthenticationAllowsOnlyPlatformAdmin(t *testing.T) {
	service := &Service{}
	err := service.OnAuthRequest(context.Background(), servertypes.AuthRequestArgs{
		PathType: servertypes.ManageType,
		Identity: servertypes.AuthIdentity{UID: "buyer"},
	})
	require.Error(t, err)
	err = service.OnAuthRequest(context.Background(), servertypes.AuthRequestArgs{
		PathType: servertypes.ManageType,
		Identity: servertypes.AuthIdentity{UID: contract.PlatformAdminUserID},
	})
	require.NoError(t, err)
}
