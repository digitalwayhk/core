package router_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	manageapi "github.com/digitalwayhk/core/examples/01-simple-shop/api/manage"
	privateapi "github.com/digitalwayhk/core/examples/01-simple-shop/api/private"
	publicfixture "github.com/digitalwayhk/core/internal/compat/fixture/api/public"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type registeredRouterService struct {
	name string
}

var (
	_ func(interface{}) *types.RouterInfo                 = router.DefaultRouterInfo
	_ func(interface{}, string, string) *types.RouterInfo = router.NewRouterInfo
)

func (s *registeredRouterService) ServiceName() string { return s.name }
func (*registeredRouterService) Routers() []types.IRouter {
	return []types.IRouter{&privateapi.GetOrders{}}
}
func (*registeredRouterService) SubscribeRouters() []*types.ObserveArgs { return nil }

type registeredManageService struct {
	name string
}

func (s *registeredManageService) ServiceName() string { return s.name }
func (*registeredManageService) Routers() []types.IRouter {
	return manageapi.NewProductManage().Routers()
}
func (*registeredManageService) SubscribeRouters() []*types.ObserveArgs { return nil }

type fixedRouter struct {
	info *types.RouterInfo
}

func (*fixedRouter) Parse(types.IRequest) error             { return nil }
func (*fixedRouter) Validation(types.IRequest) error        { return nil }
func (*fixedRouter) Do(types.IRequest) (interface{}, error) { return nil, nil }
func (r *fixedRouter) RouterInfo() *types.RouterInfo        { return r.info }

type fixedRouterService struct {
	name  string
	route types.IRouter
}

func (s *fixedRouterService) ServiceName() string                  { return s.name }
func (s *fixedRouterService) Routers() []types.IRouter             { return []types.IRouter{s.route} }
func (*fixedRouterService) SubscribeRouters() []*types.ObserveArgs { return nil }

type recordingEventRuntime struct {
	subscriptions int
}

func (r *recordingEventRuntime) Subscribe(string, event.Handler) (func(), error) {
	r.subscriptions++
	return func() {}, nil
}
func (*recordingEventRuntime) Publish(context.Context, event.PublishRequest) error { return nil }

type recordingCacheRuntime struct {
	enabled int
}

func (r *recordingCacheRuntime) EnableRoute(string, time.Duration) error {
	r.enabled++
	return nil
}
func (*recordingCacheRuntime) Get(string, interface{}) (interface{}, bool, error) {
	return nil, false, nil
}
func (*recordingCacheRuntime) Set(string, interface{}, interface{}, time.Duration) error { return nil }
func (*recordingCacheRuntime) Delete(string, interface{}) error                          { return nil }
func (*recordingCacheRuntime) DeleteRoute(string) error                                  { return nil }

func TestRouterInfoReturnsRegisteredSingletonWhenDirectoryDiffersFromServiceName(t *testing.T) {
	serviceName := uniqueServiceName("sctest-router-owner")
	service := &registeredRouterService{name: serviceName}
	sc := router.NewServiceContextWithConfig(service, testServiceConfig(serviceName, 31201))
	sc.SetRunState(true)
	t.Cleanup(func() { sc.SetRunState(false) })

	registered := sc.Router.GetRouters()
	require.Len(t, registered, 1)
	require.Equal(t, serviceName, registered[0].GetServiceName())
	require.True(t, strings.HasPrefix(registered[0].GetPath(), "/api/"+serviceName+"/"))

	resolved := (&privateapi.GetOrders{}).RouterInfo()
	assert.Same(t, registered[0], resolved)
	assert.Equal(t, serviceName, resolved.GetServiceName())
	assert.Equal(t, registered[0].GetPath(), resolved.GetPath())
}

func TestRouterInfoRegistryUnregistersBeforeServiceContextCanBeRebuilt(t *testing.T) {
	serviceName := uniqueServiceName("sctest-router-unregister")
	service := &registeredRouterService{name: serviceName}
	sc := router.NewServiceContextWithConfig(service, testServiceConfig(serviceName, 31202))
	sc.SetRunState(true)

	registeredRoutes := sc.Router.GetRouters()
	require.Len(t, registeredRoutes, 1)
	registered := registeredRoutes[0]
	assert.Same(t, registered, (&privateapi.GetOrders{}).RouterInfo())
	sc.SetRunState(false)

	resolvedAfterShutdown := (&privateapi.GetOrders{}).RouterInfo()
	assert.NotSame(t, registered, resolvedAfterShutdown)
	assert.NotEqual(t, serviceName, resolvedAfterShutdown.GetServiceName())

	rebuilt := router.NewServiceContextWithConfig(service, testServiceConfig(serviceName, 31202))
	rebuilt.SetRunState(true)
	t.Cleanup(func() { rebuilt.SetRunState(false) })
	rebuiltRoutes := rebuilt.Router.GetRouters()
	require.Len(t, rebuiltRoutes, 1)
	assert.NotSame(t, registered, rebuiltRoutes[0])
	assert.Same(t, rebuiltRoutes[0], (&privateapi.GetOrders{}).RouterInfo())
	assert.Equal(t, serviceName, rebuiltRoutes[0].GetServiceName())
	assert.Equal(t, "/api/"+serviceName+"/getorders", rebuiltRoutes[0].GetPath())
}

