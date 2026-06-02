package grpc_test

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"

	coretypes "github.com/digitalwayhk/core/pkg/server/types"
	grpctransport "github.com/digitalwayhk/core/pkg/server/transport/grpc"
	pb "github.com/digitalwayhk/core/pkg/server/transport/grpc/proto"
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
	go grpcSrv.Serve(lis)
	return lis.Addr().String(), grpcSrv.GracefulStop
}

func TestGRPCTransport_SendAndReceive(t *testing.T) {
	addr, stop := startTestServer(t, func(_ context.Context, payload *coretypes.PayLoad) ([]byte, error) {
		return []byte("pong-" + payload.TraceID), nil
	})
	defer stop()

	tr := grpctransport.New(0, 0)
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

	tr := grpctransport.New(0, 0)
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

	tr := grpctransport.New(0, 0)
	assert.NoError(t, tr.Health(context.Background(), addr))
}

func TestGRPCTransport_Health_Unreachable(t *testing.T) {
	tr := grpctransport.New(0, 0)
	// Use a port that should not be listening; give a short deadline so the test
	// doesn't block forever waiting for gRPC's internal connect backoff.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := tr.Health(ctx, "127.0.0.1:19999")
	assert.Error(t, err)
}

func TestGRPCTransport_Supports(t *testing.T) {
	tr := grpctransport.New(0, 0)
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

func TestGRPCTransport_Timeout(t *testing.T) {
	addr, stop := startRawGRPCServer(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 0) // immediately expire
	defer cancel()

	tr := grpctransport.New(0, 0)
	_, err := tr.Send(ctx, &coretypes.PayLoad{}, addr)
	assert.Error(t, err)
}

