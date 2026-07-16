// Package grpc provides a Transport implementation using gRPC for high-performance
// inter-service calls within the core framework.
package grpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/digitalwayhk/core/pkg/server/config"
	pb "github.com/digitalwayhk/core/pkg/server/transport/grpc/proto"
	coretypes "github.com/digitalwayhk/core/pkg/server/types"
)

var (
	errServerAlreadyStarted = errors.New("grpc server already started")
	errServerStopping       = errors.New("grpc server is stopping or stopped")
)

const defaultServerStopTimeout = 5 * time.Second

// Server wraps the gRPC server and implements the CoreTransport service.
type Server struct {
	pb.UnimplementedCoreTransportServer
	listener net.Listener
	address  string
	grpcSrv  *grpc.Server
	health   *health.Server
	ready    chan struct{}
	stopping chan struct{}
	done     chan struct{}

	stateMu   sync.Mutex
	started   bool
	stopped   bool
	readyOnce sync.Once
	stopOnce  sync.Once
	doneOnce  sync.Once

	// handler is called to process incoming Call RPCs.
	handler func(ctx context.Context, payload *coretypes.PayLoad) ([]byte, error)
}

// NewServer pre-binds address and loads server credentials so startup errors are
// returned before the server is handed to a service lifecycle.
func NewServer(address string, cfg config.GRPCTransportConfig, handler func(ctx context.Context, payload *coretypes.PayLoad) ([]byte, error)) (*Server, error) {
	options, err := serverSecurityOptions(cfg.Security)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("grpc server: listen on %q: %w", address, err)
	}
	server := &Server{
		listener: listener,
		address:  listener.Addr().String(),
		grpcSrv:  grpc.NewServer(options...),
		health:   health.NewServer(),
		ready:    make(chan struct{}),
		stopping: make(chan struct{}),
		done:     make(chan struct{}),
		handler:  handler,
	}
	return server, nil
}

// Start registers services, publishes readiness, and blocks in Serve. A Server
// instance can be started exactly once.
func (s *Server) Start() error {
	s.stateMu.Lock()
	if s.started {
		s.stateMu.Unlock()
		return errServerAlreadyStarted
	}
	if s.stopped {
		s.stateMu.Unlock()
		return errServerStopping
	}
	s.started = true
	pb.RegisterCoreTransportServer(s.grpcSrv, s)
	grpc_health_v1.RegisterHealthServer(s.grpcSrv, s.health)
	s.health.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	s.readyOnce.Do(func() { close(s.ready) })
	s.stateMu.Unlock()

	err := s.grpcSrv.Serve(s.listener)
	_ = s.listener.Close()
	s.doneOnce.Do(func() { close(s.done) })

	s.stateMu.Lock()
	stopped := s.stopped
	s.stateMu.Unlock()
	if stopped || errors.Is(err, grpc.ErrServerStopped) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// Ready is closed immediately before Start enters Serve.
func (s *Server) Ready() <-chan struct{} { return s.ready }

// Done is closed after Serve exits and the listener has been released.
func (s *Server) Done() <-chan struct{} { return s.done }

// Address returns the pre-bound listener address.
func (s *Server) Address() string { return s.address }

// StopContext marks the server NOT_SERVING before graceful shutdown. If ctx
// expires, in-flight RPCs are forcefully stopped.
func (s *Server) StopContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.stopOnce.Do(func() {
		s.stateMu.Lock()
		s.stopped = true
		started := s.started
		s.health.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
		close(s.stopping)
		s.stateMu.Unlock()

		if !started {
			_ = s.listener.Close()
			s.grpcSrv.Stop()
			s.doneOnce.Do(func() { close(s.done) })
			return
		}
		go s.grpcSrv.GracefulStop()
	})

	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		s.grpcSrv.Stop()
		_ = s.listener.Close()
		<-s.done
		return ctx.Err()
	}
}

// Stop uses a bounded graceful-shutdown budget and is safe to call repeatedly.
func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), defaultServerStopTimeout)
	defer cancel()
	_ = s.StopContext(ctx)
}

// Call implements pb.CoreTransportServer.
func (s *Server) Call(ctx context.Context, req *pb.PayloadRequest) (*pb.PayloadResponse, error) {
	payload := pbToPayload(req)
	data, err := s.handler(ctx, payload)
	if err != nil {
		return &pb.PayloadResponse{Error: err.Error()}, nil
	}
	return &pb.PayloadResponse{Data: data}, nil
}

// Health implements pb.CoreTransportServer.
func (s *Server) Health(_ context.Context, _ *pb.HealthRequest) (*pb.HealthResponse, error) {
	return &pb.HealthResponse{Healthy: true}, nil
}

// Check reports whether the Server has started and is not stopping.
func (s *Server) Check() bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.started && !s.stopped
}

func pbToPayload(req *pb.PayloadRequest) *coretypes.PayLoad {
	return &coretypes.PayLoad{
		TraceID:          req.TraceId,
		SourceAddress:    req.SourceAddress,
		SourcePort:       int(req.SourcePort),
		SourceSocketPort: int(req.SourceSocketPort),
		SourceService:    req.SourceService,
		TargetAddress:    req.TargetAddress,
		TargetPort:       int(req.TargetPort),
		TargetSocketPort: int(req.TargetSocketPort),
		TargetService:    req.TargetService,
		SourcePath:       req.SourcePath,
		TargetPath:       req.TargetPath,
		UserId:           req.UserId,
		UserName:         req.UserName,
		ClientIP:         req.ClientIp,
		Auth:             req.Auth,
		Data:             req.Data,
		HttpMethod:       req.HttpMethod,
		Token:            req.Token,
	}
}
