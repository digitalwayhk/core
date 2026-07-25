package grpc

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/core/service"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/digitalwayhk/core/pkg/server/config"
	pb "github.com/digitalwayhk/core/pkg/server/transport/grpc/proto"
	coretypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ service.Service = (*Server)(nil)

func TestServerLifecycleRejectsNilHandlerDuringConstruction(t *testing.T) {
	server, err := NewServer("127.0.0.1:0", insecureServerConfig(), nil)
	require.EqualError(t, err, "grpc server: handler is required")
	assert.Nil(t, server)
}

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

	err := server.Serve()
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
	go func() { firstResult <- first.Serve() }()
	waitReady(t, first)
	first.Stop()
	require.NoError(t, <-firstResult)

	second, err := NewServer(address, insecureServerConfig(), echoHandler)
	require.NoError(t, err)
	secondResult := make(chan error, 1)
	go func() { secondResult <- second.Serve() }()
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

func TestServerLifecycleUnexpectedServeExitIsObservableAndTerminal(t *testing.T) {
	server, err := NewServer("127.0.0.1:0", insecureServerConfig(), echoHandler)
	require.NoError(t, err)
	require.NoError(t, server.listener.Close())

	server.Start()
	serveErr := server.Err()
	require.Error(t, serveErr)
	require.ErrorContains(t, serveErr, server.Address())
	assert.False(t, server.Check())
	select {
	case <-server.Ready():
	default:
		t.Fatal("Serve did not publish its attempted ready transition")
	}
	select {
	case <-server.Done():
	default:
		t.Fatal("unexpected Serve exit did not close Done")
	}
}

func TestServerLifecycleHandlerPanicIsIsolatedAndServerRemainsUsable(t *testing.T) {
	var calls atomic.Int64
	panicServer, panicResult := startLifecycleServer(t, func(_ context.Context, _ *coretypes.PayLoad) ([]byte, error) {
		if calls.Add(1) == 1 {
			panic("secret panic detail")
		}
		return []byte("recovered"), nil
	})
	otherServer, otherResult := startLifecycleServer(t, func(_ context.Context, _ *coretypes.PayLoad) ([]byte, error) {
		return []byte("other-ok"), nil
	})
	panicClient := newLifecycleClient(t, panicServer.Address())
	defer panicClient.Close()
	otherClient := newLifecycleClient(t, otherServer.Address())
	defer otherClient.Close()

	_, err := pb.NewCoreTransportClient(panicClient).Call(context.Background(), &pb.PayloadRequest{})
	require.Equal(t, codes.Internal, status.Code(err))
	assert.Equal(t, "rpc error: code = Internal desc = internal server error", err.Error())

	response, err := pb.NewCoreTransportClient(panicClient).Call(context.Background(), &pb.PayloadRequest{})
	require.NoError(t, err)
	assert.Equal(t, []byte("recovered"), response.Data)
	otherResponse, err := pb.NewCoreTransportClient(otherClient).Call(context.Background(), &pb.PayloadRequest{})
	require.NoError(t, err)
	assert.Equal(t, []byte("other-ok"), otherResponse.Data)

	panicServer.Stop()
	otherServer.Stop()
	require.NoError(t, <-panicResult)
	require.NoError(t, <-otherResult)
}

