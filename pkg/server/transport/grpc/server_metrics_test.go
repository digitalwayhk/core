package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/observability"
	pb "github.com/digitalwayhk/core/pkg/server/transport/grpc/proto"
	coretypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestGRPCServerRecordsCoreRouteAfterAuth(t *testing.T) {
	observability.EnableMetrics()

	handler := func(ctx context.Context, payload *coretypes.PayLoad) ([]byte, error) {
		return []byte(`{"ok":true}`), nil
	}
	server, err := NewServer("127.0.0.1:0", insecureServerConfig(), handler)
	require.NoError(t, err)
	go server.Start()
	t.Cleanup(server.Stop)
	select {
	case <-server.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("server not ready")
	}

	labels := map[string]string{
		"service":      "shop-order",
		"route":        "/api/shop-order/addorder",
		"protocol":     "grpc",
		"result_class": "success",
	}
	before := gatherCounter(t, "core_service_request_requests_total", labels)

	conn, err := grpc.Dial(server.Address(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	_, err = pb.NewCoreTransportClient(conn).Call(context.Background(), &pb.PayloadRequest{
		TargetService: "shop-order",
		TargetPath:    "/api/shop-order/addorder",
		SourceService: "shop-user",
	})
	require.NoError(t, err)

	after := gatherCounter(t, "core_service_request_requests_total", labels)
	require.Equal(t, before+1, after)
}

func TestGRPCServerInvalidRouteStillRecordsStableLabel(t *testing.T) {
	observability.EnableMetrics()

	handler := func(ctx context.Context, payload *coretypes.PayLoad) ([]byte, error) {
		return nil, context.DeadlineExceeded
	}
	server, err := NewServer("127.0.0.1:0", insecureServerConfig(), handler)
	require.NoError(t, err)
	go server.Start()
	t.Cleanup(server.Stop)
	select {
	case <-server.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("server not ready")
	}

	labels := map[string]string{
		"service":      "shop-order",
		"route":        "invalid_route",
		"protocol":     "grpc",
		"result_class": "timeout",
	}
	before := gatherCounter(t, "core_service_request_requests_total", labels)

	conn, err := grpc.Dial(server.Address(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	_, err = pb.NewCoreTransportClient(conn).Call(context.Background(), &pb.PayloadRequest{
		TargetService: "shop-order",
		TargetPath:    "/api/x?id=1",
		SourceService: "shop-user",
	})
	require.Error(t, err)

	after := gatherCounter(t, "core_service_request_requests_total", labels)
	require.Equal(t, before+1, after)
}

func gatherCounter(t *testing.T, name string, want map[string]string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			if matchLabels(m.GetLabel(), want) {
				if m.GetCounter() != nil {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

func matchLabels(got []*dto.LabelPair, want map[string]string) bool {
	values := make(map[string]string, len(got))
	for _, l := range got {
		values[l.GetName()] = l.GetValue()
	}
	for k, v := range want {
		if values[k] != v {
			return false
		}
	}
	return true
}
