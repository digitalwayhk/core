// 本文件直接用 zrpc 与 grpc-go 提供 mTLS 内部 RPC，不使用 Core Transport。
package reference

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/go-zero-reference/internal/wire"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const rpcMethod = "/reference07transport.OrderTransport/Call"

type rpcHandler interface {
	Call(context.Context, *wire.PayloadRequest) (*wire.PayloadResponse, error)
}

func tlsConfig(serverName string) (*tls.Config, error) {
	ca, e := os.ReadFile(os.Getenv("SHOP_GRPC_CA_FILE"))
	if e != nil {
		return nil, e
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, errors.New("CA 证书不可用")
	}
	cert, e := tls.LoadX509KeyPair(os.Getenv("SHOP_GRPC_CERT_FILE"), os.Getenv("SHOP_GRPC_KEY_FILE"))
	if e != nil {
		return nil, e
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool, ClientCAs: pool, Certificates: []tls.Certificate{cert}, ClientAuth: tls.RequireAndVerifyClientCert, ServerName: serverName}, nil
}

func newRPCClient(target, address string) (zrpc.Client, error) {
	t, e := tlsConfig(target)
	if e != nil {
		return nil, e
	}
	c := zrpc.RpcClientConf{Endpoints: []string{address}, NonBlock: true, Timeout: 2000, Middlewares: zrpc.ClientMiddlewaresConf{Trace: true, Duration: true, Prometheus: true, Breaker: true, Timeout: true}}
	return zrpc.NewClient(c, zrpc.WithDialOption(grpc.WithTransportCredentials(credentials.NewTLS(t))))
}

func (a *application) startRPC() error {
	t, e := tlsConfig("")
	if e != nil {
		return e
	}
	var c zrpc.RpcServerConf
	if e = conf.FillDefault(&c); e != nil {
		return e
	}
	c.ServiceConf = a.file.ServiceConf
	c.Prometheus.Port = 0
	c.ListenOn = fmt.Sprintf("127.0.0.1:%d", a.c.GRPCPort)
	c.Timeout = 0
	// 保留原生 zrpc 的入站中间件；与 Core 入站 grpc-go 的差异写入对照说明。
	zrpc.DontLogContentForMethod(rpcMethod)
	s, e := zrpc.NewServer(c, func(s *grpc.Server) {
		a.serverMu.Lock()
		a.grpcServer = s
		a.serverMu.Unlock()
		s.RegisterService(&grpc.ServiceDesc{ServiceName: "reference07transport.OrderTransport", HandlerType: (*rpcHandler)(nil), Methods: []grpc.MethodDesc{{MethodName: "Call", Handler: func(srv interface{}, ctx context.Context, decode func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
			in := &wire.PayloadRequest{}
			if e := decode(in); e != nil {
				return nil, e
			}
			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				return srv.(rpcHandler).Call(ctx, req.(*wire.PayloadRequest))
			}
			if interceptor == nil {
				return handler(ctx, in)
			}
			return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: rpcMethod}, handler)
		}}}}, a)
	})
	if e != nil {
		return e
	}
	s.AddOptions(grpc.Creds(credentials.NewTLS(t)), grpc.MaxRecvMsgSize(4<<20), grpc.MaxSendMsgSize(4<<20))
	go s.Start()
	return nil
}

// Call 按证书校验内部调用者后执行原生业务；自报 source_service 不能建立身份。
func (a *application) Call(ctx context.Context, r *wire.PayloadRequest) (*wire.PayloadResponse, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "缺少 mTLS 身份")
	}
	t, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(t.State.VerifiedChains) == 0 || len(t.State.PeerCertificates) == 0 {
		return nil, status.Error(codes.Unauthenticated, "内部证书未验证")
	}
	if e := t.State.PeerCertificates[0].VerifyHostname("shop-user"); e != nil || r.SourceService != "shop-user" {
		return nil, status.Error(codes.PermissionDenied, "内部调用者无权限")
	}
	var data interface{}
	var e error
	switch {
	case a.c.Role == "supplier" && r.TargetPath == "/api/shop-supplier/getproducts":
		var q struct {
			ID uint `json:"id"`
		}
		if e = json.Unmarshal(r.Data, &q); e == nil {
			data, e = a.products(q.ID)
		}
	case a.c.Role == "order" && r.TargetPath == "/api/shop-order/createorder":
		var q OrderRequest
		if e = json.Unmarshal(r.Data, &q); e == nil {
			if !r.Auth || r.UserId != fmt.Sprint(q.UserID) {
				return nil, status.Error(codes.PermissionDenied, "订单用户身份不一致")
			}
			data, e = a.create(&q, r.TraceId)
		}
	case a.c.Role == "order" && r.TargetPath == "/api/shop-order/getorders":
		var q struct {
			UserID uint `json:"userID"`
		}
		if e = json.Unmarshal(r.Data, &q); e == nil {
			if !r.Auth || r.UserId != fmt.Sprint(q.UserID) {
				return nil, status.Error(codes.PermissionDenied, "订单用户身份不一致")
			}
			data, e = a.orders(q.UserID)
		}
	default:
		return nil, status.Error(codes.Unimplemented, "对照未实现该路由")
	}
	if e != nil {
		recordError("rpc_business", e)
		return nil, status.Error(codes.Internal, "internal server error")
	}
	b, e := json.Marshal(map[string]interface{}{"success": true, "errorCode": 200, "data": data})
	if e != nil {
		return nil, e
	}
	return &wire.PayloadResponse{Data: b}, nil
}

func (a *application) call(ctx context.Context, target, path string, user uint, trace string, in, out interface{}) error {
	b, e := json.Marshal(in)
	if e != nil {
		return e
	}
	r := &wire.PayloadRequest{SourceService: "shop-user", TargetService: target, SourcePath: "/api/shop-user/addorder", TargetPath: path, UserId: fmt.Sprint(user), TraceId: trace, Auth: true, HttpMethod: "POST", Data: b}
	var response wire.PayloadResponse
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if e = a.clients[target].Conn().Invoke(ctx, rpcMethod, r, &response); e != nil {
		return e
	}
	var envelope struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if e = json.Unmarshal(response.Data, &envelope); e != nil {
		return e
	}
	if !envelope.Success {
		return errors.New("内部调用失败")
	}
	return json.Unmarshal(envelope.Data, out)
}
