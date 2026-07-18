// 本文件验证当前服务启动配置、路由注册和服务边界能力。
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

// EnableRoute 实现本类型在当前服务边界中的行为。
func (r *cacheRecorder) EnableRoute(path string, _ time.Duration) error {
	r.enabled = append(r.enabled, path)
	return nil
}

// Get 实现本类型在当前服务边界中的行为。
func (*cacheRecorder) Get(string, interface{}) (interface{}, bool, error) { return nil, false, nil }

// Set 实现本类型在当前服务边界中的行为。
func (*cacheRecorder) Set(string, interface{}, interface{}, time.Duration) error {
	return nil
}

// Delete 实现本类型在当前服务边界中的行为。
func (*cacheRecorder) Delete(string, interface{}) error { return nil }

// DeleteRoute 实现本类型在当前服务边界中的行为。
func (*cacheRecorder) DeleteRoute(string) error { return nil }

// TestOrderServiceExposesOnlyConstrainedPublicAndManageRoutes 验证当前场景的业务闭环和边界行为。
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

// TestOrderManageAuthenticationAllowsOnlyPlatformAdmin 验证当前场景的业务闭环和边界行为。
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

// TestOrderAuthorityPublicRoutesDoNotEnableRouteCache 验证当前场景的业务闭环和边界行为。
func TestOrderAuthorityPublicRoutesDoNotEnableRouteCache(t *testing.T) {
	recorder := &cacheRecorder{}
	api := &publicapi.GetPaymentTypes{}
	api.RouterInfo().SetCacheManager(contract.OrderServiceName, recorder)
	require.Empty(t, recorder.enabled, api.RouterInfo().GetPath())
}
