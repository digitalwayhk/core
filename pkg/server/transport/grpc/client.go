package grpc

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	coretypes "github.com/digitalwayhk/core/pkg/server/types"
	pb "github.com/digitalwayhk/core/pkg/server/transport/grpc/proto"
)

// GRPCTransport implements transport.Transport using gRPC.
type GRPCTransport struct {
	maxRecvMsgSize int
	maxSendMsgSize int
	pool           sync.Map // map[string]*grpc.ClientConn
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

func (g *GRPCTransport) Stop(_ context.Context) error {
	g.pool.Range(func(key, value interface{}) bool {
		if cc, ok := value.(*grpc.ClientConn); ok {
			cc.Close()
		}
		g.pool.Delete(key)
		return true
	})
	return nil
}

// PooledConns returns the number of cached gRPC connections in the pool.
func (g *GRPCTransport) PooledConns() int {
	count := 0
	g.pool.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// Supports returns true when the target looks like a gRPC address (host:port without http scheme).
func (g *GRPCTransport) Supports(_ context.Context, _ *coretypes.PayLoad, target string) bool {
	return target != "" && !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://")
}

// Send dials the target, invokes the Call RPC, and returns the raw response bytes.
// Connections are pooled and reused across calls to the same target.
func (g *GRPCTransport) Send(ctx context.Context, payload *coretypes.PayLoad, target string) ([]byte, error) {
	cc, err := g.getConn(target)
	if err != nil {
		return nil, err
	}
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
	cc, err := g.getConn(target)
	if err != nil {
		return err
	}
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

// getConn returns a pooled grpc.ClientConn for target, creating one if needed.
func (g *GRPCTransport) getConn(target string) (*grpc.ClientConn, error) {
	if cached, ok := g.pool.Load(target); ok {
		return cached.(*grpc.ClientConn), nil
	}
	cc, err := g.dial(target)
	if err != nil {
		return nil, err
	}
	// Store only if not already cached by a concurrent caller; discard our copy
	// if the race was lost to avoid leaking connections.
	actual, loaded := g.pool.LoadOrStore(target, cc)
	if loaded {
		cc.Close()
		return actual.(*grpc.ClientConn), nil
	}
	return cc, nil
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
