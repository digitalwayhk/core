package grpc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/zrpc"
	"golang.org/x/sync/singleflight"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/digitalwayhk/core/pkg/server/config"
	pb "github.com/digitalwayhk/core/pkg/server/transport/grpc/proto"
	coretypes "github.com/digitalwayhk/core/pkg/server/types"
)

const (
	defaultMessageSize = 4 * 1024 * 1024
	defaultRPCTimeout  = 2 * time.Second
)

var (
	errTransportStopped = errors.New("grpc transport: stopped")
	errEmptyEndpoint    = errors.New("grpc transport: empty endpoint")
)

type zrpcClientFactory func(zrpc.RpcClientConf, ...zrpc.ClientOption) (zrpc.Client, error)

// GRPCTransport implements transport.Transport using go-zero zrpc clients.
type GRPCTransport struct {
	config config.GRPCTransportConfig
	pool   sync.Map // endpoint -> zrpc.Client
	init   singleflight.Group

	lifecycleMu sync.RWMutex
	stopped     bool

	securityOnce sync.Once
	securityOpts []zrpc.ClientOption
	securityErr  error
	newClient    zrpcClientFactory
}

// New returns a gRPC transport configured with the framework transport contract.
func New(cfg config.GRPCTransportConfig) *GRPCTransport {
	if cfg.MaxRecvMsgSize <= 0 {
		cfg.MaxRecvMsgSize = defaultMessageSize
	}
	if cfg.MaxSendMsgSize <= 0 {
		cfg.MaxSendMsgSize = defaultMessageSize
	}
	return &GRPCTransport{config: cfg, newClient: zrpc.NewClient}
}

func (g *GRPCTransport) Name() string { return "grpc" }

func (g *GRPCTransport) Start(_ context.Context) error {
	g.lifecycleMu.RLock()
	defer g.lifecycleMu.RUnlock()
	if g.stopped {
		return errTransportStopped
	}
	return nil
}

// Stop closes every cached zrpc connection and permanently stops this transport.
func (g *GRPCTransport) Stop(_ context.Context) error {
	g.lifecycleMu.Lock()
	defer g.lifecycleMu.Unlock()
	if g.stopped {
		return nil
	}
	g.stopped = true
	var firstErr error
	g.pool.Range(func(key, value any) bool {
		g.pool.Delete(key)
		if client, ok := value.(zrpc.Client); ok && client.Conn() != nil {
			if err := client.Conn().Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return true
	})
	return firstErr
}

// PooledConns returns the number of cached zrpc clients.
func (g *GRPCTransport) PooledConns() int {
	count := 0
	g.pool.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

func (g *GRPCTransport) Supports(_ context.Context, _ *coretypes.PayLoad, target string) bool {
	return target != "" && !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://")
}

func (g *GRPCTransport) Send(ctx context.Context, payload *coretypes.PayLoad, target string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client, err := g.getClient(target)
	if err != nil {
		return nil, err
	}
	resp, err := pb.NewCoreTransportClient(client.Conn()).Call(ctx, payloadToPB(payload))
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, status.Error(codes.Internal, "internal server error")
	}
	return resp.Data, nil
}

// Health uses the standard gRPC health protocol and only accepts SERVING.
func (g *GRPCTransport) Health(ctx context.Context, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	client, err := g.getClient(target)
	if err != nil {
		return err
	}
	resp, err := grpc_health_v1.NewHealthClient(client.Conn()).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		return err
	}
	if resp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		return fmt.Errorf("grpc: target %s health status is %s", target, resp.Status)
	}
	return nil
}

func (g *GRPCTransport) getClient(endpoint string) (zrpc.Client, error) {
	if strings.TrimSpace(endpoint) == "" {
		return nil, errEmptyEndpoint
	}
	g.lifecycleMu.RLock()
	defer g.lifecycleMu.RUnlock()
	if g.stopped {
		return nil, errTransportStopped
	}
	if cached, ok := g.pool.Load(endpoint); ok {
		return cached.(zrpc.Client), nil
	}
	result := <-g.initializeClient(endpoint)
	if result.Err != nil {
		return nil, result.Err
	}
	return result.Val.(zrpc.Client), nil
}

func (g *GRPCTransport) initializeClient(endpoint string) <-chan singleflight.Result {
	return g.init.DoChan(endpoint, func() (any, error) {
		if cached, ok := g.pool.Load(endpoint); ok {
			return cached.(zrpc.Client), nil
		}
		options, err := g.clientOptions()
		if err != nil {
			return nil, err
		}
		rpcConf := zrpc.RpcClientConf{
			Endpoints: []string{endpoint},
			NonBlock:  true,
			Timeout:   defaultRPCTimeout.Milliseconds(),
			Middlewares: zrpc.ClientMiddlewaresConf{
				Trace: true, Duration: true, Prometheus: true, Breaker: true, Timeout: true,
			},
		}
		client, err := g.newClient(rpcConf, options...)
		if err != nil {
			return nil, err
		}
		actual, loaded := g.pool.LoadOrStore(endpoint, client)
		if loaded {
			_ = client.Conn().Close()
			return actual.(zrpc.Client), nil
		}
		return client, nil
	})
}

func (g *GRPCTransport) clientOptions() ([]zrpc.ClientOption, error) {
	g.securityOnce.Do(func() {
		g.securityOpts, g.securityErr = clientSecurityOptions(g.config.Security)
		g.securityOpts = append(g.securityOpts, zrpc.WithDialOption(googlegrpc.WithDefaultCallOptions(
			googlegrpc.MaxCallRecvMsgSize(g.config.MaxRecvMsgSize),
			googlegrpc.MaxCallSendMsgSize(g.config.MaxSendMsgSize),
		)))
	})
	return g.securityOpts, g.securityErr
}

func payloadToPB(p *coretypes.PayLoad) *pb.PayloadRequest {
	return &pb.PayloadRequest{
		TraceId:       p.TraceID,
		SourceService: p.SourceService,
		TargetService: p.TargetService,
		SourcePath:    p.SourcePath,
		TargetPath:    p.TargetPath,
		UserId:        p.UserId,
		UserName:      p.UserName,
		ClientIp:      p.ClientIP,
		Auth:          p.Auth,
		Data:          p.Data,
		HttpMethod:    p.HttpMethod,
		Token:         p.Token,
	}
}
