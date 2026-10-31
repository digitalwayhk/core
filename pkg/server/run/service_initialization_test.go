// 本文件验证服务资源初始化先于任何监听器构造，失败时回滚已初始化的业务资源。
package run

import (
	"context"
	"errors"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

type initializationTestResource struct{ closes int }

func (r *initializationTestResource) Close(context.Context) error { r.closes++; return nil }

type initializationTestService struct {
	name       string
	initialize func() error
	stops      int
}

func (s *initializationTestService) ServiceName() string    { return s.name }
func (*initializationTestService) Routers() []types.IRouter { return nil }
func (s *initializationTestService) Initialize() error      { return s.initialize() }
func (s *initializationTestService) Stop()                  { s.stops++ }

// TestServiceInitializationFailurePreventsListeners 验证初始化失败不会构造 HTTP/gRPC，并清理部分初始化。
func TestServiceInitializationFailurePreventsListeners(t *testing.T) {
	ctx := newRollbackContext("initialize-failure")
	failure := errors.New("injected storage initialization failure")
	business := &initializationTestService{name: ctx.Service.Name, initialize: func() error { return failure }}
	ctx.Service.Instance = business
	server := bareWebServer()
	server.saveConfig = func(*config.ServerConfig) error { return nil }
	constructed, err := server.initializeServers([]*router.ServiceContext{ctx})
	t.Cleanup(func() {
		for _, transport := range constructed {
			transport.Stop()
		}
	})
	require.ErrorIs(t, err, failure)
	require.Empty(t, constructed)
	require.Nil(t, ctx.Service.HttpServer)
	require.Equal(t, 1, business.stops, "失败的初始化也必须释放部分资源")
}

// TestAllServiceInitializersRunBeforeTransports 验证所有服务先初始化，再构造任一监听器；后续失败反向清理。
func TestAllServiceInitializersRunBeforeTransports(t *testing.T) {
	first, second := newRollbackContext("initialize-a"), newRollbackContext("initialize-b")
	var events []string
	newBusiness := func(ctx *router.ServiceContext) *initializationTestService {
		return &initializationTestService{name: ctx.Service.Name, initialize: func() error {
			require.Nil(t, first.Service.HttpServer)
			require.Nil(t, second.Service.HttpServer)
			events = append(events, ctx.Service.Name)
			return nil
		}}
	}
	a, b := newBusiness(first), newBusiness(second)
	first.Service.Instance, second.Service.Instance = a, b
	server := bareWebServer()
	failure := errors.New("injected config persistence failure")
	server.saveConfig = func(*config.ServerConfig) error {
		require.ElementsMatch(t, []string{first.Service.Name, second.Service.Name}, events)
		return failure
	}
	_, err := server.initializeServers([]*router.ServiceContext{second, first})
	require.ErrorIs(t, err, failure)
	require.Equal(t, 1, a.stops)
	require.Equal(t, 1, b.stops)
	assertGRPCPortsReleased(t, first, second)
}

// TestInitializationRollbackClosesManagedResources 验证错误、panic 和后续失败均关闭托管资源且不重复关闭。
func TestInitializationRollbackClosesManagedResources(t *testing.T) {
	for _, mode := range []string{"error", "panic", "transport"} {
		t.Run(mode, func(t *testing.T) {
			name := "initialize-owned-" + mode
			resource := &initializationTestResource{}
			failure := errors.New("injected initializer failure")
			business := &initializationTestService{name: name}
			var ctx *router.ServiceContext
			business.initialize = func() error {
				if err := ctx.UseResource("owned", resource); err != nil {
					return err
				}
				switch mode {
				case "error":
					return failure
				case "panic":
					panic(failure)
				}
				return nil
			}
			ctx = router.NewServiceContextWithConfig(business, newRollbackContext(name).Config)
			t.Cleanup(func() { ctx.SetRunState(false) })
			server := bareWebServer()
			server.saveConfig = func(*config.ServerConfig) error { return failure }
			if mode == "panic" {
				require.Panics(t, func() { _, _ = server.initializeServers([]*router.ServiceContext{ctx}) })
			} else {
				_, err := server.initializeServers([]*router.ServiceContext{ctx})
				require.ErrorIs(t, err, failure)
			}
			require.Equal(t, 1, business.stops)
			require.Equal(t, 1, resource.closes)
			ctx.SetRunState(false)
			require.Equal(t, 1, resource.closes)
		})
	}
}
