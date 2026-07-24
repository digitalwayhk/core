package private

import (
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

type privateCacheService struct{}

func (*privateCacheService) ServiceName() string { return "performanceshop" }
func (*privateCacheService) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{&GetOrders{}}
}

type identityRequest struct{ userID string }

func (*identityRequest) GetTraceId() string          { return "trace" }
func (r *identityRequest) GetUser() (string, string) { return r.userID, "" }
func (*identityRequest) GetClientIP() string         { return "127.0.0.1" }
func (*identityRequest) NewID() uint                 { return 1 }
func (*identityRequest) Authorized() bool            { return true }
func (*identityRequest) CallService(servertypes.IRouter, ...func(servertypes.IResponse)) (servertypes.IResponse, error) {
	return nil, nil
}
func (*identityRequest) CallTargetService(servertypes.IRouter, *servertypes.TargetInfo, ...func(servertypes.IResponse)) (servertypes.IResponse, error) {
	return nil, nil
}
func (*identityRequest) GetValue(string) string                               { return "client-supplied-user" }
func (*identityRequest) Bind(interface{}) error                               { return nil }
func (*identityRequest) GoZeroBind(interface{}) error                         { return nil }
func (*identityRequest) NewResponse(interface{}, error) servertypes.IResponse { return nil }
func (*identityRequest) GetPath() string                                      { return "" }
func (*identityRequest) GetClaims(string) interface{}                         { return nil }
func (*identityRequest) ServiceName() string                                  { return "performanceshop" }
func (*identityRequest) GetServerInfo() *servertypes.TargetInfo               { return nil }
func (*identityRequest) GetTargetServerInfo(string) *servertypes.TargetInfo   { return nil }

type privateCacheRuntimeSpy struct {
	enabled int
	deleted int
}

func (s *privateCacheRuntimeSpy) EnableRoute(string, time.Duration) error { s.enabled++; return nil }
func (*privateCacheRuntimeSpy) Get(string, interface{}) (interface{}, bool, error) {
	return nil, false, nil
}
func (*privateCacheRuntimeSpy) Set(string, interface{}, interface{}, time.Duration) error { return nil }
func (s *privateCacheRuntimeSpy) Delete(string, interface{}) error                        { s.deleted++; return nil }
func (*privateCacheRuntimeSpy) DeleteRoute(string) error                                  { return nil }

func TestGetOrdersCacheKeyUsesTrustedRequestIdentity(t *testing.T) {
	first := &GetOrders{}
	if err := first.Parse(&identityRequest{userID: "user-a"}); err != nil {
		t.Fatal(err)
	}
	second := &GetOrders{}
	if err := second.Parse(&identityRequest{userID: "user-b"}); err != nil {
		t.Fatal(err)
	}

	if first.GetCacheKey() == second.GetCacheKey() {
		t.Fatal("不同 Token 用户不能共享订单缓存键")
	}
	if first.GetCacheKey() == "user-a" {
		t.Fatal("缓存键不能暴露原始用户 ID")
	}
	first.Clean()
	if first.GetCacheKey() != "" {
		t.Fatal("路由归池前必须清理请求身份")
	}
}

func TestInvalidateOrderCacheDeletesOnlyUserKey(t *testing.T) {
	cfg := config.NewServiceDefaultConfig("performanceshop", 32102)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	ctx := router.NewServiceContextWithConfig(&privateCacheService{}, cfg)
	ctx.SetRunState(true)
	t.Cleanup(func() { ctx.SetRunState(false) })
	spy := &privateCacheRuntimeSpy{}
	info := (&GetOrders{}).RouterInfo()
	info.SetCacheManager("performanceshop", spy)
	enabledBeforeInvalidation := spy.enabled

	InvalidateOrderCache("user-a")

	if enabledBeforeInvalidation != 1 || spy.enabled-enabledBeforeInvalidation != 1 || spy.deleted != 1 {
		t.Fatalf("cache calls = enabled:%d deleted:%d", spy.enabled, spy.deleted)
	}
}
