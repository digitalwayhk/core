// 本文件验证新增内部通知接线仍尊重既有显式缓存 bypass 配置。
package router_test

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/server/routecache"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/stretchr/testify/require"
)

func TestInternalNotificationPreservesExplicitCacheBypass(t *testing.T) {
	name := uniqueServiceName("notification-bypass")
	cfg := testServiceConfig(name, 31234)
	cfg.RouteCache.Mode = "shared"
	cfg.RouteCache.Redis.Addr = "127.0.0.1:1"
	cfg.RouteCache.Redis.OnUnavailable = "bypass"
	var sc *router.ServiceContext
	require.NotPanics(t, func() { sc = router.NewServiceContextWithConfig(&instrumentedService{name: name}, cfg) })
	t.Cleanup(func() { sc.SetRunState(false) })
	require.Equal(t, routecache.StateBypass, sc.RouteCacheManager.State())
}
