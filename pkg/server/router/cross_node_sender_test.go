package router

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/transport"
	httptransport "github.com/digitalwayhk/core/pkg/server/transport/http"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type serviceContextRecordingTransport struct {
	name      string
	healthErr error
	sendErr   error
	sendCalls atomic.Int32
}

func (t *serviceContextRecordingTransport) Name() string              { return t.name }
func (*serviceContextRecordingTransport) Start(context.Context) error { return nil }
func (*serviceContextRecordingTransport) Stop(context.Context) error  { return nil }
func (*serviceContextRecordingTransport) Supports(context.Context, *types.PayLoad, string) bool {
	return true
}
func (t *serviceContextRecordingTransport) Health(context.Context, string) error {
	return t.healthErr
}
func (t *serviceContextRecordingTransport) Send(context.Context, *types.PayLoad, string) ([]byte, error) {
	t.sendCalls.Add(1)
	return nil, t.sendErr
}

func TestMakeCrossNodeSenderPreservesNoticeJSONForHTTPFallback(t *testing.T) {
	type capturedRequest struct {
		body []byte
		err  error
	}
	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPost {
			body, err := io.ReadAll(req.Body)
			requests <- capturedRequest{body: body, err: err}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, portText, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)
	grpcTransport := &serviceContextRecordingTransport{name: "grpc", healthErr: errors.New("grpc unavailable")}
	selector := transport.NewDefaultSelector(grpcTransport, httptransport.New())
	stats := &transport.Stats{}
	selector.SetStats(stats)
	serviceContext := &ServiceContext{
		Service: &types.Service{Name: "users"},
		Config: &config.ServerConfig{Transport: config.TransportConfig{
			MaxRetries: 1,
		}},
		TransportSelector: selector,
		TransportStats:    stats,
	}
	notice := json.RawMessage(`{"route_path":"/ws/orders","hash":88,"message":{"id":"order-1"}}`)

	_, err = serviceContext.makeCrossNodeSender()(context.Background(), &cluster.NodeInfo{
		ID: "peer", Address: host, Port: port,
	}, notice, "/api/servermanage/ws/notice")
	require.NoError(t, err)

	request := <-requests
	require.NoError(t, request.err)
	assert.JSONEq(t, string(notice), string(request.body))
	assert.Zero(t, grpcTransport.sendCalls.Load())
	assert.Equal(t, uint64(1), stats.Snapshot().HTTPFallback)
}

func TestServiceContextSendPayloadDoesNotReplayAfterSendStarts(t *testing.T) {
	grpcTransport := &serviceContextRecordingTransport{name: "grpc", sendErr: context.DeadlineExceeded}
	httpTransport := &serviceContextRecordingTransport{name: "http"}
	selector := transport.NewDefaultSelector(grpcTransport, httpTransport)
	serviceContext := &ServiceContext{
		Service: &types.Service{Name: "users"},
		Config: &config.ServerConfig{Transport: config.TransportConfig{
			MaxRetries: 3,
		}},
		TransportSelector: selector,
	}

	_, err := serviceContext.sendPayload(context.Background(), &types.PayLoad{}, transport.TransportEndpoints{
		GRPC: "orders:19090", HTTP: "http://orders:8080",
	})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, int32(1), grpcTransport.sendCalls.Load())
	assert.Zero(t, httpTransport.sendCalls.Load())
}

func TestServiceContextTransportStatsAreIsolated(t *testing.T) {
	firstStats := &transport.Stats{}
	firstSelector := transport.NewDefaultSelector(&serviceContextRecordingTransport{name: "grpc"})
	firstSelector.SetStats(firstStats)
	first := &ServiceContext{
		Service:           &types.Service{Name: "users"},
		Config:            &config.ServerConfig{Transport: config.TransportConfig{MaxRetries: 1}},
		TransportSelector: firstSelector,
		TransportStats:    firstStats,
	}
	secondStats := &transport.Stats{}
	secondSelector := transport.NewDefaultSelector(&serviceContextRecordingTransport{name: "http"})
	secondSelector.SetStats(secondStats)
	second := &ServiceContext{
		Service:           &types.Service{Name: "suppliers"},
		Config:            &config.ServerConfig{Transport: config.TransportConfig{MaxRetries: 1}},
		TransportSelector: secondSelector,
		TransportStats:    secondStats,
	}

	_, err := first.sendPayload(context.Background(), &types.PayLoad{}, transport.TransportEndpoints{GRPC: "orders:19090"})
	require.NoError(t, err)

	assert.Equal(t, transport.StatsSnapshot{GRPCSelected: 1, SendSuccess: 1}, first.TransportStats.Snapshot())
	assert.Equal(t, transport.StatsSnapshot{}, second.TransportStats.Snapshot())
}
