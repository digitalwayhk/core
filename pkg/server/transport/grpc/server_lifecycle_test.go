package grpc

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/digitalwayhk/core/pkg/server/config"
	pb "github.com/digitalwayhk/core/pkg/server/transport/grpc/proto"
	coretypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerLifecycleRejectsOccupiedAddressDuringConstruction(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	server, err := NewServer(listener.Addr().String(), insecureServerConfig(), echoHandler)
	require.Error(t, err)
	assert.Nil(t, server)
}

func TestServerLifecycleRejectsInvalidAddressDuringConstruction(t *testing.T) {
	server, err := NewServer("127.0.0.1:not-a-port", insecureServerConfig(), echoHandler)
	require.Error(t, err)
	assert.Nil(t, server)
}

func TestServerLifecycleReadyAndHealthServing(t *testing.T) {
	server, startResult := startLifecycleServer(t, echoHandler)
	client := newLifecycleClient(t, server.Address())
	defer client.Close()

	response, err := grpc_health_v1.NewHealthClient(client).Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	require.NoError(t, err)
	assert.Equal(t, grpc_health_v1.HealthCheckResponse_SERVING, response.Status)

	require.NoError(t, server.StopContext(context.Background()))
	require.NoError(t, <-startResult)
}

func TestServerLifecycleConcurrentStartFailsClosed(t *testing.T) {
	server, startResult := startLifecycleServer(t, echoHandler)

	err := server.Start()
	require.ErrorIs(t, err, errServerAlreadyStarted)

	server.Stop()
	require.NoError(t, <-startResult)
}

func TestServerLifecycleStopPublishesNotServingBeforeGracefulWait(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	server, startResult := startLifecycleServer(t, func(ctx context.Context, _ *coretypes.PayLoad) ([]byte, error) {
		once.Do(func() { close(entered) })
		select {
		case <-release:
			return []byte("released"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	client := newLifecycleClient(t, server.Address())
	defer client.Close()

	rpcResult := make(chan error, 1)
	go func() {
		_, err := pb.NewCoreTransportClient(client).Call(context.Background(), &pb.PayloadRequest{})
		rpcResult <- err
	}()
	<-entered

	stopResult := make(chan error, 1)
	go func() { stopResult <- server.StopContext(context.Background()) }()
	<-server.stopping
	status, err := server.health.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	require.NoError(t, err)
	assert.Equal(t, grpc_health_v1.HealthCheckResponse_NOT_SERVING, status.Status)

	close(release)
	require.NoError(t, <-rpcResult)
	require.NoError(t, <-stopResult)
	require.NoError(t, <-startResult)
}

func TestServerLifecycleStopContextForcesBlockedRPCAtDeadline(t *testing.T) {
	entered := make(chan struct{})
	var once sync.Once
	server, startResult := startLifecycleServer(t, func(ctx context.Context, _ *coretypes.PayLoad) ([]byte, error) {
		once.Do(func() { close(entered) })
		<-ctx.Done()
		return nil, ctx.Err()
	})
	client := newLifecycleClient(t, server.Address())
	defer client.Close()

	rpcResult := make(chan error, 1)
	go func() {
		_, err := pb.NewCoreTransportClient(client).Call(context.Background(), &pb.PayloadRequest{})
		rpcResult <- err
	}()
	<-entered

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := server.StopContext(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Error(t, <-rpcResult)
	require.NoError(t, <-startResult)
	select {
	case <-server.Done():
	case <-time.After(time.Second):
		t.Fatal("forced gRPC stop did not close Done")
	}
}

func TestServerLifecycleTwoServersAreIndependent(t *testing.T) {
	first, firstResult := startLifecycleServer(t, echoHandler)
	second, secondResult := startLifecycleServer(t, echoHandler)
	secondClient := newLifecycleClient(t, second.Address())
	defer secondClient.Close()

	first.Stop()
	require.NoError(t, <-firstResult)

	healthResponse, err := grpc_health_v1.NewHealthClient(secondClient).Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	require.NoError(t, err)
	assert.Equal(t, grpc_health_v1.HealthCheckResponse_SERVING, healthResponse.Status)

	second.Stop()
	require.NoError(t, <-secondResult)
}

func TestServerCanStopAndRebuildIndependently(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := probe.Addr().String()
	require.NoError(t, probe.Close())

	first, err := NewServer(address, insecureServerConfig(), echoHandler)
	require.NoError(t, err)
	firstResult := make(chan error, 1)
	go func() { firstResult <- first.Start() }()
	waitReady(t, first)
	first.Stop()
	require.NoError(t, <-firstResult)

	second, err := NewServer(address, insecureServerConfig(), echoHandler)
	require.NoError(t, err)
	secondResult := make(chan error, 1)
	go func() { secondResult <- second.Start() }()
	waitReady(t, second)
	second.Stop()
	require.NoError(t, <-secondResult)
}

func TestServerLifecycleRepeatedStopIsIdempotent(t *testing.T) {
	server, startResult := startLifecycleServer(t, echoHandler)

	server.Stop()
	server.Stop()
	require.NoError(t, server.StopContext(context.Background()))
	require.NoError(t, <-startResult)
}

func insecureServerConfig() config.GRPCTransportConfig {
	return config.GRPCTransportConfig{Security: config.GRPCSecurityConfig{Mode: "insecure"}}
}

func echoHandler(_ context.Context, payload *coretypes.PayLoad) ([]byte, error) {
	return []byte(payload.TraceID), nil
}

func startLifecycleServer(t *testing.T, handler func(context.Context, *coretypes.PayLoad) ([]byte, error)) (*Server, <-chan error) {
	t.Helper()
	server, err := NewServer("127.0.0.1:0", insecureServerConfig(), handler)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() { result <- server.Start() }()
	waitReady(t, server)
	t.Cleanup(func() { server.Stop() })
	return server, result
}

func waitReady(t *testing.T, server *Server) {
	t.Helper()
	select {
	case <-server.Ready():
	case <-time.After(time.Second):
		t.Fatal("gRPC server did not become ready")
	}
}

func newLifecycleClient(t *testing.T, address string) *googlegrpc.ClientConn {
	t.Helper()
	client, err := googlegrpc.NewClient(address, googlegrpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	return client
}
