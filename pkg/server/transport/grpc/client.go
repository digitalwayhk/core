package grpc

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	coretypes "github.com/digitalwayhk/core/pkg/server/types"
	pb "github.com/digitalwayhk/core/pkg/server/transport/grpc/proto"
)

// GRPCTransport implements transport.Transport using gRPC.
type GRPCTransport struct {
	maxRecvMsgSize int
	maxSendMsgSize int
}

// New returns a GRPCTransport. If msgSize is 0 the default (4 MiB) is used.
func New(maxRecvMsgSize, maxSendMsgSize int) *GRPCTransport {
	if maxRecvMsgSize <= 0 {
		maxRecvMsgSize = 4 * 1024 * 1024
	}
	if maxSendMsgSize <= 0 {
		maxSendMsgSize = 4 * 1024 * 1024
	}
	return &GRPCTransport{maxRecvMsgSize: maxRecvMsgSize, maxSendMsgSize: maxSendMsgSize}
}

func (g *GRPCTransport) Name() string { return "grpc" }

func (g *GRPCTransport) Start(_ context.Context) error { return nil }

func (g *GRPCTransport) Stop(_ context.Context) error { return nil }

// Supports returns true when the target looks like a gRPC address (host:port without http scheme).
func (g *GRPCTransport) Supports(_ context.Context, _ *coretypes.PayLoad, target string) bool {
	return target != "" && !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://")
}

// Send dials the target, invokes the Call RPC, and returns the raw response bytes.
func (g *GRPCTransport) Send(ctx context.Context, payload *coretypes.PayLoad, target string) ([]byte, error) {
	cc, err := g.dial(target)
	if err != nil {
		return nil, err
	}
	defer cc.Close()
	client := pb.NewCoreTransportClient(cc)
	resp, err := client.Call(ctx, payloadToPB(payload))
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("grpc: remote error: %s", resp.Error)
	}
	return resp.Data, nil
}

// Health checks the target gRPC server by calling the Health RPC.
func (g *GRPCTransport) Health(ctx context.Context, target string) error {
	cc, err := g.dial(target)
	if err != nil {
		return err
	}
	defer cc.Close()
	client := pb.NewCoreTransportClient(cc)
	resp, err := client.Health(ctx, &pb.HealthRequest{})
	if err != nil {
		return err
	}
	if !resp.Healthy {
		return fmt.Errorf("grpc: target %s reports unhealthy: %s", target, resp.Message)
	}
	return nil
}

func (g *GRPCTransport) dial(target string) (*grpc.ClientConn, error) {
	return grpc.NewClient(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(g.maxRecvMsgSize),
			grpc.MaxCallSendMsgSize(g.maxSendMsgSize),
		),
	)
}

func payloadToPB(p *coretypes.PayLoad) *pb.PayloadRequest {
	req := &pb.PayloadRequest{
		TraceId:          p.TraceID,
		SourceAddress:    p.SourceAddress,
		SourcePort:       int32(p.SourcePort),
		SourceSocketPort: int32(p.SourceSocketPort),
		SourceService:    p.SourceService,
		TargetAddress:    p.TargetAddress,
		TargetPort:       int32(p.TargetPort),
		TargetSocketPort: int32(p.TargetSocketPort),
		TargetService:    p.TargetService,
		SourcePath:       p.SourcePath,
		TargetPath:       p.TargetPath,
		UserId:           p.UserId,
		UserName:         p.UserName,
		ClientIp:         p.ClientIP,
		Auth:             p.Auth,
		Data:             p.Data,
		HttpMethod:       p.HttpMethod,
		Token:            p.Token,
	}
	return req
}
