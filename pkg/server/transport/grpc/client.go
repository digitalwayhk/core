package grpc

import (
	"context"
	"encoding/json"
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

	newClient zrpcClientFactory
}

type clientPoolKey struct {
	endpoint   string
	serverName string
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
	serverName, err := g.serverName(payload)
	if err != nil {
		return nil, err
	}
	client, err := g.getClient(target, serverName)
	if err != nil {
		return nil, err
	}
	request, err := payloadToPB(payload)
	if err != nil {
		return nil, err
	}
	resp, err := pb.NewCoreTransportClient(client.Conn()).Call(ctx, request)
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
	serverName, err := g.serverName(nil)
	if err != nil {
		return err
	}
	return g.health(ctx, target, serverName)
}

// HealthPayload verifies the target using the service identity carried by the
// payload when Security.ServerName is configured as {service}.
func (g *GRPCTransport) HealthPayload(ctx context.Context, payload *coretypes.PayLoad, target string) error {
	serverName, err := g.serverName(payload)
	if err != nil {
		return err
	}
	return g.health(ctx, target, serverName)
}

func (g *GRPCTransport) health(ctx context.Context, target, serverName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	client, err := g.getClient(target, serverName)
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

func (g *GRPCTransport) getClient(endpoint string, serverNames ...string) (zrpc.Client, error) {
	if strings.TrimSpace(endpoint) == "" {
		return nil, errEmptyEndpoint
	}
	serverName := ""
	if len(serverNames) > 0 {
		serverName = serverNames[0]
	}
	g.lifecycleMu.RLock()
	defer g.lifecycleMu.RUnlock()
	if g.stopped {
		return nil, errTransportStopped
	}
	key := clientPoolKey{endpoint: endpoint, serverName: serverName}
	if cached, ok := g.pool.Load(key); ok {
		return cached.(zrpc.Client), nil
	}
	result := <-g.initializeClient(endpoint, serverName)
	if result.Err != nil {
		return nil, result.Err
	}
	return result.Val.(zrpc.Client), nil
}

func (g *GRPCTransport) initializeClient(endpoint string, serverNames ...string) <-chan singleflight.Result {
	serverName := ""
	if len(serverNames) > 0 {
		serverName = serverNames[0]
	}
	key := clientPoolKey{endpoint: endpoint, serverName: serverName}
	flightKey := key.endpoint + "\x00" + key.serverName
	return g.init.DoChan(flightKey, func() (any, error) {
		if cached, ok := g.pool.Load(key); ok {
			return cached.(zrpc.Client), nil
		}
		options, err := g.clientOptions(key.serverName)
		if err != nil {
			return nil, err
		}
		rpcConf := zrpc.RpcClientConf{
			Endpoints: []string{key.endpoint},
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
		actual, loaded := g.pool.LoadOrStore(key, client)
		if loaded {
			_ = client.Conn().Close()
			return actual.(zrpc.Client), nil
		}
		return client, nil
	})
}

func (g *GRPCTransport) clientOptions(serverName string) ([]zrpc.ClientOption, error) {
	security := g.config.Security
	security.ServerName = serverName
	options, err := clientSecurityOptions(security)
	if err != nil {
		return nil, err
	}
	return append(options, zrpc.WithDialOption(googlegrpc.WithDefaultCallOptions(
		googlegrpc.MaxCallRecvMsgSize(g.config.MaxRecvMsgSize),
		googlegrpc.MaxCallSendMsgSize(g.config.MaxSendMsgSize),
	))), nil
}

func (g *GRPCTransport) serverName(payload *coretypes.PayLoad) (string, error) {
	serverName := strings.TrimSpace(g.config.Security.ServerName)
	if serverName != config.GRPCServerNameTargetService {
		return serverName, nil
	}
	if payload == nil || strings.TrimSpace(payload.TargetService) == "" {
		return "", errors.New("grpc: target service is required for service identity verification")
	}
	return strings.TrimSpace(payload.TargetService), nil
}

func payloadToPB(p *coretypes.PayLoad) (*pb.PayloadRequest, error) {
	data := p.Data
	if len(data) == 0 && p.Instance != nil {
		var err error
		data, err = json.Marshal(p.Instance)
		if err != nil {
			return nil, fmt.Errorf("grpc: encode payload instance: %w", err)
		}
	}
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
		Data:          data,
		HttpMethod:    p.HttpMethod,
		Token:         p.Token,
	}, nil
}
