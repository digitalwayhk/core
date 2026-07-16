package transport

import "sync/atomic"

// Stats 记录单个 ServiceContext 的低基数传输指标。
// 字段集合固定，避免以服务、路由或端点作为动态 map key 造成无界增长。
type Stats struct {
	grpcSelected atomic.Uint64
	httpSelected atomic.Uint64
	sendSuccess  atomic.Uint64
	sendFailure  atomic.Uint64
	httpFallback atomic.Uint64
	inboundGRPC  atomic.Uint64
}

// StatsSnapshot 是传输指标的一致用途快照。
type StatsSnapshot struct {
	GRPCSelected uint64
	HTTPSelected uint64
	SendSuccess  uint64
	SendFailure  uint64
	HTTPFallback uint64
	InboundGRPC  uint64
}

// Snapshot 返回当前累计值。
func (s *Stats) Snapshot() StatsSnapshot {
	if s == nil {
		return StatsSnapshot{}
	}
	return StatsSnapshot{
		GRPCSelected: s.grpcSelected.Load(),
		HTTPSelected: s.httpSelected.Load(),
		SendSuccess:  s.sendSuccess.Load(),
		SendFailure:  s.sendFailure.Load(),
		HTTPFallback: s.httpFallback.Load(),
		InboundGRPC:  s.inboundGRPC.Load(),
	}
}

// RecordInboundGRPC 记录当前 ServiceContext 收到的一次 gRPC 调用。
func (s *Stats) RecordInboundGRPC() {
	if s != nil {
		s.inboundGRPC.Add(1)
	}
}

func (s *Stats) recordSelection(protocol string, fallback bool) {
	if s == nil {
		return
	}
	switch protocol {
	case "grpc":
		s.grpcSelected.Add(1)
	case "http":
		s.httpSelected.Add(1)
		if fallback {
			s.httpFallback.Add(1)
		}
	}
}

func (s *Stats) recordSend(err error) {
	if s == nil {
		return
	}
	if err != nil {
		s.sendFailure.Add(1)
		return
	}
	s.sendSuccess.Add(1)
}
