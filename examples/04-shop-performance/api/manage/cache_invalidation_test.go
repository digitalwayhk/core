package manage

import (
	"testing"
	"time"

	publicapi "github.com/digitalwayhk/core/examples/04-shop-performance/api/public"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

type manageCacheService struct{}

func (*manageCacheService) ServiceName() string { return "performanceshop" }
func (*manageCacheService) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{&publicapi.GetProducts{}, &publicapi.GetSuppliers{}, &publicapi.GetPaymentTypes{}}
}

type manageCacheRuntimeSpy struct{ deletedRoutes []string }

func (*manageCacheRuntimeSpy) EnableRoute(string, time.Duration) error { return nil }
func (*manageCacheRuntimeSpy) Get(string, interface{}) (interface{}, bool, error) {
	return nil, false, nil
}
func (*manageCacheRuntimeSpy) Set(string, interface{}, interface{}, time.Duration) error { return nil }
func (*manageCacheRuntimeSpy) Delete(string, interface{}) error                          { return nil }
func (s *manageCacheRuntimeSpy) DeleteRoute(route string) error {
	s.deletedRoutes = append(s.deletedRoutes, route)
	return nil
}

func TestManageDoAfterInvalidatesDependentPublicCaches(t *testing.T) {
	cfg := config.NewServiceDefaultConfig("performanceshop", 32103)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	ctx := router.NewServiceContextWithConfig(&manageCacheService{}, cfg)
	ctx.SetRunState(true)
	t.Cleanup(func() { ctx.SetRunState(false) })

	spy := &manageCacheRuntimeSpy{}
	(&publicapi.GetProducts{}).RouterInfo().SetCacheManager("performanceshop", spy)
	(&publicapi.GetSuppliers{}).RouterInfo().SetCacheManager("performanceshop", spy)
	(&publicapi.GetPaymentTypes{}).RouterInfo().SetCacheManager("performanceshop", spy)

	if _, err := NewProductManage(nil).DoAfter(nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSupplierManage().DoAfter(nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPaymentTypeManage().DoAfter(nil, nil); err != nil {
		t.Fatal(err)
	}

	want := map[string]int{
		(&publicapi.GetProducts{}).RouterInfo().GetPath():     2,
		(&publicapi.GetSuppliers{}).RouterInfo().GetPath():    1,
		(&publicapi.GetPaymentTypes{}).RouterInfo().GetPath(): 1,
	}
	got := make(map[string]int)
	for _, route := range spy.deletedRoutes {
		got[route]++
	}
	for route, count := range want {
		if got[route] != count {
			t.Fatalf("路由 %s 失效次数 = %d, want %d", route, got[route], count)
		}
	}
}