func TestServerLifecycleMessageLimitsAreAppliedAndIsolated(t *testing.T) {
	largePayload := make([]byte, 2048)
	requestHandler := func(_ context.Context, payload *coretypes.PayLoad) ([]byte, error) {
		return []byte{byte(len(payload.Data) % 251)}, nil
	}
	smallRecv, smallRecvResult := startLifecycleServerWithConfig(t, config.GRPCTransportConfig{
		MaxRecvMsgSize: 256, Security: config.GRPCSecurityConfig{Mode: "insecure"},
	}, requestHandler)
	largeRecv, largeRecvResult := startLifecycleServerWithConfig(t, config.GRPCTransportConfig{
		MaxRecvMsgSize: 4096, Security: config.GRPCSecurityConfig{Mode: "insecure"},
	}, requestHandler)

	_, err := callLifecycleServer(t, smallRecv.Address(), &pb.PayloadRequest{Data: largePayload})
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	_, err = callLifecycleServer(t, largeRecv.Address(), &pb.PayloadRequest{Data: largePayload})
	require.NoError(t, err)

	responseHandler := func(context.Context, *coretypes.PayLoad) ([]byte, error) { return largePayload, nil }
	smallSend, smallSendResult := startLifecycleServerWithConfig(t, config.GRPCTransportConfig{
		MaxSendMsgSize: 256, Security: config.GRPCSecurityConfig{Mode: "insecure"},
	}, responseHandler)
	largeSend, largeSendResult := startLifecycleServerWithConfig(t, config.GRPCTransportConfig{
		MaxSendMsgSize: 4096, Security: config.GRPCSecurityConfig{Mode: "insecure"},
	}, responseHandler)
	_, err = callLifecycleServer(t, smallSend.Address(), &pb.PayloadRequest{})
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	response, err := callLifecycleServer(t, largeSend.Address(), &pb.PayloadRequest{})
	require.NoError(t, err)
	assert.Equal(t, largePayload, response.Data)

	for _, server := range []*Server{smallRecv, largeRecv, smallSend, largeSend} {
		server.Stop()
	}
	for _, result := range []<-chan error{smallRecvResult, largeRecvResult, smallSendResult, largeSendResult} {
		require.NoError(t, <-result)
	}
}

func TestServerLifecycleConcurrentStopContextSharesForcedResult(t *testing.T) {
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

	backgroundResult := make(chan error, 1)
	go func() { backgroundResult <- server.StopContext(context.Background()) }()
	<-server.stopping
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	deadlineResult := server.StopContext(ctx)
	require.ErrorIs(t, deadlineResult, context.DeadlineExceeded)
	require.ErrorIs(t, <-backgroundResult, context.DeadlineExceeded)
	require.ErrorIs(t, server.LastStopError(), context.DeadlineExceeded)
	require.Error(t, <-rpcResult)
	require.NoError(t, <-startResult)
	require.ErrorIs(t, server.StopContext(context.Background()), context.DeadlineExceeded)
}

func TestServerLifecycleConcurrentStopContextSharesGracefulResult(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	server, startResult := startLifecycleServer(t, func(ctx context.Context, _ *coretypes.PayLoad) ([]byte, error) {
		close(entered)
		select {
		case <-release:
			return []byte("ok"), nil
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

	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() { first <- server.StopContext(context.Background()) }()
	<-server.stopping
	go func() { second <- server.StopContext(context.Background()) }()
	close(release)
	require.NoError(t, <-rpcResult)
	require.NoError(t, <-first)
	require.NoError(t, <-second)
	assert.NoError(t, server.LastStopError())
	require.NoError(t, <-startResult)
}

func TestServerLifecyclePreCancelledStopContextSharesCancelledResult(t *testing.T) {
	entered := make(chan struct{})
	server, startResult := startLifecycleServer(t, func(ctx context.Context, _ *coretypes.PayLoad) ([]byte, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	client := newLifecycleClient(t, server.Address())
	defer client.Close()
	go pb.NewCoreTransportClient(client).Call(context.Background(), &pb.PayloadRequest{})
	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, server.StopContext(ctx), context.Canceled)
	require.ErrorIs(t, server.StopContext(context.Background()), context.Canceled)
	require.ErrorIs(t, server.LastStopError(), context.Canceled)
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
	return startLifecycleServerWithConfig(t, insecureServerConfig(), handler)
}

func startLifecycleServerWithConfig(t *testing.T, cfg config.GRPCTransportConfig, handler func(context.Context, *coretypes.PayLoad) ([]byte, error)) (*Server, <-chan error) {
	t.Helper()
	server, err := NewServer("127.0.0.1:0", cfg, handler)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() { result <- server.Serve() }()
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

func callLifecycleServer(t *testing.T, address string, request *pb.PayloadRequest) (*pb.PayloadResponse, error) {
	t.Helper()
	client := newLifecycleClient(t, address)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return pb.NewCoreTransportClient(client).Call(ctx, request)
}
