package transport_test

import (
	"context"
	"errors"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/transport"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTransport is a test double for transport.Transport.
type mockTransport struct {
	name       string
	supports   bool
	healthErr  error
	sendResult []byte
	sendErr    error
}

func (m *mockTransport) Name() string { return m.name }
func (m *mockTransport) Start(_ context.Context) error { return nil }
func (m *mockTransport) Stop(_ context.Context) error  { return nil }
func (m *mockTransport) Supports(_ context.Context, _ *types.PayLoad, _ string) bool {
	return m.supports
}
func (m *mockTransport) Health(_ context.Context, _ string) error { return m.healthErr }
func (m *mockTransport) Send(_ context.Context, _ *types.PayLoad, _ string) ([]byte, error) {
	return m.sendResult, m.sendErr
}

func TestDefaultSelector_SelectPrimary(t *testing.T) {
	primary := &mockTransport{name: "grpc", supports: true, healthErr: nil}
	sel := transport.NewDefaultSelector(primary)

	chosen, err := sel.Select(context.Background(), &types.PayLoad{}, "host:19090")
	require.NoError(t, err)
	assert.Equal(t, "grpc", chosen.Name())
}

func TestDefaultSelector_FallbackWhenPrimaryUnhealthy(t *testing.T) {
	primary := &mockTransport{name: "grpc", supports: true, healthErr: errors.New("refused")}
	fallback := &mockTransport{name: "http", supports: true, healthErr: nil}
	sel := transport.NewDefaultSelector(primary, fallback)

	chosen, err := sel.Select(context.Background(), &types.PayLoad{}, "http://host:8080")
	require.NoError(t, err)
	assert.Equal(t, "http", chosen.Name())
}

func TestDefaultSelector_FallbackWhenPrimaryNotSupported(t *testing.T) {
	primary := &mockTransport{name: "socket", supports: false}
	fallback := &mockTransport{name: "http", supports: true, healthErr: nil}
	sel := transport.NewDefaultSelector(primary, fallback)

	chosen, err := sel.Select(context.Background(), &types.PayLoad{}, "target")
	require.NoError(t, err)
	assert.Equal(t, "http", chosen.Name())
}

func TestDefaultSelector_ErrorWhenAllUnhealthy(t *testing.T) {
	primary := &mockTransport{name: "grpc", supports: true, healthErr: errors.New("refused")}
	fallback := &mockTransport{name: "http", supports: true, healthErr: errors.New("timeout")}
	sel := transport.NewDefaultSelector(primary, fallback)

	_, err := sel.Select(context.Background(), &types.PayLoad{}, "target")
	assert.ErrorIs(t, err, transport.ErrNoTransport)
}

func TestDefaultSelector_ErrorWhenNoneSupported(t *testing.T) {
	primary := &mockTransport{name: "grpc", supports: false}
	fallback := &mockTransport{name: "socket", supports: false}
	sel := transport.NewDefaultSelector(primary, fallback)

	_, err := sel.Select(context.Background(), &types.PayLoad{}, "target")
	assert.ErrorIs(t, err, transport.ErrNoTransport)
}

func TestSendWithFallback_ReturnsResult(t *testing.T) {
	primary := &mockTransport{
		name:       "grpc",
		supports:   true,
		healthErr:  nil,
		sendResult: []byte(`{"ok":true}`),
	}
	sel := transport.NewDefaultSelector(primary)
	result, err := transport.SendWithFallback(context.Background(), sel, &types.PayLoad{}, "host:19090")
	require.NoError(t, err)
	assert.Equal(t, []byte(`{"ok":true}`), result)
}
