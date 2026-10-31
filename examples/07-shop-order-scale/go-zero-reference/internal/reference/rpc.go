// 本文件直接用原生 zrpc 的业务类型接口提供 mTLS RPC，不复刻 Core 通用调用协议。
package reference

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"time"

	pb "github.com/digitalwayhk/core/examples/07-shop-order-scale/go-zero-reference/internal/wire"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const productMethod = "/reference07.SupplierService/GetProducts"
const createMethod = "/reference07.OrderService/CreateOrder"
const ordersMethod = "/reference07.OrderService/GetOrders"

type supplierRPC interface {
	GetProducts(context.Context, *pb.ProductQuery) (*pb.Products, error)
}
type orderRPC interface {
	CreateOrder(context.Context, *pb.CreateOrderRequest) (*pb.OrderSnapshot, error)
	GetOrders(context.Context, *pb.OrderQuery) (*pb.Orders, error)
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
	for _, method := range []string{productMethod, createMethod, ordersMethod} {
		zrpc.DontLogContentForMethod(method)
	}
	s, e := zrpc.NewServer(c, func(s *grpc.Server) {
		a.serverMu.Lock()
		a.grpcServer = s
		a.serverMu.Unlock()
		if a.c.Role == "supplier" {
			s.RegisterService(&grpc.ServiceDesc{ServiceName: "reference07.SupplierService", HandlerType: (*supplierRPC)(nil), Methods: []grpc.MethodDesc{{MethodName: "GetProducts", Handler: productsHandler}}}, a)
		} else {
			s.RegisterService(&grpc.ServiceDesc{ServiceName: "reference07.OrderService", HandlerType: (*orderRPC)(nil), Methods: []grpc.MethodDesc{{MethodName: "CreateOrder", Handler: createHandler}, {MethodName: "GetOrders", Handler: ordersHandler}}}, a)
		}
	})
	if e != nil {
		return e
	}
	s.AddOptions(grpc.Creds(credentials.NewTLS(t)), grpc.MaxRecvMsgSize(4<<20), grpc.MaxSendMsgSize(4<<20))
	go s.Start()
	return nil
}

func productsHandler(srv interface{}, ctx context.Context, decode func(interface{}) error, i grpc.UnaryServerInterceptor) (interface{}, error) {
	in := &pb.ProductQuery{}
	if e := decode(in); e != nil {
		return nil, e
	}
	h := func(ctx context.Context, r interface{}) (interface{}, error) {
		return srv.(supplierRPC).GetProducts(ctx, r.(*pb.ProductQuery))
	}
	if i == nil {
		return h(ctx, in)
	}
	return i(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: productMethod}, h)
}
func createHandler(srv interface{}, ctx context.Context, decode func(interface{}) error, i grpc.UnaryServerInterceptor) (interface{}, error) {
	in := &pb.CreateOrderRequest{}
	if e := decode(in); e != nil {
		return nil, e
	}
	h := func(ctx context.Context, r interface{}) (interface{}, error) {
		return srv.(orderRPC).CreateOrder(ctx, r.(*pb.CreateOrderRequest))
	}
	if i == nil {
		return h(ctx, in)
	}
	return i(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: createMethod}, h)
}
func ordersHandler(srv interface{}, ctx context.Context, decode func(interface{}) error, i grpc.UnaryServerInterceptor) (interface{}, error) {
	in := &pb.OrderQuery{}
	if e := decode(in); e != nil {
		return nil, e
	}
	h := func(ctx context.Context, r interface{}) (interface{}, error) {
		return srv.(orderRPC).GetOrders(ctx, r.(*pb.OrderQuery))
	}
	if i == nil {
		return h(ctx, in)
	}
	return i(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: ordersMethod}, h)
}

func trustedUserService(ctx context.Context) error {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "缺少 mTLS 身份")
	}
	t, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(t.State.VerifiedChains) == 0 || len(t.State.PeerCertificates) == 0 {
		return status.Error(codes.Unauthenticated, "内部证书未验证")
	}
	if e := t.State.PeerCertificates[0].VerifyHostname("shop-user"); e != nil {
		return status.Error(codes.PermissionDenied, "内部调用者无权限")
	}
	return nil
}

// GetProducts 直接执行供应商业务，不解释路径或通用 JSON 载荷。
func (a *application) GetProducts(ctx context.Context, r *pb.ProductQuery) (*pb.Products, error) {
	if e := trustedUserService(ctx); e != nil {
		return nil, e
	}
	items, e := a.products(uint(r.Id))
	if e != nil {
		return nil, rpcFailure("products", e)
	}
	out := &pb.Products{}
	for _, p := range items {
		out.Items = append(out.Items, &pb.ProductSnapshot{Id: uint64(p["id"].(uint)), SupplierId: uint64(p["supplierID"].(uint)), SupplierCode: p["supplierCode"].(string), SupplierName: p["supplierName"].(string), Code: p["code"].(string), Name: p["name"].(string), Price: p["price"].(decimal.Decimal).String()})
	}
	return out, nil
}

