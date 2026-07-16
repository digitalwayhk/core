package grpc_test

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/digitalwayhk/core/pkg/server/config"
	grpctransport "github.com/digitalwayhk/core/pkg/server/transport/grpc"
	pb "github.com/digitalwayhk/core/pkg/server/transport/grpc/proto"
	coretypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startTestServer registers a minimal CoreTransport gRPC server on a random port.
func startTestServer(t *testing.T, handler func(ctx context.Context, payload *coretypes.PayLoad) ([]byte, error)) (addr string, stop func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := grpctransport.NewServer(0, handler)
	grpcSrv := grpc.NewServer()
	pb.RegisterCoreTransportServer(grpcSrv, srv)
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcSrv, healthServer)
	go grpcSrv.Serve(lis)
	return lis.Addr().String(), grpcSrv.GracefulStop
}

func newInsecureTransport() *grpctransport.GRPCTransport {
	return grpctransport.New(config.GRPCTransportConfig{
		Security: config.GRPCSecurityConfig{Mode: "insecure"},
	})
}

func TestGRPCTransport_SendAndReceive(t *testing.T) {
	addr, stop := startTestServer(t, func(_ context.Context, payload *coretypes.PayLoad) ([]byte, error) {
		return []byte("pong-" + payload.TraceID), nil
	})
	defer stop()

	tr := newInsecureTransport()
	result, err := tr.Send(context.Background(), &coretypes.PayLoad{TraceID: "abc123"}, addr)
	require.NoError(t, err)
	assert.Equal(t, []byte("pong-abc123"), result)
}

func TestGRPCTransport_PayloadRoundTrip(t *testing.T) {
	var received *coretypes.PayLoad
	addr, stop := startTestServer(t, func(_ context.Context, payload *coretypes.PayLoad) ([]byte, error) {
		received = payload
		return []byte("ok"), nil
	})
	defer stop()

	tr := newInsecureTransport()
	sent := &coretypes.PayLoad{
		TraceID:       "trace-1",
		SourceService: "svc-a",
		TargetService: "svc-b",
		TargetPath:    "/api/test",
		Auth:          true,
		Data:          []byte(`{"x":1}`),
	}
	_, err := tr.Send(context.Background(), sent, addr)
	require.NoError(t, err)
	require.NotNil(t, received)
	assert.Equal(t, "trace-1", received.TraceID)
	assert.Equal(t, "svc-a", received.SourceService)
	assert.Equal(t, "svc-b", received.TargetService)
	assert.Equal(t, "/api/test", received.TargetPath)
	assert.True(t, received.Auth)
	assert.Equal(t, []byte(`{"x":1}`), received.Data)
}

func TestGRPCTransport_Health_Reachable(t *testing.T) {
	addr, stop := startTestServer(t, func(_ context.Context, _ *coretypes.PayLoad) ([]byte, error) {
		return nil, nil
	})
	defer stop()

	tr := newInsecureTransport()
	assert.NoError(t, tr.Health(context.Background(), addr))
}

func TestGRPCTransport_Health_Unreachable(t *testing.T) {
	tr := newInsecureTransport()
	// Use a port that should not be listening; give a short deadline so the test
	// doesn't block forever waiting for gRPC's internal connect backoff.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := tr.Health(ctx, "127.0.0.1:19999")
	assert.Error(t, err)
}

func TestGRPCTransport_Supports(t *testing.T) {
	tr := newInsecureTransport()
	ctx := context.Background()
	// grpc transport supports non-http targets
	assert.True(t, tr.Supports(ctx, nil, "127.0.0.1:19090"))
	// http targets are not handled by grpc transport
	assert.False(t, tr.Supports(ctx, nil, "http://127.0.0.1:8080"))
	// empty target not supported
	assert.False(t, tr.Supports(ctx, nil, ""))
}

// startRawGRPCServer starts a grpc server directly using credentials for timeout test.
func startRawGRPCServer(t *testing.T) (addr string, stop func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer()
	// Register a handler that blocks forever to test timeout.
	pb.RegisterCoreTransportServer(srv, &blockingServer{})
	go srv.Serve(lis)
	return lis.Addr().String(), srv.GracefulStop
}

// blockingServer is a server that never responds (for timeout tests).
type blockingServer struct {
	pb.UnimplementedCoreTransportServer
}

