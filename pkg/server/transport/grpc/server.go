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

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/digitalwayhk/core/pkg/server/config"
	pb "github.com/digitalwayhk/core/pkg/server/transport/grpc/proto"
	coretypes "github.com/digitalwayhk/core/pkg/server/types"
)

var (
	errServerAlreadyStarted = errors.New("grpc server already started")
	errServerStopping       = errors.New("grpc server is stopping or stopped")
)

const defaultServerStopTimeout = 5 * time.Second

var _ service.Service = (*Server)(nil)
var _ coretypes.GRPCServerLifecycle = (*Server)(nil)

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

	stateMu       sync.RWMutex
	started       bool
	stopRequested bool
	completed     bool
	serveErr      error
	stopErr       error
	readyOnce     sync.Once
	stopOnce      sync.Once
	forceOnce     sync.Once
	doneOnce      sync.Once

	// handler is called to process incoming Call RPCs.
	handler func(ctx context.Context, payload *coretypes.PayLoad) ([]byte, error)
}

// NewServer pre-binds address and loads server credentials so startup errors are
// returned before the server is handed to a service lifecycle.
func NewServer(address string, cfg config.GRPCTransportConfig, handler func(ctx context.Context, payload *coretypes.PayLoad) ([]byte, error)) (*Server, error) {
	if handler == nil {
		return nil, errors.New("grpc server: handler is required")
	}
	options, err := serverSecurityOptions(cfg.Security)
	if err != nil {
		return nil, err
	}
	if cfg.MaxRecvMsgSize <= 0 {
		cfg.MaxRecvMsgSize = defaultMessageSize
	}
	if cfg.MaxSendMsgSize <= 0 {
		cfg.MaxSendMsgSize = defaultMessageSize
	}
	options = append(options,
		grpc.MaxRecvMsgSize(cfg.MaxRecvMsgSize),
		grpc.MaxSendMsgSize(cfg.MaxSendMsgSize),
	)
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

// Start implements service.Service. Serve errors remain observable through Err
// and are logged once at the lifecycle boundary.
func (s *Server) Start() {
	_ = s.Serve()
}

// Serve registers services, publishes readiness, and blocks in grpc.Serve. It
// returns lifecycle errors to tests and explicit callers; an instance starts once.
func (s *Server) Serve() error {
	s.stateMu.Lock()
	if s.started {
		s.stateMu.Unlock()
		logx.Errorw("grpc_server_start_failed", logx.Field("address", s.address), logx.Field("error", errServerAlreadyStarted))
		return errServerAlreadyStarted
	}
	if s.stopRequested || s.completed {
		s.stateMu.Unlock()
		logx.Errorw("grpc_server_start_failed", logx.Field("address", s.address), logx.Field("error", errServerStopping))
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
	s.stateMu.Lock()
	activeStop := s.stopRequested
	normalStop := activeStop && (err == nil || errors.Is(err, grpc.ErrServerStopped) || errors.Is(err, net.ErrClosed))
	if !normalStop {
		if err == nil {
			err = errors.New("serve exited unexpectedly")
		}
		err = fmt.Errorf("grpc server %s serve failed: %w", s.address, err)
		s.serveErr = err
		s.stopRequested = true
		s.health.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	}
	s.stateMu.Unlock()
	if !normalStop {
		s.grpcSrv.Stop()
		logx.Errorw("grpc_server_serve_failed", logx.Field("address", s.address), logx.Field("error", err))
	}
	s.stateMu.Lock()
	s.completed = true
	s.stateMu.Unlock()
	s.doneOnce.Do(func() { close(s.done) })
	if normalStop {
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

// Err returns an unexpected Serve terminal error, if any.
func (s *Server) Err() error {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.serveErr
}

// LastStopError returns the shared forced-shutdown result observed by every
// StopContext caller. A graceful shutdown leaves it nil.
func (s *Server) LastStopError() error {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.stopErr
}

// BeginShutdown 同步将标准健康状态切换为 NOT_SERVING。
// ServiceContext 使用该边界保证先停止接收流量，再注销服务发现。
func (s *Server) BeginShutdown() {
	s.stateMu.Lock()
	if !s.completed {
		s.stopRequested = true
		s.health.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	}
	s.stateMu.Unlock()
}

// StopContext marks the server NOT_SERVING before graceful shutdown. If ctx
// expires, in-flight RPCs are forcefully stopped.
func (s *Server) StopContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.stopOnce.Do(func() {
		s.stateMu.Lock()
		if s.completed {
			s.stateMu.Unlock()
			return
		}
		s.stopRequested = true
		started := s.started
		s.health.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
		close(s.stopping)
		s.stateMu.Unlock()

		if !started {
			_ = s.listener.Close()
			s.grpcSrv.Stop()
			s.stateMu.Lock()
			s.completed = true
			s.stateMu.Unlock()
			s.doneOnce.Do(func() { close(s.done) })
			return
		}
		go s.grpcSrv.GracefulStop()
	})

	select {
	case <-s.done:
		return s.LastStopError()
	case <-ctx.Done():
		s.forceStop(ctx.Err())
		<-s.done
		return s.LastStopError()
	}
}

func (s *Server) forceStop(reason error) {
	s.forceOnce.Do(func() {
		s.stateMu.Lock()
		if s.completed {
			s.stateMu.Unlock()
			return
		}
		s.stopErr = reason
		s.stateMu.Unlock()
		s.grpcSrv.Stop()
		_ = s.listener.Close()
	})
}

// Stop uses a bounded graceful-shutdown budget and is safe to call repeatedly.
func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), defaultServerStopTimeout)
	defer cancel()
	if err := s.StopContext(ctx); err != nil {
		logx.Errorw("grpc_server_stop_failed", logx.Field("address", s.address), logx.Field("error", err))
	}
}

// Call implements pb.CoreTransportServer.
func (s *Server) Call(ctx context.Context, req *pb.PayloadRequest) (response *pb.PayloadResponse, err error) {
	defer func() {
		if recover() != nil {
			logx.Errorw("grpc_handler_panicked", logx.Field("address", s.address))
			response = nil
			err = status.Error(codes.Internal, "internal server error")
		}
	}()
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
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.started && !s.stopRequested && !s.completed && s.serveErr == nil
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