func TestRouterInfoWithoutOwnerFailsClosedWhenTypeHasMultipleServices(t *testing.T) {
	firstName := uniqueServiceName("sctest-router-owner-a")
	secondName := uniqueServiceName("sctest-router-owner-b")

	config.BeginServerInitialization()
	first := router.NewServiceContextWithConfig(
		&registeredRouterService{name: firstName},
		testServiceConfig(firstName, 31203),
	)
	second := router.NewServiceContextWithConfig(
		&registeredRouterService{name: secondName},
		testServiceConfig(secondName, 31204),
	)
	config.EndServerInitialization()
	first.SetRunState(true)
	second.SetRunState(true)
	t.Cleanup(func() {
		first.SetRunState(false)
		second.SetRunState(false)
	})

	assert.PanicsWithValue(t,
		"router GetOrders is registered by multiple services; resolve it through a ServiceContext",
		func() { (&privateapi.GetOrders{}).RouterInfo() },
	)
}

func TestServiceContextPreflightRejectsRouterOwnedByAnotherServiceWithoutMutation(t *testing.T) {
	firstName := uniqueServiceName("sctest-router-owner-first")
	secondName := uniqueServiceName("sctest-router-owner-second")
	first := router.NewServiceContextWithConfig(
		&registeredRouterService{name: firstName},
		testServiceConfig(firstName, 31208),
	)
	first.SetRunState(true)
	t.Cleanup(func() { first.SetRunState(false) })

	firstRoutes := first.Router.GetRouters()
	require.Len(t, firstRoutes, 1)
	registered := firstRoutes[0]
	originalID := registered.GetID()
	originalPath := registered.GetPath()
	originalServiceName := registered.GetServiceName()

	assert.PanicsWithValue(t, "router metadata owner conflict", func() {
		router.NewServiceContextWithConfig(
			&registeredRouterService{name: secondName},
			testServiceConfig(secondName, 31209),
		)
	})
	assert.NotPanics(t, func() {
		assert.Equal(t, originalID, registered.GetID())
		assert.Equal(t, originalPath, registered.GetPath())
		assert.Equal(t, originalServiceName, registered.GetServiceName())
	})
}

func TestServiceRouterAddRoutesRejectsFrozenRouterOwnedByAnotherServiceWithoutMutation(t *testing.T) {
	firstOwner := uniqueServiceName("sctest-add-routes-owner-first")
	secondOwner := uniqueServiceName("sctest-add-routes-owner-second")
	path := "/api/" + firstOwner + "/fixed"
	api := &fixedRouter{}
	info := &types.RouterInfo{
		ID:           utils.HashCode64(path),
		Path:         path,
		ServiceName:  firstOwner,
		PackPath:     "fixture/api/public",
		Method:       http.MethodPost,
		PathType:     types.PublicType,
		InstanceName: "Fixed",
		StructName:   "fixedRouter",
	}
	api.info = info
	info.SetInstance(api)
	info.Freeze(firstOwner)

	service := &fixedRouterService{name: secondOwner, route: api}
	serviceContext := &router.ServiceContext{
		Service: &types.Service{Name: secondOwner, Routers: []types.IRouter{api}, Instance: service},
	}

	assert.PanicsWithValue(t, "router metadata owner conflict", func() {
		router.NewServiceRouter(serviceContext, service)
	})
	assert.Equal(t, utils.HashCode64(path), info.GetID())
	assert.Equal(t, path, info.GetPath())
	assert.Equal(t, firstOwner, info.GetServiceName())
}

func TestServiceRouterSameOwnerReuseDoesNotRebindRuntimes(t *testing.T) {
	const owner = "same-owner"
	eventRuntime := &recordingEventRuntime{}
	cacheRuntime := &recordingCacheRuntime{}
	api := &fixedRouter{}
	info := &types.RouterInfo{
		ID:          utils.HashCode64("/api/same-owner/fixed"),
		Path:        "/api/same-owner/fixed",
		ServiceName: owner,
		Method:      http.MethodPost,
		PathType:    types.PublicType,
	}
	api.info = info
	info.SetInstance(api)
	info.SetEventBridge(owner, eventRuntime)
	info.SetCacheManager(owner, cacheRuntime)
	info.Freeze(owner)

	service := &fixedRouterService{name: owner, route: api}
	serviceContext := &router.ServiceContext{
		Config:    testServiceConfig(owner, 31210),
		StateChan: make(chan bool, 1),
		Service:   &types.Service{Name: owner, Routers: []types.IRouter{api}, Instance: service},
	}
	serviceRouter := router.NewServiceRouter(serviceContext, service)
	serviceContext.Router = serviceRouter
	serviceContext.SetRunState(true)
	t.Cleanup(func() { serviceContext.SetRunState(false) })
	require.Same(t, info, serviceRouter.GetRouter(info.GetPath()))

	require.NoError(t, info.Subscribe(&types.ObserveArgs{
		State:          types.ObserveRequest,
		ReceiveService: "same-owner-observer",
	}))
	info.UseCache(time.Second)
	assert.Equal(t, 1, eventRuntime.subscriptions, "同 owner 复用不得替换原事件运行时")
	assert.Equal(t, 1, cacheRuntime.enabled, "同 owner 复用不得替换原缓存运行时")
}

