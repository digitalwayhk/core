package router

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/transport"
	grpctransport "github.com/digitalwayhk/core/pkg/server/transport/grpc"
	pb "github.com/digitalwayhk/core/pkg/server/transport/grpc/proto"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

func newGRPCLifecycleTestContext(t *testing.T, name string) (*ServiceContext, *grpctransport.Server) {
	t.Helper()
	cfg := config.NewServiceDefaultConfig(name, 0)
	cfg.Host = "127.0.0.1"
	cfg.Transport.GRPC.Port = 0
	cfg.Transport.GRPC.Security = config.GRPCSecurityConfig{Mode: "insecure"}
	sc := &ServiceContext{
		Config:         cfg,
		Service:        &types.Service{Name: name},
		StateChan:      make(chan bool, 1),
		TransportStats: &transport.Stats{},
	}
	server, err := grpctransport.NewServer("127.0.0.1:0", cfg.Transport.GRPC, sc.HandleInternalPayload)
	require.NoError(t, err)
	sc.SetGRPCServer(server)
	go server.Start()
	select {
	case <-server.Ready():
	case <-time.After(time.Second):
		t.Fatal("等待 gRPC 服务就绪超时")
	}
	return sc, server
}

func healthStatus(t *testing.T, address string) grpc_health_v1.HealthCheckResponse_ServingStatus {
	t.Helper()
	conn, err := googlegrpc.NewClient(address, googlegrpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	require.NoError(t, err)
	return response.Status
}

func TestStoppingOneServiceContextReleasesOnlyItsGRPCPort(t *testing.T) {
	firstContext, first := newGRPCLifecycleTestContext(t, "grpc-release-first")
	secondContext, second := newGRPCLifecycleTestContext(t, "grpc-release-second")
	t.Cleanup(func() { secondContext.SetRunState(false) })

	require.Eventually(t, func() bool {
		return healthStatusNoFail(first.Address()) == grpc_health_v1.HealthCheckResponse_SERVING &&
			healthStatusNoFail(second.Address()) == grpc_health_v1.HealthCheckResponse_SERVING
	}, time.Second, 10*time.Millisecond)

	firstContext.SetRunState(false)
	assert.Equal(t, grpc_health_v1.HealthCheckResponse_SERVING, healthStatus(t, second.Address()))

	replacement, err := grpctransport.NewServer(first.Address(), firstContext.Config.Transport.GRPC, func(context.Context, *types.PayLoad) ([]byte, error) {
		return nil, nil
	})
	require.NoError(t, err)
	replacement.Stop()
}

func TestGRPCInboundStatsAreIsolatedPerServiceContext(t *testing.T) {
	firstContext, first := newGRPCLifecycleTestContext(t, "grpc-stats-first")
	secondContext, _ := newGRPCLifecycleTestContext(t, "grpc-stats-second")
	resolved := false
	firstContext.ServiceResolver = NewServiceResolver(nil, func(string) *ServiceContext {
		resolved = true
		return nil
	})
	t.Cleanup(func() {
		firstContext.SetRunState(false)
		secondContext.SetRunState(false)
	})

	conn, err := googlegrpc.NewClient(first.Address(), googlegrpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = pb.NewCoreTransportClient(conn).Call(ctx, &pb.PayloadRequest{TargetService: "missing", TargetPath: "/missing"})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.Equal(t, "internal server error", status.Convert(err).Message())

	assert.Equal(t, uint64(1), firstContext.TransportStats.Snapshot().InboundGRPC)
	assert.Zero(t, secondContext.TransportStats.Snapshot().InboundGRPC)
	assert.False(t, resolved, "错误 listener 上的请求不得查询或转发其他服务")
}

func TestGRPCInboundRejectsTargetForAnotherServiceBeforeResolving(t *testing.T) {
	resolved := false
	sc := &ServiceContext{
		Service:        &types.Service{Name: "grpc-listener-owner"},
		TransportStats: &transport.Stats{},
	}
	sc.ServiceResolver = NewServiceResolver(nil, func(string) *ServiceContext {
		resolved = true
		return nil
	})

	_, err := sc.HandleInternalPayload(context.Background(), &types.PayLoad{
		TargetService: "another-service",
		TargetPath:    "/api/private/query",
	})

	require.ErrorIs(t, err, ErrTargetServiceUnavailable)
	assert.False(t, resolved, "错误 listener 上的请求不得查询或转发其他服务")
	assert.Equal(t, uint64(1), sc.TransportStats.Snapshot().InboundGRPC)
}

func healthStatusNoFail(address string) grpc_health_v1.HealthCheckResponse_ServingStatus {
	conn, err := googlegrpc.NewClient(address, googlegrpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return grpc_health_v1.HealthCheckResponse_UNKNOWN
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	response, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		return grpc_health_v1.HealthCheckResponse_UNKNOWN
	}
	return response.Status
}
