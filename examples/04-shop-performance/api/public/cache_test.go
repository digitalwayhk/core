package public

import (
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

type publicCacheService struct{}

func (*publicCacheService) ServiceName() string { return "performanceshop" }
func (*publicCacheService) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{&GetProducts{}, &GetSuppliers{}, &GetPaymentTypes{}}
}

type cacheRuntimeSpy struct {
	enabled []string
	routes  []string
	keys    int
}

func (s *cacheRuntimeSpy) EnableRoute(route string, _ time.Duration) error {
	s.enabled = append(s.enabled, route)
	return nil
}
func (*cacheRuntimeSpy) Get(string, interface{}) (interface{}, bool, error)        { return nil, false, nil }
func (*cacheRuntimeSpy) Set(string, interface{}, interface{}, time.Duration) error { return nil }
func (s *cacheRuntimeSpy) Delete(_ string, _ interface{}) error {
	s.keys++
	return nil
}
func (s *cacheRuntimeSpy) DeleteRoute(route string) error {
	s.routes = append(s.routes, route)
	return nil
}

func TestPublicCacheKeysAreStableAndUnambiguous(t *testing.T) {
	first := &GetProducts{ID: 1, Code: "code", Name: "ab", SupplierID: 2, SupplierCode: "c"}
	second := &GetProducts{ID: 1, Code: "code", Name: "a", SupplierID: 2, SupplierCode: "bc"}
	copyOfFirst := *first

	if first.GetCacheKey() != copyOfFirst.GetCacheKey() {
		t.Fatal("相同商品筛选条件必须生成相同缓存键")
	}
	if first.GetCacheKey() == second.GetCacheKey() {
		t.Fatal("字段边界不同的商品筛选条件不能生成相同缓存键")
	}
	if (&GetSuppliers{ID: 1, Code: "s", Name: "n"}).GetCacheKey() == "" {
		t.Fatal("供应商缓存键不能为空")
	}
	if (&GetPaymentTypes{Code: "p", Name: "n"}).GetCacheKey() == "" {
		t.Fatal("支付类型缓存键不能为空")
	}
}

func TestPublicRoutesEnableCacheAndInvalidateByDependency(t *testing.T) {
	cfg := config.NewServiceDefaultConfig("performanceshop", 32101)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	ctx := router.NewServiceContextWithConfig(&publicCacheService{}, cfg)
	ctx.SetRunState(true)
	t.Cleanup(func() { ctx.SetRunState(false) })
	spy := &cacheRuntimeSpy{}
	productInfo := (&GetProducts{}).RouterInfo()
	supplierInfo := (&GetSuppliers{}).RouterInfo()
	paymentInfo := (&GetPaymentTypes{}).RouterInfo()
	productInfo.SetCacheManager("performanceshop", spy)
	supplierInfo.SetCacheManager("performanceshop", spy)
	paymentInfo.SetCacheManager("performanceshop", spy)
	enabledBeforeInvalidation := len(spy.enabled)

	InvalidateSupplierCaches()
	InvalidatePaymentTypeCache()

	if enabledBeforeInvalidation != 3 {
		t.Fatalf("启用缓存路由数 = %d, want 3", enabledBeforeInvalidation)
	}
	if len(spy.enabled)-enabledBeforeInvalidation != 3 {
		t.Fatalf("失效时重放缓存声明数 = %d, want 3", len(spy.enabled)-enabledBeforeInvalidation)
	}
	if len(spy.routes) != 3 {
		t.Fatalf("整路由失效数 = %d, want 3", len(spy.routes))
	}
}
