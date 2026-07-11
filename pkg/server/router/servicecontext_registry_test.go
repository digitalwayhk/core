package router_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var registryTestSequence atomic.Uint64

func uniqueServiceName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, registryTestSequence.Add(1))
}

func waitForRegistrySignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("等待注册表初始化信号超时")
	}
}

type instrumentedService struct {
	name       string
	routerCall func() []types.IRouter
}

func (s *instrumentedService) ServiceName() string { return s.name }
func (s *instrumentedService) Routers() []types.IRouter {
	if s.routerCall == nil {
		return nil
	}
	return s.routerCall()
}
func (s *instrumentedService) SubscribeRouters() []*types.ObserveArgs { return nil }

func testServiceConfig(name string, port int) *config.ServerConfig {
	con := config.NewServiceDefaultConfig(name, port)
	con.Cluster.Mode = "off"
	con.MQ.Mode = "off"
	con.Transport.Internal = ""
	con.Transport.Fallback = nil
	return con
}

func TestServiceContextConcurrentSameNameInitializesOnce(t *testing.T) {
	const callers = 32
	serviceName := uniqueServiceName("sctest-concurrent-same-name")

	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseAll()
	var calls atomic.Int32
	service := &instrumentedService{
		name: serviceName,
		routerCall: func() []types.IRouter {
			if calls.Add(1) == 1 {
				close(entered)
			}
			<-release
			return nil
		},
	}
	con := testServiceConfig(serviceName, 31001)

	results := make(chan *router.ServiceContext, callers)
	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			results <- router.NewServiceContextWithConfig(service, con)
		}()
	}

	waitForRegistrySignal(t, entered)
	releaseAll()
	wg.Wait()
	close(results)

	var first *router.ServiceContext
	for got := range results {
		if first == nil {
			first = got
		}
		assert.Same(t, first, got)
	}
	assert.Equal(t, int32(1), calls.Load())
}

func TestServiceContextConcurrentDifferentNamesInitializeInParallel(t *testing.T) {
	entered := make(chan string, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseAll()
	newService := func(name string) *instrumentedService {
		return &instrumentedService{
			name: name,
			routerCall: func() []types.IRouter {
				entered <- name
				<-release
				return nil
			},
		}
	}

	services := []*instrumentedService{
		newService(uniqueServiceName("sctest-concurrent-parallel-a")),
		newService(uniqueServiceName("sctest-concurrent-parallel-b")),
	}
	var wg sync.WaitGroup
	wg.Add(len(services))
	for i, service := range services {
		go func(service *instrumentedService, port int) {
			defer wg.Done()
			router.NewServiceContextWithConfig(service, testServiceConfig(service.name, port))
		}(service, 31010+i)
	}

	seen := map[string]bool{}
	for range services {
		select {
		case name := <-entered:
			seen[name] = true
		case <-time.After(2 * time.Second):
			t.Fatal("等待不同服务并行初始化超时")
		}
	}
	releaseAll()
	wg.Wait()

	assert.True(t, seen[services[0].name])
	assert.True(t, seen[services[1].name])
}

func TestServiceContextSnapshotIsIsolated(t *testing.T) {
	serviceName := uniqueServiceName("sctest-context-snapshot")
	sc := router.NewServiceContextWithConfig(
		&instrumentedService{name: serviceName},
		testServiceConfig(serviceName, 31020),
	)

	snapshot := router.GetContexts()
	require.Same(t, sc, snapshot[serviceName])
	delete(snapshot, serviceName)
	snapshot["injected"] = sc

	assert.Same(t, sc, router.GetContext(serviceName))
	assert.Nil(t, router.GetContext("injected"))
}

func TestServiceContextPanicAllowsSameNameRetry(t *testing.T) {
	serviceName := uniqueServiceName("sctest-context-panic-retry")
	panicValue := &struct{ message string }{message: "initialization failed"}
	var shouldPanic atomic.Bool
	shouldPanic.Store(true)
	service := &instrumentedService{
		name: serviceName,
		routerCall: func() []types.IRouter {
			if shouldPanic.Load() {
				panic(panicValue)
			}
			return nil
		},
	}
	con := testServiceConfig(serviceName, 31030)

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		router.NewServiceContextWithConfig(service, con)
	}()
	assert.Same(t, panicValue, recovered)

	shouldPanic.Store(false)
	assert.NotNil(t, router.NewServiceContextWithConfig(service, con))
}

func TestServiceContextDefaultConfigSequenceIsConcurrentSafe(t *testing.T) {
	const callers = 8
	prefix := uniqueServiceName("sctest-default-sequence")
	contexts := make(chan *router.ServiceContext, callers)
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := range callers {
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("%s-%d", prefix, i)
			contexts <- router.NewServiceContext(&instrumentedService{name: name})
		}(i)
	}
	wg.Wait()
	close(contexts)

	ports := make(map[int]struct{}, callers)
	dataCenters := make(map[uint]struct{}, callers)
	for sc := range contexts {
		ports[sc.Config.Port] = struct{}{}
		dataCenters[sc.Config.DataCenterID] = struct{}{}
	}
	assert.Len(t, ports, callers)
	assert.Len(t, dataCenters, callers)
}

func TestServiceContextDefaultSequenceIsReusedAfterPanic(t *testing.T) {
	serviceName := uniqueServiceName("sctest-default-sequence-panic-retry")
	expectedSequence := len(router.GetContexts())
	var calls atomic.Int32
	service := &instrumentedService{
		name: serviceName,
		routerCall: func() []types.IRouter {
			if calls.Add(1) == 1 {
				panic("default initialization failed")
			}
			return nil
		},
	}

	assert.Panics(t, func() {
		router.NewServiceContext(service)
	})

	sc := router.NewServiceContext(service)
	require.NotNil(t, sc)
	assert.Equal(t, router.DEFAULTPORT+expectedSequence, sc.Config.Port)
	assert.Equal(t, uint(expectedSequence+1), sc.Config.DataCenterID)
}

func TestResultConcurrentReadWrite(t *testing.T) {
	const callers = 128
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := range callers {
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("/sctest/result/%d", i)
			router.SetTestResult(key, i)
			assert.Equal(t, i, router.GetTestResult(key))
		}(i)
	}
	wg.Wait()
}

func TestResultConcurrentSameKeyReadWrite(t *testing.T) {
	const callers = 64
	key := fmt.Sprintf("/sctest/result/shared/%d", registryTestSequence.Add(1))
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := range callers {
		go func(i int) {
			defer wg.Done()
			for range 20 {
				router.SetTestResult(key, i)
				_ = router.GetTestResult(key)
			}
		}(i)
	}
	wg.Wait()
}