func TestRouterInfoOptionsApplyOnlyBeforeRegistration(t *testing.T) {
	config.BeginServerInitialization()
	created := router.DefaultRouterInfoWithOptions(
		&privateapi.GetOrders{},
		router.WithMethod(http.MethodGet),
	)
	config.EndServerInitialization()
	assert.Equal(t, http.MethodGet, created.GetMethod())

	serviceName := uniqueServiceName("sctest-router-options")
	sc := router.NewServiceContextWithConfig(
		&registeredRouterService{name: serviceName},
		testServiceConfig(serviceName, 31205),
	)
	sc.SetRunState(true)
	t.Cleanup(func() { sc.SetRunState(false) })

	registered := sc.Router.GetRouters()[0]
	resolved := router.DefaultRouterInfoWithOptions(
		&privateapi.GetOrders{},
		router.WithMethod(http.MethodPost),
	)
	assert.Same(t, registered, resolved)
	assert.Equal(t, http.MethodGet, resolved.GetMethod(), "注册后的 Option 不得改写冻结元数据")
}

func TestInternalCallersAreNormalizedFrozenAndDefensivelyCopied(t *testing.T) {
	info := router.DefaultRouterInfoWithOptions(
		&publicfixture.GetThing{},
		router.WithInternalCallers(" shop-user ", "shop-order", "shop-user", ""),
	)
	info.Freeze("internal-caller-fixture")

	got := info.GetInternalCallers()
	require.Equal(t, []string{"shop-order", "shop-user"}, got)
	got[0] = "mutated"
	require.Equal(t, []string{"shop-order", "shop-user"}, info.GetInternalCallers())

	require.Panics(t, func() {
		info.InternalCallers = []string{"changed"}
		_ = info.GetPath()
	})
}

func TestRouterInfoOptionsConfigureRegistrationMetadata(t *testing.T) {
	const path = "/api/catalog/products"
	config.BeginServerInitialization()
	info := router.DefaultRouterInfoWithOptions(
		&privateapi.GetOrders{},
		router.WithPath(path),
		router.WithMethod(http.MethodPatch),
		router.WithAuth(false),
		router.WithPathType(types.PublicType),
		router.WithPoolSize(17),
	)
	config.EndServerInitialization()

	assert.Equal(t, path, info.GetPath())
	assert.Equal(t, http.MethodPatch, info.GetMethod())
	assert.False(t, info.GetAuth())
	assert.Equal(t, types.PublicType, info.GetPathType())
	assert.Equal(t, 17, info.GetPoolSize())
	assert.Equal(t, utils.HashCode64(path), info.GetID(), "ID 必须基于 Option 应用后的最终 Path")
}

func TestRouterInfoConcurrentResolveIsReadOnly(t *testing.T) {
	serviceName := uniqueServiceName("sctest-router-concurrent-read")
	sc := router.NewServiceContextWithConfig(
		&registeredRouterService{name: serviceName},
		testServiceConfig(serviceName, 31206),
	)
	sc.SetRunState(true)
	t.Cleanup(func() { sc.SetRunState(false) })

	const workers = 64
	const iterations = 100
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for range iterations {
				info := (&privateapi.GetOrders{}).RouterInfo()
				assert.Equal(t, http.MethodGet, info.GetMethod())
				assert.Equal(t, serviceName, info.GetServiceName())
				assert.Equal(t, "/api/"+serviceName+"/getorders", info.GetPath())
			}
		}()
	}
	wg.Wait()
}

func TestManageRouterInfoConcurrentResolveIsReadOnly(t *testing.T) {
	serviceName := uniqueServiceName("sctest-manage-router-concurrent-read")
	sc := router.NewServiceContextWithConfig(
		&registeredManageService{name: serviceName},
		testServiceConfig(serviceName, 31207),
	)
	sc.SetRunState(true)
	t.Cleanup(func() { sc.SetRunState(false) })

	registeredByPath := make(map[string]*types.RouterInfo)
	for _, info := range sc.Router.GetRouters() {
		registeredByPath[info.GetPath()] = info
	}
	require.NotEmpty(t, registeredByPath)

	const workers = 32
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for _, api := range manageapi.NewProductManage().Routers() {
				info := api.RouterInfo()
				assert.Same(t, registeredByPath[info.GetPath()], info)
				assert.Equal(t, serviceName, info.GetServiceName())
				assert.Equal(t, types.ManageType, info.GetPathType())
				assert.True(t, info.GetAuth())
			}
		}()
	}
	wg.Wait()
}
