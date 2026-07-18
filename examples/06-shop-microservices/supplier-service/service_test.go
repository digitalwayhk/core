// 本文件验证当前服务启动配置、路由注册和服务边界能力。
package supplierservice

import (
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	publicapi "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/api/public"
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

// TestSupplierServiceExposesManageAndConstrainedPublicRoutesOnly 验证当前场景的业务闭环和边界行为。
func TestSupplierServiceExposesManageAndConstrainedPublicRoutesOnly(t *testing.T) {
	routers := (&Service{}).Routers()
	require.NotEmpty(t, routers)

	for _, api := range routers {
		info := api.RouterInfo()
		require.NotEqual(t, servertypes.PrivateType, info.GetPathType(), info.GetPath())
		require.NotContains(t, info.GetPackPath(), "/api/call")
	}

	suppliers := (&publicapi.GetSuppliers{}).RouterInfo()
	require.Equal(t, []string{contract.UserServiceName}, suppliers.GetInternalCallers())

	products := (&publicapi.GetProducts{}).RouterInfo()
	require.Equal(t, []string{contract.OrderServiceName, contract.UserServiceName}, products.GetInternalCallers())
}

// TestSupplierAuthorityPublicRoutesDoNotEnableRouteCache 验证当前场景的业务闭环和边界行为。
func TestSupplierAuthorityPublicRoutesDoNotEnableRouteCache(t *testing.T) {
	for _, api := range []servertypes.IRouter{&publicapi.GetSuppliers{}, &publicapi.GetProducts{}} {
		recorder := &cacheRecorder{}
		api.RouterInfo().SetCacheManager(contract.SupplierServiceName, recorder)
		require.Empty(t, recorder.enabled, api.RouterInfo().GetPath())
	}
}
