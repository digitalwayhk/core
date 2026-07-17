package grpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"

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
	srv, err := grpctransport.NewServer("127.0.0.1:0", config.GRPCTransportConfig{
		Security: config.GRPCSecurityConfig{Mode: "insecure"},
	}, handler)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() { result <- srv.Serve() }()
	select {
	case <-srv.Ready():
	case <-time.After(time.Second):
		t.Fatal("gRPC test server did not become ready")
	}
	return srv.Address(), func() {
		srv.Stop()
		require.NoError(t, <-result)
	}
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
		SourceAddress: "192.0.2.10",
		SourcePort:    8080,
		SourceService: "svc-a",
		TargetAddress: "192.0.2.20",
		TargetPort:    8081,
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
	assert.Empty(t, received.SourceAddress)
	assert.Zero(t, received.SourcePort)
	assert.Empty(t, received.TargetAddress)
	assert.Zero(t, received.TargetPort)
}

func TestGRPCTransport_TypeSafeInstanceRoundTrip(t *testing.T) {
	type requestBody struct {
		ProductID uint `json:"productID"`
		Quantity  int  `json:"quantity"`
	}
	var decoded requestBody
	addr, stop := startTestServer(t, func(_ context.Context, payload *coretypes.PayLoad) ([]byte, error) {
		require.NoError(t, json.Unmarshal(payload.Data, &decoded))
		require.IsType(t, json.RawMessage{}, payload.Instance)
		return []byte("ok"), nil
	})
	defer stop()

	transport := newInsecureTransport()
	t.Cleanup(func() { _ = transport.Stop(context.Background()) })
	data, err := transport.Send(context.Background(), &coretypes.PayLoad{
		Instance: &requestBody{ProductID: 42, Quantity: 3},
	}, addr)
	require.NoError(t, err)
	assert.Equal(t, []byte("ok"), data)
	assert.Equal(t, requestBody{ProductID: 42, Quantity: 3}, decoded)
}

func TestCoreTransportDescriptorExcludesPrivateHealthAndEndpointFields(t *testing.T) {
	file := pb.File_pkg_server_transport_grpc_proto_payload_proto
	service := file.Services().ByName("CoreTransport")
	require.NotNil(t, service)
	require.Equal(t, 1, service.Methods().Len())
	assert.Equal(t, protoreflect.Name("Call"), service.Methods().Get(0).Name())
	assert.Nil(t, service.Methods().ByName("Health"))
	assert.Nil(t, file.Messages().ByName("HealthRequest"))
	assert.Nil(t, file.Messages().ByName("HealthResponse"))

	request := file.Messages().ByName("PayloadRequest")
	require.NotNil(t, request)
	reservedNumbers := map[protoreflect.FieldNumber]bool{}
	for i := 0; i < request.ReservedRanges().Len(); i++ {
		fieldRange := request.ReservedRanges().Get(i)
		for number := fieldRange[0]; number < fieldRange[1]; number++ {
			reservedNumbers[number] = true
		}
	}
	for _, number := range []protoreflect.FieldNumber{2, 3, 4, 6, 7, 8} {
		assert.Truef(t, reservedNumbers[number], "field number %d must be reserved", number)
	}

	reservedNames := map[protoreflect.Name]bool{}
	for i := 0; i < request.ReservedNames().Len(); i++ {
		reservedNames[request.ReservedNames().Get(i)] = true
	}
	for _, name := range []protoreflect.Name{
		"source_address", "source_port", "source_socket_port",
		"target_address", "target_port", "target_socket_port",
	} {
		assert.Nilf(t, request.Fields().ByName(name), "field %q must not exist", name)
		assert.Truef(t, reservedNames[name], "field name %q must be reserved", name)
	}
}

func TestGRPCTransport_HandlerErrorUsesSafeInternalStatus(t *testing.T) {
	const privateError = "database password leaked"
	addr, stop := startTestServer(t, func(context.Context, *coretypes.PayLoad) ([]byte, error) {
		return nil, errors.New(privateError)
	})
	defer stop()

	tr := newInsecureTransport()
	defer tr.Stop(context.Background())
	data, err := tr.Send(context.Background(), &coretypes.PayLoad{}, addr)
	require.Error(t, err)
	assert.Nil(t, data)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.Equal(t, "internal server error", status.Convert(err).Message())
	assert.NotContains(t, err.Error(), privateError)
}

