package event_test

import (
	"context"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/stretchr/testify/require"
)

type plainExternal struct{}

func (*plainExternal) Publish(context.Context, string, *event.Envelope) error { return nil }

func TestRequireOrderedReliableFailClosedWithoutExternal(t *testing.T) {
	bridge := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{
		SubscriberID: "svc",
	})
	err := bridge.RequireOrderedReliableByShardKey()
	require.ErrorIs(t, err, event.ErrExternalProviderUnavailable)
	require.False(t, bridge.RequiresOrderedReliable())
}

func TestRequireOrderedReliableFailClosedWithoutCapability(t *testing.T) {
	bridge := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{
		SubscriberID: "svc",
	})
	bridge.SetExternalPublisher(&plainExternal{})
	err := bridge.RequireOrderedReliableByShardKey()
	require.ErrorIs(t, err, event.ErrOrderedReliableUnsupported)
	require.False(t, bridge.RequiresOrderedReliable())
}

func TestRequireOrderedReliableEmptyShardKeyRejectedOnExternalPublish(t *testing.T) {
	stream := event.NewStream()
	provider := mq.NewFakeOrderedReliableProvider()
	mgr := mq.NewManager()
	mgr.Register(provider)
	require.NoError(t, mgr.SetCurrent(provider.Name()))
	mqBridge := event.NewMQBridge(stream, mgr)

	bridge := event.NewServiceEventBridge(stream, event.ServiceEventBridgeOptions{SubscriberID: "svc"})
	bridge.SetExternalPublisher(mqBridge)
	require.NoError(t, bridge.RequireOrderedReliableByShardKey())
	require.True(t, bridge.RequiresOrderedReliable())

	env := event.NewEnvelope("svc", "fill", []byte(`{}`))
	err := bridge.Publish(context.Background(), event.PublishRequest{
		Class:    event.ControlDelivery,
		External: true,
		Subject:  "fills",
		Envelope: env,
	})
	require.ErrorIs(t, err, event.ErrOrderingKeyRequired)

	env.ShardKey = "market-a"
	require.NoError(t, bridge.Publish(context.Background(), event.PublishRequest{
		Class:    event.ControlDelivery,
		External: true,
		Subject:  "fills",
		Envelope: env,
	}))
}

func TestRequireOrderedReliableOptionDefersUntilExternalEnsured(t *testing.T) {
	stream := event.NewStream()
	// 仅构造选项不能在无 provider 校验时开启门禁。
	bridge := event.NewServiceEventBridge(stream, event.ServiceEventBridgeOptions{
		SubscriberID:                     "svc",
		RequireOrderedReliableByShardKey: true,
	})
	require.False(t, bridge.RequiresOrderedReliable())

	provider := mq.NewFakeOrderedReliableProvider()
	mgr := mq.NewManager()
	mgr.Register(provider)
	require.NoError(t, mgr.SetCurrent(provider.Name()))
	bridge.SetExternalPublisher(event.NewMQBridge(stream, mgr))
	require.True(t, bridge.RequiresOrderedReliable())
}

func TestRequireOrderedReliableOptionWithIncapableProviderFailClosedOnPublish(t *testing.T) {
	// option=true + 无能力 provider 不得静默降级为无序伪 key 路径。
	bridge := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{
		SubscriberID:                     "svc",
		RequireOrderedReliableByShardKey: true,
	})
	bridge.SetExternalPublisher(&plainExternal{})
	require.False(t, bridge.RequiresOrderedReliable())

	env := event.NewEnvelope("svc", "fill", []byte(`{}`))
	env.ShardKey = "market-a"
	err := bridge.Publish(context.Background(), event.PublishRequest{
		Class:    event.ControlDelivery,
		External: true,
		Subject:  "fills",
		Envelope: env,
	})
	require.ErrorIs(t, err, event.ErrOrderedReliableUnsupported)
}
