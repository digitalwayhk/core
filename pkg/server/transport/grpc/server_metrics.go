package grpc

import (
	"context"
	"strconv"
	"time"

	"github.com/zeromicro/go-zero/core/metric"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// 使用 core_ 前缀，避免与 go-zero zrpc/internal/serverinterceptors 的
// rpc_server_* 全局注册冲突（import zrpc 会加载 server 包文件）。
var (
	rpcServerReqDur = metric.NewHistogramVec(&metric.HistogramVecOpts{
		Namespace: "core",
		Subsystem: "grpc_server",
		Name:      "requests_duration_ms",
		Help:      "core grpc server requests duration(ms).",
		Labels:    []string{"method"},
		Buckets:   []float64{1, 2, 5, 10, 25, 50, 100, 250, 500, 1000, 2000, 5000},
	})
	rpcServerReqCodeTotal = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "core",
		Subsystem: "grpc_server",
		Name:      "requests_code_total",
		Help:      "core grpc server requests code count.",
		Labels:    []string{"method", "code"},
	})
)

func unaryServerMetricsInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	method := "unknown"
	if info != nil && info.FullMethod != "" {
		method = info.FullMethod
	}
	code := status.Code(err)
	ms := time.Since(start).Milliseconds()
	if ms < 0 {
		ms = 0
	}
	if rpcServerReqDur != nil {
		rpcServerReqDur.Observe(ms, method)
	}
	if rpcServerReqCodeTotal != nil {
		rpcServerReqCodeTotal.Inc(method, strconv.Itoa(int(code)))
	}
	return resp, err
}
