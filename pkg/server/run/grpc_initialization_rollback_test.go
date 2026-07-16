package run

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	grpctransport "github.com/digitalwayhk/core/pkg/server/transport/grpc"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/service"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
)

func newRollbackContext(name string) *router.ServiceContext {
	service := &concurrencyTestService{name: name, started: make(chan struct{}, 1)}
	ctx := newConcurrencyTestContext(service)
	cfg := config.NewServiceDefaultConfig(name, 0)
	cfg.Host = "127.0.0.1"
	cfg.RunIp = "127.0.0.1"
	cfg.DataCenterID = 1
	cfg.Port = 0
	cfg.SocketPort = 0
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	cfg.Transport.GRPC.Port = 0
	cfg.Transport.GRPC.Security = config.GRPCSecurityConfig{Mode: "insecure"}
	ctx.Config = cfg
	return ctx
}

func assertGRPCPortsReleased(t *testing.T, contexts ...*router.ServiceContext) {
	t.Helper()
	for _, ctx := range contexts {
		port := ctx.Config.Transport.GRPC.Port
		if port == 0 {
			continue
		}
		address := net.JoinHostPort(ctx.Config.Host, strconv.Itoa(port))
		server, err := grpctransport.NewServer(address, ctx.Config.Transport.GRPC, func(_ context.Context, _ *types.PayLoad) ([]byte, error) {
			return nil, nil
		})
		require.NoError(t, err, "gRPC 端口 %s 未被回滚释放", address)
		server.Stop()
	}
}

func TestInitializeServersRollsBackSecondServiceFailures(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*WebServer, *router.ServiceContext)
	}{
		{
			name: "http",
			prepare: func(server *WebServer, second *router.ServiceContext) {
				server.serverOption[strings.ToLower(second.Service.Name)] = &types.ServerOption{IsCors: true}
			},
		},
		{
			name: "grpc",
			prepare: func(_ *WebServer, second *router.ServiceContext) {
				second.Config.Transport.GRPC.Security = config.GRPCSecurityConfig{
					Mode: "mtls", CAFile: "missing-ca.pem", CertFile: "missing-cert.pem", KeyFile: "missing-key.pem",
				}
			},
		},
		{
			name: "save",
			prepare: func(server *WebServer, _ *router.ServiceContext) {
				calls := 0
				server.saveConfig = func(*config.ServerConfig) error {
					calls++
					if calls == 2 {
						return errors.New("injected save failure")
					}
					return nil
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first := newRollbackContext("rollback-a-" + tt.name)
			second := newRollbackContext("rollback-b-" + tt.name)
			server := bareWebServer()
			server.saveConfig = func(*config.ServerConfig) error { return nil }
			tt.prepare(server, second)

			_, err := server.initializeServers([]*router.ServiceContext{second, first})
			require.Error(t, err)
			assertGRPCPortsReleased(t, first, second)
			for _, ctx := range []*router.ServiceContext{first, second} {
				select {
				case failure := <-ctx.Failure():
					t.Fatalf("初始化回滚不得发布运行失败: %v", failure)
				default:
				}
			}

			stopped := make(chan struct{})
			go func() { server.Stop(); close(stopped) }()
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("初始化失败后的 Stop 永久等待")
			}
		})
	}
}

func TestSocketOverrideKeepsDefaultGRPCServer(t *testing.T) {
	ctx := newRollbackContext("socket-with-default-grpc")
	server := bareWebServer()
	server.SocketPort = 25001
	server.saveConfig = func(*config.ServerConfig) error { return nil }

	constructed, err := server.initializeServers([]*router.ServiceContext{ctx})
	require.NoError(t, err)
	require.Equal(t, 25001, ctx.Config.SocketPort)
	require.Positive(t, ctx.Config.Transport.GRPC.Port)
	require.Len(t, ctx.GetServers(), 3, "HTTP、Socket 与 gRPC 都必须进入生命周期")

	for index := len(constructed) - 1; index >= 0; index-- {
		constructed[index].Stop()
	}
}

func TestGRPCRuntimeFailureStopsOnlyOwningWebServer(t *testing.T) {
	firstWeb := bareWebServer()
	secondWeb := bareWebServer()
	firstContext := newRollbackContext("runtime-owner-a")
	secondContext := newRollbackContext("runtime-owner-b")
	failing := newFailingGRPCLifecycle()
	firstContext.SetGRPCServer(failing)
	secondGRPC, err := grpctransport.NewServer("127.0.0.1:0", secondContext.Config.Transport.GRPC,
		func(context.Context, *types.PayLoad) ([]byte, error) { return nil, nil })
	require.NoError(t, err)
	secondContext.SetGRPCServer(secondGRPC)
	firstWeb.AddServiceContext(firstContext)
	secondWeb.AddServiceContext(secondContext)

	firstGroup := service.NewServiceGroup()
	firstGroup.Add(firstContext.GetServers()[0])
	secondGroup := service.NewServiceGroup()
	secondGroup.Add(secondContext.GetServers()[0])
	firstWeb.prepareRunLifecycle()
	secondWeb.prepareRunLifecycle()
	firstWeb.runStarted.Store(true)
	secondWeb.runStarted.Store(true)
	go firstWeb.runServiceGroup(firstGroup)
	go secondWeb.runServiceGroup(secondGroup)
	select {
	case <-secondGRPC.Ready():
	case <-time.After(time.Second):
		t.Fatal("第二个 gRPC 服务未就绪")
	}

	failing.Fail(errors.New("injected runtime failure"))
	require.Eventually(t, firstWeb.stopped.Load, time.Second, time.Millisecond)
	require.False(t, secondWeb.stopped.Load())
	assertStandardHealthServing(t, secondGRPC.Address())

	secondWeb.Stop()
}

func TestWebServerStartFailureClosesLifecycleAndStopReturns(t *testing.T) {
	ctx := newRollbackContext("webserver-start-rollback")
	ctx.Config.Transport.GRPC.Security = config.GRPCSecurityConfig{
		Mode: "mtls", CAFile: "missing-ca.pem", CertFile: "missing-cert.pem", KeyFile: "missing-key.pem",
	}
	server := bareWebServer()
	server.serviceContexts[ctx.Service.Name] = ctx
	server.saveConfig = func(*config.ServerConfig) error { return nil }
	failed := make(chan interface{}, 1)
	go func() {
		defer func() { failed <- recover() }()
		server.Start()
	}()

	select {
	case failure := <-failed:
		require.NotNil(t, failure)
	case <-time.After(time.Second):
		t.Fatal("等待 WebServer 初始化失败超时")
	}
	select {
	case <-server.runReady:
	default:
		t.Fatal("初始化失败必须关闭 runReady")
	}
	select {
	case <-server.runDone:
	default:
		t.Fatal("初始化失败必须关闭 runDone")
	}

	stopped := make(chan struct{})
	go func() { server.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Start recover 后 Stop 永久等待")
	}
}

func assertStandardHealthServing(t *testing.T, address string) {
	t.Helper()
	conn, err := googlegrpc.NewClient(address, googlegrpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	require.NoError(t, err)
	require.Equal(t, grpc_health_v1.HealthCheckResponse_SERVING, response.Status)
}