type legacyErrorServer struct {
	pb.UnimplementedCoreTransportServer
	errorText string
}

func (s *legacyErrorServer) Call(context.Context, *pb.PayloadRequest) (*pb.PayloadResponse, error) {
	return &pb.PayloadResponse{Error: s.errorText}, nil
}

func TestGRPCTransport_LegacyResponseErrorIsSanitized(t *testing.T) {
	const privateError = "legacy database password leaked"
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	pb.RegisterCoreTransportServer(server, &legacyErrorServer{errorText: privateError})
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	transport := newInsecureTransport()
	t.Cleanup(func() { _ = transport.Stop(context.Background()) })
	data, err := transport.Send(context.Background(), &coretypes.PayLoad{}, lis.Addr().String())
	require.Error(t, err)
	assert.Nil(t, data)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.Equal(t, "internal server error", status.Convert(err).Message())
	assert.NotContains(t, err.Error(), privateError)
}

func TestGRPCTransport_BusinessFailureResponseRemainsData(t *testing.T) {
	businessResponse := []byte(`{"success":false,"errorCode":700,"errorMessage":"validation failed"}`)
	addr, stop := startTestServer(t, func(context.Context, *coretypes.PayLoad) ([]byte, error) {
		return businessResponse, nil
	})
	defer stop()

	transport := newInsecureTransport()
	t.Cleanup(func() { _ = transport.Stop(context.Background()) })
	data, err := transport.Send(context.Background(), &coretypes.PayLoad{}, addr)
	require.NoError(t, err)
	assert.Equal(t, businessResponse, data)
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
func startRawGRPCServer(t *testing.T) (addr string, entered <-chan struct{}, stop func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer()
	// Register a handler that blocks forever to test timeout.
	blocking := &blockingServer{entered: make(chan struct{})}
	pb.RegisterCoreTransportServer(srv, blocking)
	go srv.Serve(lis)
	return lis.Addr().String(), blocking.entered, srv.GracefulStop
}

// blockingServer is a server that never responds (for timeout tests).
type blockingServer struct {
	pb.UnimplementedCoreTransportServer
	entered chan struct{}
	once    sync.Once
}

func (b *blockingServer) Call(ctx context.Context, _ *pb.PayloadRequest) (*pb.PayloadResponse, error) {
	b.once.Do(func() { close(b.entered) })
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
	addr, entered, stop := startRawGRPCServer(t)
	defer stop()

	tr := newInsecureTransport()
	defer tr.Stop(context.Background())
	started := time.Now()
	_, err := tr.Send(context.Background(), &coretypes.PayLoad{}, addr)
	require.Equal(t, codes.DeadlineExceeded, status.Code(err))
	assert.Less(t, time.Since(started), 4*time.Second)
	select {
	case <-entered:
	default:
		t.Fatal("blocking RPC was not entered")
	}
}

func TestGRPCTransport_StopInterruptsInFlightSend(t *testing.T) {
	addr, entered, stopServer := startRawGRPCServer(t)
	defer stopServer()
	transport := newInsecureTransport()
	result := make(chan error, 1)
	go func() {
		_, err := transport.Send(context.Background(), &coretypes.PayLoad{}, addr)
		result <- err
	}()
	<-entered
	require.NoError(t, transport.Stop(context.Background()))
	select {
	case err := <-result:
		require.Error(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("in-flight Send did not return after Stop")
	}
	assert.Zero(t, transport.PooledConns())
}

func TestGRPCTransport_StopInterruptsInFlightHealth(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	blocking := &blockingHealthServer{entered: make(chan struct{})}
	grpc_health_v1.RegisterHealthServer(server, blocking)
	go server.Serve(lis)
	defer server.GracefulStop()

	transport := newInsecureTransport()
	result := make(chan error, 1)
	go func() { result <- transport.Health(context.Background(), lis.Addr().String()) }()
	<-blocking.entered
	require.NoError(t, transport.Stop(context.Background()))
	select {
	case err := <-result:
		require.Error(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("in-flight Health did not return after Stop")
	}
	assert.Zero(t, transport.PooledConns())
}

type blockingHealthServer struct {
	grpc_health_v1.UnimplementedHealthServer
	entered chan struct{}
	once    sync.Once
}

func (s *blockingHealthServer) Check(ctx context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	s.once.Do(func() { close(s.entered) })
	<-ctx.Done()
	return nil, ctx.Err()
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
