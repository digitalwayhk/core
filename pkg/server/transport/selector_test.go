package transport_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/transport"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTransport is a test double for transport.Transport.
type mockTransport struct {
	name         string
	supports     bool
	healthErr    error
	sendResult   []byte
	sendErr      error
	sendCalls    atomic.Int32
	target       string
	healthTarget string
}

func (m *mockTransport) Name() string                  { return m.name }
func (m *mockTransport) Start(_ context.Context) error { return nil }
func (m *mockTransport) Stop(_ context.Context) error  { return nil }
func (m *mockTransport) Supports(_ context.Context, _ *types.PayLoad, _ string) bool {
	return m.supports
}
func (m *mockTransport) Health(_ context.Context, target string) error {
	m.healthTarget = target
	return m.healthErr
}

func (m *mockTransport) Send(_ context.Context, _ *types.PayLoad, target string) ([]byte, error) {
	m.sendCalls.Add(1)
	m.target = target
	return m.sendResult, m.sendErr
}

func TestDefaultSelector_SelectPrimary(t *testing.T) {
	primary := &mockTransport{name: "grpc", supports: true, healthErr: nil}
	sel := transport.NewDefaultSelector(primary)

	chosen, err := sel.Select(context.Background(), &types.PayLoad{}, transport.TransportEndpoints{GRPC: "host:19090"})
	require.NoError(t, err)
	assert.Equal(t, "grpc", chosen.Transport.Name())
	assert.Equal(t, "host:19090", chosen.Endpoint)
}

func TestDefaultSelector_FallbackWhenPrimaryUnhealthy(t *testing.T) {
	primary := &mockTransport{name: "grpc", supports: true, healthErr: errors.New("refused")}
	fallback := &mockTransport{name: "http", supports: true, healthErr: nil}
	sel := transport.NewDefaultSelector(primary, fallback)

	chosen, err := sel.Select(context.Background(), &types.PayLoad{}, transport.TransportEndpoints{
		GRPC: "host:19090", HTTP: "http://host:8080",
	})
	require.NoError(t, err)
	assert.Equal(t, "http", chosen.Transport.Name())
	assert.Equal(t, "http://host:8080", chosen.Endpoint)
	assert.Equal(t, "host:19090", primary.healthTarget)
	assert.Equal(t, "http://host:8080", fallback.healthTarget)
}

func TestDefaultSelector_FallbackWhenPrimaryNotSupported(t *testing.T) {
	primary := &mockTransport{name: "socket", supports: false}
	fallback := &mockTransport{name: "http", supports: true, healthErr: nil}
	sel := transport.NewDefaultSelector(primary, fallback)

	chosen, err := sel.Select(context.Background(), &types.PayLoad{}, transport.TransportEndpoints{HTTP: "http://target"})
	require.NoError(t, err)
	assert.Equal(t, "http", chosen.Transport.Name())
}

func TestDefaultSelector_ErrorWhenAllUnhealthy(t *testing.T) {
	primary := &mockTransport{name: "grpc", supports: true, healthErr: errors.New("refused")}
	fallback := &mockTransport{name: "http", supports: true, healthErr: errors.New("timeout")}
	sel := transport.NewDefaultSelector(primary, fallback)

	_, err := sel.Select(context.Background(), &types.PayLoad{}, transport.TransportEndpoints{
		GRPC: "target:19090", HTTP: "http://target:8080",
	})
	assert.ErrorIs(t, err, transport.ErrNoTransport)
}

func TestDefaultSelector_ErrorWhenNoneSupported(t *testing.T) {
	primary := &mockTransport{name: "grpc", supports: false}
	fallback := &mockTransport{name: "socket", supports: false}
	sel := transport.NewDefaultSelector(primary, fallback)

	_, err := sel.Select(context.Background(), &types.PayLoad{}, transport.TransportEndpoints{
		GRPC: "target:19090", HTTP: "http://target:8080",
	})
	assert.ErrorIs(t, err, transport.ErrNoTransport)
}

func TestSendReturnsSelectedTransportResult(t *testing.T) {
	primary := &mockTransport{
		name:       "grpc",
		supports:   true,
		healthErr:  nil,
		sendResult: []byte(`{"ok":true}`),
	}
	sel := transport.NewDefaultSelector(primary)
	result, err := transport.Send(context.Background(), sel, &types.PayLoad{}, transport.TransportEndpoints{GRPC: "host:19090"})
	require.NoError(t, err)
	assert.Equal(t, []byte(`{"ok":true}`), result)
}

func TestSendDoesNotFallbackAfterGRPCSendStarts(t *testing.T) {
	grpcTransport := &mockTransport{name: "grpc", supports: true, sendErr: context.DeadlineExceeded}
	httpTransport := &mockTransport{name: "http", supports: true, sendResult: []byte("unexpected")}
	selector := transport.NewDefaultSelector(grpcTransport, httpTransport)

	_, err := transport.Send(context.Background(), selector, &types.PayLoad{}, transport.TransportEndpoints{
		GRPC: "orders:19090", HTTP: "http://orders:8080",
	})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, int32(1), grpcTransport.sendCalls.Load())
	assert.Zero(t, httpTransport.sendCalls.Load())
}

func TestSelectorStatsAreScopedAndLowCardinality(t *testing.T) {
	grpcTransport := &mockTransport{name: "grpc", supports: true, healthErr: errors.New("unhealthy")}
	httpTransport := &mockTransport{name: "http", supports: true, sendErr: errors.New("send failed")}
	stats := &transport.Stats{}
	selector := transport.NewDefaultSelector(grpcTransport, httpTransport)
	selector.SetStats(stats)

	_, err := transport.Send(context.Background(), selector, &types.PayLoad{}, transport.TransportEndpoints{
		GRPC: "orders:19090", HTTP: "http://orders:8080",
	})
	require.EqualError(t, err, "send failed")

	assert.Equal(t, transport.StatsSnapshot{
		HTTPSelected: 1,
		SendFailure:  1,
		HTTPFallback: 1,
	}, stats.Snapshot())
}