// CreateOrder 直接接收业务请求，用户归属由已验证的 user 服务背书。
func (a *application) CreateOrder(ctx context.Context, r *pb.CreateOrderRequest) (*pb.OrderSnapshot, error) {
	if e := trustedUserService(ctx); e != nil {
		return nil, e
	}
	price, e := decimal.NewFromString(r.UnitPrice)
	if e != nil {
		return nil, status.Error(codes.InvalidArgument, "商品价格无效")
	}
	if r.UserId == 0 {
		return nil, status.Error(codes.PermissionDenied, "订单用户身份缺失")
	}
	q := &OrderRequest{OrderID: uint(r.OrderId), UserID: uint(r.UserId), SupplierID: uint(r.SupplierId), ProductID: uint(r.ProductId), Quantity: int(r.Quantity), RequestID: r.RequestId, SupplierCode: r.SupplierCode, SupplierName: r.SupplierName, ProductCode: r.ProductCode, ProductName: r.ProductName, UnitPrice: price}
	if r.Address != nil {
		q.Address = Address{AddressID: uint(r.Address.AddressId), ReceiverName: r.Address.ReceiverName, Phone: r.Address.Phone, Province: r.Address.Province, City: r.Address.City, District: r.Address.District, Detail: r.Address.Detail}
	}
	md, _ := metadata.FromIncomingContext(ctx)
	trace := ""
	if values := md.Get("x-trace-id"); len(values) > 0 {
		trace = values[0]
	}
	o, e := a.create(q, trace)
	if e != nil {
		return nil, rpcFailure("create", e)
	}
	return orderToRPC(o), nil
}

// GetOrders 只查询已认证 user 服务指定买家的订单。
func (a *application) GetOrders(ctx context.Context, r *pb.OrderQuery) (*pb.Orders, error) {
	if e := trustedUserService(ctx); e != nil {
		return nil, e
	}
	if r.UserId == 0 {
		return nil, status.Error(codes.PermissionDenied, "订单用户身份缺失")
	}
	items, e := a.orders(uint(r.UserId))
	if e != nil {
		return nil, rpcFailure("orders", e)
	}
	out := &pb.Orders{}
	for _, o := range items {
		out.Items = append(out.Items, orderToRPC(o))
	}
	return out, nil
}
func rpcFailure(phase string, e error) error {
	recordError("rpc_"+phase, e)
	return status.Error(codes.Internal, "internal server error")
}
func orderToRPC(o *Order) *pb.OrderSnapshot {
	return &pb.OrderSnapshot{OrderId: uint64(o.ID), UserId: uint64(o.UserID), SupplierId: uint64(o.SupplierID), ProductId: uint64(o.ProductID), Quantity: int64(o.Quantity), UnitPrice: o.UnitPrice.String(), TotalAmount: o.TotalAmount.String(), OrderStatus: o.OrderStatus, PaymentStatus: o.PaymentStatus, TraceId: o.TraceID, OrderRevision: o.OrderRevision}
}
func orderFromRPC(o *pb.OrderSnapshot) (*Order, error) {
	price, e := decimal.NewFromString(o.UnitPrice)
	if e != nil {
		return nil, e
	}
	total, e := decimal.NewFromString(o.TotalAmount)
	if e != nil {
		return nil, e
	}
	return &Order{Record: Record{TraceID: o.TraceId}, ID: uint(o.OrderId), UserID: uint(o.UserId), SupplierID: uint(o.SupplierId), ProductID: uint(o.ProductId), Quantity: int(o.Quantity), UnitPrice: price, TotalAmount: total, OrderStatus: o.OrderStatus, PaymentStatus: o.PaymentStatus, OrderRevision: o.OrderRevision}, nil
}

func (a *application) productRPC(ctx context.Context, id uint) ([]*pb.ProductSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out := &pb.Products{}
	e := a.clients["shop-supplier"].Conn().Invoke(ctx, productMethod, &pb.ProductQuery{Id: uint64(id)}, out)
	return out.Items, e
}
func (a *application) createRPC(ctx context.Context, q *OrderRequest, trace string) (*Order, error) {
	r := &pb.CreateOrderRequest{OrderId: uint64(q.OrderID), UserId: uint64(q.UserID), SupplierId: uint64(q.SupplierID), ProductId: uint64(q.ProductID), Quantity: int64(q.Quantity), RequestId: q.RequestID, SupplierCode: q.SupplierCode, SupplierName: q.SupplierName, ProductCode: q.ProductCode, ProductName: q.ProductName, UnitPrice: q.UnitPrice.String(), Address: &pb.Address{AddressId: uint64(q.Address.AddressID), ReceiverName: q.Address.ReceiverName, Phone: q.Address.Phone, Province: q.Address.Province, City: q.Address.City, District: q.Address.District, Detail: q.Address.Detail}}
	ctx = metadata.AppendToOutgoingContext(ctx, "x-trace-id", trace)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out := &pb.OrderSnapshot{}
	if e := a.clients["shop-order"].Conn().Invoke(ctx, createMethod, r, out); e != nil {
		return nil, e
	}
	return orderFromRPC(out)
}
func (a *application) ordersRPC(ctx context.Context, user uint) ([]*Order, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out := &pb.Orders{}
	if e := a.clients["shop-order"].Conn().Invoke(ctx, ordersMethod, &pb.OrderQuery{UserId: uint64(user)}, out); e != nil {
		return nil, e
	}
	result := make([]*Order, 0, len(out.Items))
	for _, o := range out.Items {
		v, e := orderFromRPC(o)
		if e != nil {
			return nil, e
		}
		result = append(result, v)
	}
	return result, nil
}
