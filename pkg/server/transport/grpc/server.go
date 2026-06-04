// Package grpc provides a Transport implementation using gRPC for high-performance
// inter-service calls within the core framework.
package grpc

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"

	coretypes "github.com/digitalwayhk/core/pkg/server/types"
	pb "github.com/digitalwayhk/core/pkg/server/transport/grpc/proto"
)

// Server wraps the gRPC server and implements the CoreTransport service.
type Server struct {
	pb.UnimplementedCoreTransportServer
	port    int
	grpcSrv *grpc.Server
	// handler is called to process incoming Call RPCs.
	handler func(ctx context.Context, payload *coretypes.PayLoad) ([]byte, error)
}

// NewServer creates a gRPC server listening on port, using handler for incoming calls.
func NewServer(port int, handler func(ctx context.Context, payload *coretypes.PayLoad) ([]byte, error)) *Server {
	return &Server{port: port, handler: handler}
}

// Start begins listening and serving gRPC requests.
func (s *Server) Start(ctx context.Context) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("grpc server: listen on port %d: %w", s.port, err)
	}
	s.grpcSrv = grpc.NewServer()
	pb.RegisterCoreTransportServer(s.grpcSrv, s)
	go func() {
		<-ctx.Done()
		s.grpcSrv.GracefulStop()
	}()
	go func() {
		if err := s.grpcSrv.Serve(lis); err != nil {
			// Serve returns after GracefulStop; suppress that error.
		}
	}()
	return nil
}

// Stop gracefully shuts down the gRPC server.
func (s *Server) Stop(_ context.Context) error {
	if s.grpcSrv != nil {
		s.grpcSrv.GracefulStop()
	}
	return nil
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

// Check implements a simple health check (for future grpc_health_v1 integration).
func (s *Server) Check() bool {
	return true
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
