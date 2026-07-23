package event_test

import (
	"context"
	"errors"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/stretchr/testify/require"
)

func TestStreamPublishControlReturnsHandlerError(t *testing.T) {
	stream := event.NewStream()
	want := errors.New("persist inbox failed")
	cancel, err := stream.SubscribeControl("order.changed", func(*event.Envelope) error { return want })
	require.NoError(t, err)
	defer cancel()

	err = stream.PublishControl(context.Background(), event.NewEnvelope("orders", "order.changed", nil))
	require.ErrorIs(t, err, want)
}

func TestStreamPublishControlConvertsHandlerPanicToError(t *testing.T) {
	stream := event.NewStream()
	cancel, err := stream.SubscribeControl("order.changed", func(*event.Envelope) error {
		panic("inbox panic")
	})
	require.NoError(t, err)
	defer cancel()

	require.Error(t, stream.PublishControl(context.Background(), event.NewEnvelope("orders", "order.changed", nil)))
}
