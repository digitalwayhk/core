package router

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

var localInternalCallerCounters struct {
	parse      atomic.Int32
	validation atomic.Int32
	do         atomic.Int32
}

type localInternalCallerRoute struct {
	info *types.RouterInfo
}

func (*localInternalCallerRoute) Parse(types.IRequest) error {
	localInternalCallerCounters.parse.Add(1)
	return nil
}

func (*localInternalCallerRoute) Validation(types.IRequest) error {
	localInternalCallerCounters.validation.Add(1)
	return nil
}

func (*localInternalCallerRoute) Do(types.IRequest) (interface{}, error) {
	localInternalCallerCounters.do.Add(1)
	return "ok", nil
}

func (r *localInternalCallerRoute) RouterInfo() *types.RouterInfo { return r.info }

type localInternalCallerService struct {
	name  string
	route types.IRouter
}

func (s *localInternalCallerService) ServiceName() string                  { return s.name }
func (s *localInternalCallerService) Routers() []types.IRouter             { return []types.IRouter{s.route} }
func (*localInternalCallerService) SubscribeRouters() []*types.ObserveArgs { return nil }

func newLocalInternalCallerTarget(t *testing.T) (*ServiceContext, *localInternalCallerRoute) {
	t.Helper()
	serviceName := fmt.Sprintf("internal-target-%d", time.Now().UnixNano())
	path := "/api/" + serviceName + "/query"
	route := &localInternalCallerRoute{}
	info := &types.RouterInfo{
		Path:            path,
		ServiceName:     serviceName,
		Method:          http.MethodPost,
		PathType:        types.PublicType,
		InternalCallers: []string{"shop-user"},
		Subscriber:      make(map[types.ObserveState]map[string]*types.ObserveArgs),
	}
	route.info = info
	info.SetInstance(route)
	cfg := config.NewServiceDefaultConfig(serviceName, 0)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	target := NewServiceContextWithConfig(&localInternalCallerService{name: serviceName, route: route}, cfg)
	t.Cleanup(func() { target.SetRunState(false) })
	return target, route
}

func newLocalInternalCallerSource(name string) *ServiceContext {
	return &ServiceContext{
		Service: &types.Service{Name: name},
		Config:  config.NewServiceDefaultConfig(name, 0),
	}
}

func resetLocalInternalCallerCounters() {
	localInternalCallerCounters.parse.Store(0)
	localInternalCallerCounters.validation.Store(0)
	localInternalCallerCounters.do.Store(0)
}

func TestLocalDispatchTrustsActualSourceContextInsteadOfPayloadClaim(t *testing.T) {
	target, route := newLocalInternalCallerTarget(t)
	resetLocalInternalCallerCounters()
	source := newLocalInternalCallerSource("shop-user")

	response, err := source.CallService(&types.PayLoad{
		TraceID:       "trace-local-allowed",
		SourceService: "spoofed-source",
		TargetService: target.Service.Name,
		TargetPath:    route.info.GetPath(),
		Instance:      &localInternalCallerRoute{},
	})

	require.NoError(t, err)
	require.True(t, response.GetSuccess())
	require.Zero(t, localInternalCallerCounters.parse.Load(), "内部载荷已结构化，不重复执行 HTTP Parse")
	require.Equal(t, int32(1), localInternalCallerCounters.validation.Load())
	require.Equal(t, int32(1), localInternalCallerCounters.do.Load())
}

func TestLocalDispatchRejectsWrongActualSourceBeforeParse(t *testing.T) {
	target, route := newLocalInternalCallerTarget(t)
	resetLocalInternalCallerCounters()
	source := newLocalInternalCallerSource("shop-supplier")

	_, err := source.CallService(&types.PayLoad{
		TraceID:       "trace-local-denied",
		SourceService: "shop-user",
		TargetService: target.Service.Name,
		TargetPath:    route.info.GetPath(),
		Instance:      &localInternalCallerRoute{},
	})

	require.ErrorIs(t, err, types.ErrInternalCallerForbidden)
	require.Zero(t, localInternalCallerCounters.parse.Load())
	require.Zero(t, localInternalCallerCounters.validation.Load())
	require.Zero(t, localInternalCallerCounters.do.Load())
}