func (b *blockingServer) Call(ctx context.Context, _ *pb.PayloadRequest) (*pb.PayloadResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestGRPCTransport_ConnectionPooling_ReusesConnections(t *testing.T) {
	addr, stop := startTestServer(t, func(_ context.Context, payload *coretypes.PayLoad) ([]byte, error) {
		return []byte("ok"), nil
	})
	defer stop()

	tr := newInsecureTransport()

	// Before any calls, pool should be empty.
	assert.Equal(t, 0, tr.PooledConns(), "pool should start empty")

	// First call — creates a new connection.
	_, err := tr.Send(context.Background(), &coretypes.PayLoad{TraceID: "c1"}, addr)
	require.NoError(t, err)
	assert.Equal(t, 1, tr.PooledConns(), "pool should have 1 connection after first call")

	// Second call to same target — must reuse the cached connection.
	_, err = tr.Send(context.Background(), &coretypes.PayLoad{TraceID: "c2"}, addr)
	require.NoError(t, err)
	assert.Equal(t, 1, tr.PooledConns(), "pool should still have 1 connection after second call (reused)")

	// Health check also reuses connections.
	require.NoError(t, tr.Health(context.Background(), addr))
	assert.Equal(t, 1, tr.PooledConns(), "Health check should reuse existing connection")
}

func TestGRPCTransport_ConnectionPooling_SeparateTargets(t *testing.T) {
	addr1, stop1 := startTestServer(t, func(_ context.Context, _ *coretypes.PayLoad) ([]byte, error) {
		return []byte("server1"), nil
	})
	defer stop1()
	addr2, stop2 := startTestServer(t, func(_ context.Context, _ *coretypes.PayLoad) ([]byte, error) {
		return []byte("server2"), nil
	})
	defer stop2()

	tr := newInsecureTransport()

	// Call two different targets.
	_, err := tr.Send(context.Background(), &coretypes.PayLoad{}, addr1)
	require.NoError(t, err)
	_, err = tr.Send(context.Background(), &coretypes.PayLoad{}, addr2)
	require.NoError(t, err)

	assert.Equal(t, 2, tr.PooledConns(), "different targets should have separate connections")
}

func TestGRPCTransport_ConnectionPooling_CloseEvictsAll(t *testing.T) {
	addr1, stop1 := startTestServer(t, func(_ context.Context, _ *coretypes.PayLoad) ([]byte, error) {
		return []byte("ok"), nil
	})
	defer stop1()
	addr2, stop2 := startTestServer(t, func(_ context.Context, _ *coretypes.PayLoad) ([]byte, error) {
		return []byte("ok"), nil
	})
	defer stop2()

	tr := newInsecureTransport()

	// Populate pool with two connections.
	_, err := tr.Send(context.Background(), &coretypes.PayLoad{}, addr1)
	require.NoError(t, err)
	_, err = tr.Send(context.Background(), &coretypes.PayLoad{}, addr2)
	require.NoError(t, err)
	assert.Equal(t, 2, tr.PooledConns())

	// Stop should close and evict all pooled connections.
	require.NoError(t, tr.Stop(context.Background()))
	assert.Equal(t, 0, tr.PooledConns(), "Stop should evict all pooled connections")
	require.NoError(t, tr.Stop(context.Background()), "Stop should be idempotent")
	_, err = tr.Send(context.Background(), &coretypes.PayLoad{}, addr1)
	require.ErrorContains(t, err, "stopped")
	assert.Zero(t, tr.PooledConns())
}

func TestGRPCTransport_Timeout(t *testing.T) {
	addr, stop := startRawGRPCServer(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 0) // immediately expire
	defer cancel()

	tr := newInsecureTransport()
	_, err := tr.Send(ctx, &coretypes.PayLoad{}, addr)
	assert.Error(t, err)
}

func TestGRPCTransport_ConcurrentCallsReuseOneZRPCClient(t *testing.T) {
	addr, stop := startTestServer(t, func(_ context.Context, _ *coretypes.PayLoad) ([]byte, error) {
		return []byte("ok"), nil
	})
	defer stop()

	tr := newInsecureTransport()
	defer tr.Stop(context.Background())
	start := make(chan struct{})
	errs := make(chan error, 100)
	var workers sync.WaitGroup
	workers.Add(100)
	for i := 0; i < 100; i++ {
		go func() {
			defer workers.Done()
			<-start
			_, err := tr.Send(context.Background(), &coretypes.PayLoad{TraceID: "pool"}, addr)
			errs <- err
		}()
	}
	close(start)
	workers.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, 1, tr.PooledConns())
}

func TestGRPCTransport_HealthRequiresServing(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	go server.Serve(lis)
	defer server.Stop()

	tr := newInsecureTransport()
	defer tr.Stop(context.Background())
	err = tr.Health(context.Background(), lis.Addr().String())
	require.ErrorContains(t, err, "NOT_SERVING")
}

func TestGRPCTransport_CancelledContextDoesNotReachServer(t *testing.T) {
	var calls atomic.Int64
	addr, stop := startTestServer(t, func(_ context.Context, _ *coretypes.PayLoad) ([]byte, error) {
		calls.Add(1)
		return []byte("unexpected"), nil
	})
	defer stop()

	tr := newInsecureTransport()
	defer tr.Stop(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tr.Send(ctx, &coretypes.PayLoad{}, addr)
	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, calls.Load())
}
