package event_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/stretchr/testify/require"
)

type reliableBridgeProvider struct {
	handler func(*mq.Message) error
}

func (*reliableBridgeProvider) Name() string                  { return "reliable-bridge" }
func (*reliableBridgeProvider) Connect(context.Context) error { return nil }
func (*reliableBridgeProvider) Close() error                  { return nil }
func (*reliableBridgeProvider) Publish(context.Context, string, []byte, *mq.PublishOptions) error {
	return nil
}
func (*reliableBridgeProvider) Subscribe(context.Context, string, func(*mq.Message)) (func(), error) {
	return func() {}, nil
}
func (*reliableBridgeProvider) Health(context.Context) error { return nil }
func (p *reliableBridgeProvider) SubscribeReliable(_ context.Context, _ string, _ mq.ReliableSubscribeOptions, handler func(*mq.Message) error) (func(), error) {
	p.handler = handler
	return func() {}, nil
}

func TestMQBridgeReliableSubscriptionPropagatesControlHandlerError(t *testing.T) {
	stream := event.NewStream()
	want := errors.New("write inbox failed")
	cancelHandler, err := stream.SubscribeControl("order.changed", func(*event.Envelope) error { return want })
	require.NoError(t, err)
	defer cancelHandler()
	provider := &reliableBridgeProvider{}
	manager := mq.NewManager()
	manager.Register(provider)
	require.NoError(t, manager.SetCurrent(provider.Name()))
	bridge := event.NewMQBridge(stream, manager)
	cancel, err := bridge.SubscribeReliable(context.Background(), "orders.changed", "user-service")
	require.NoError(t, err)
	defer cancel()

	envelope := event.NewEnvelope("orders", "order.changed", nil)
	data, err := json.Marshal(envelope)
	require.NoError(t, err)
	require.ErrorIs(t, provider.handler(&mq.Message{ID: "1-0", Subject: "orders.changed", Data: data}), want)
}
