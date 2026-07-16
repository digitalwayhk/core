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

type retryRecordingTransport struct {
	name        string
	healthCalls atomic.Int32
	sendCalls   atomic.Int32
	healthErr   error
	sendErr     error
}

func (t *retryRecordingTransport) Name() string              { return t.name }
func (*retryRecordingTransport) Start(context.Context) error { return nil }
func (*retryRecordingTransport) Stop(context.Context) error  { return nil }
func (*retryRecordingTransport) Supports(context.Context, *types.PayLoad, string) bool {
	return true
}
func (t *retryRecordingTransport) Health(context.Context, string) error {
	t.healthCalls.Add(1)
	return t.healthErr
}
func (t *retryRecordingTransport) Send(context.Context, *types.PayLoad, string) ([]byte, error) {
	t.sendCalls.Add(1)
	return nil, t.sendErr
}

func TestSelectWithRetryRetriesOnlyHealthSelection(t *testing.T) {
	healthErr := errors.New("health unavailable")
	grpcTransport := &retryRecordingTransport{name: "grpc", healthErr: healthErr}
	selector := transport.NewDefaultSelector(grpcTransport)

	_, err := transport.SelectWithRetry(context.Background(), selector, &types.PayLoad{},
		transport.TransportEndpoints{GRPC: "orders:19090"}, 3, 0)

	require.ErrorIs(t, err, transport.ErrNoTransport)
	assert.Equal(t, int32(3), grpcTransport.healthCalls.Load())
	assert.Zero(t, grpcTransport.sendCalls.Load())
}

func TestSendAfterSelectWithRetryIsNeverRetried(t *testing.T) {
	grpcTransport := &retryRecordingTransport{name: "grpc", sendErr: context.DeadlineExceeded}
	selector := transport.NewDefaultSelector(grpcTransport)
	endpoints := transport.TransportEndpoints{GRPC: "orders:19090"}

	selection, err := transport.SelectWithRetry(context.Background(), selector, &types.PayLoad{}, endpoints, 3, 0)
	require.NoError(t, err)
	_, err = selection.Transport.Send(context.Background(), &types.PayLoad{}, selection.Endpoint)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, int32(1), grpcTransport.healthCalls.Load())
	assert.Equal(t, int32(1), grpcTransport.sendCalls.Load())
}
