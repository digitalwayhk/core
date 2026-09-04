package event_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
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

type keyedReliableBridgeProvider struct {
	reliableBridgeProvider
	options mq.ReliableSubscribeOptions
}

func (*keyedReliableBridgeProvider) SupportsKeyedReliableConcurrency() bool { return true }

func (p *keyedReliableBridgeProvider) SubscribeReliable(
	_ context.Context,
	_ string,
	options mq.ReliableSubscribeOptions,
	handler func(*mq.Message) error,
) (func(), error) {
	p.options = options
	p.handler = handler
	return func() {}, nil
}

func TestMQBridgeRejectsKeyConcurrencyOnKafkaAndRabbitMQ(t *testing.T) {
	providers := []mq.MQProvider{
		mq.NewKafkaProvider(config.KafkaMQConfig{Brokers: []string{"127.0.0.1:9092"}}),
		mq.NewRabbitMQProvider(config.RabbitMQConfig{
			URL: "amqp://guest:guest@127.0.0.1:5672/", Exchange: "events", Prefetch: 1,
		}),
	}
	for _, provider := range providers {
		t.Run(provider.Name(), func(t *testing.T) {
			manager := mq.NewManager()
			manager.Register(provider)
			require.NoError(t, manager.SetCurrent(provider.Name()))
			bridge := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{
				SubscriberID: "order-service",
			})
			t.Cleanup(func() { require.NoError(t, bridge.Close(context.Background())) })
			bridge.SetExternalPublisher(event.NewMQBridge(event.NewStream(), manager))

			_, err := bridge.SubscribeEvent(event.Subscription{
				Subject: "fills", Reliable: true, KeyConcurrency: 2,
				Handler: func(context.Context, *event.Envelope) error { return nil },
			})
			require.ErrorIs(t, err, mq.ErrKeyedReliableSubscribeUnsupported)
		})
	}
}

func TestMQBridgeReliableSubscriptionPropagatesKeyConcurrency(t *testing.T) {
	provider := &keyedReliableBridgeProvider{}
	manager := mq.NewManager()
	manager.Register(provider)
	require.NoError(t, manager.SetCurrent(provider.Name()))
	bridge := event.NewMQBridge(event.NewStream(), manager)

	cancel, err := bridge.SubscribeReliableWithOptions(
		context.Background(),
		"fills",
		"positions",
		event.ReliableExternalSubscribeOptions{KeyConcurrency: 6},
	)
	require.NoError(t, err)
	defer cancel()
	require.Equal(t, 6, provider.options.KeyConcurrency)
}
