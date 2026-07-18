package orderservice

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	publicapi "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/public"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

type cacheRecorder struct {
	enabled []string
}

func (r *cacheRecorder) EnableRoute(path string, _ time.Duration) error {
	r.enabled = append(r.enabled, path)
	return nil
}
func (*cacheRecorder) Get(string, interface{}) (interface{}, bool, error) { return nil, false, nil }
func (*cacheRecorder) Set(string, interface{}, interface{}, time.Duration) error {
	return nil
}
func (*cacheRecorder) Delete(string, interface{}) error { return nil }
func (*cacheRecorder) DeleteRoute(string) error         { return nil }

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

func TestOrderAuthorityPublicRoutesDoNotEnableRouteCache(t *testing.T) {
	recorder := &cacheRecorder{}
	api := &publicapi.GetPaymentTypes{}
	api.RouterInfo().SetCacheManager(contract.OrderServiceName, recorder)
	require.Empty(t, recorder.enabled, api.RouterInfo().GetPath())
}
