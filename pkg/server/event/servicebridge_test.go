package event_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestServiceEventBridge(t *testing.T, observerQueueSize int) *event.ServiceEventBridge {
	t.Helper()
	bridge := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{
		ObserverQueueSize: observerQueueSize,
		ControlQueueSize:  8,
		ControlShards:     2,
	})
	t.Cleanup(func() { require.NoError(t, bridge.Close(context.Background())) })
	return bridge
}

func TestServiceEventBridgeDropsObserverBeforePayloadBuildWhenUnused(t *testing.T) {
	bridge := newTestServiceEventBridge(t, 2)
	var builds atomic.Int32

	err := bridge.Publish(context.Background(), event.PublishRequest{
		Class:    event.ObserverDelivery,
		Envelope: event.NewEnvelope("service-a", "router.request", nil),
		BuildData: func() ([]byte, error) {
			builds.Add(1)
			return []byte(`{"secret":true}`), nil
		},
	})

	require.NoError(t, err)
	assert.Equal(t, int32(0), builds.Load())
}

func TestServiceEventBridgeObserverQueueIsBounded(t *testing.T) {
	bridge := newTestServiceEventBridge(t, 1)
	started := make(chan struct{})
	release := make(chan struct{})
	cancel, err := bridge.Subscribe("router.request", func(*event.Envelope) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
	})
	require.NoError(t, err)
	defer cancel()

	publish := func() {
		require.NoError(t, bridge.Publish(context.Background(), event.PublishRequest{
			Class:    event.ObserverDelivery,
			Envelope: event.NewEnvelope("service-a", "router.request", nil),
		}))
	}
	publish()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("观察事件处理器未启动")
	}
	publish()
	publish()

	assert.Equal(t, uint64(1), bridge.ObserverDropped())
	close(release)
}

func TestServiceEventBridgeControlEventsKeepShardOrder(t *testing.T) {
	bridge := newTestServiceEventBridge(t, 2)
	var mu sync.Mutex
	got := make([]string, 0, 3)
	cancel, err := bridge.Subscribe("cache.invalidate", func(env *event.Envelope) {
		mu.Lock()
		got = append(got, string(env.Data))
		mu.Unlock()
	})
	require.NoError(t, err)
	defer cancel()

	for _, value := range []string{"1", "2", "3"} {
		env := event.NewEnvelope("service-a", "cache.invalidate", []byte(value))
		env.ShardKey = "route-a:key-a"
		require.NoError(t, bridge.Publish(context.Background(), event.PublishRequest{
			Class:    event.ControlDelivery,
			Envelope: env,
		}))
	}

	mu.Lock()
	assert.Equal(t, []string{"1", "2", "3"}, got)
	mu.Unlock()
}

func TestServiceEventBridgeControlPublishWithoutExternalProviderFails(t *testing.T) {
	bridge := newTestServiceEventBridge(t, 2)
	env := event.NewEnvelope("service-a", "cache.invalidate", nil)
	env.ShardKey = "route-a:key-a"

	err := bridge.Publish(context.Background(), event.PublishRequest{
		Class:    event.ControlDelivery,
		External: true,
		Subject:  "service-a.cache.invalidate",
		Envelope: env,
	})

	assert.ErrorIs(t, err, event.ErrExternalProviderUnavailable)
}

func TestServiceEventBridgeCloseIsIdempotent(t *testing.T) {
	bridge := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{})

	require.NoError(t, bridge.Close(context.Background()))
	require.NoError(t, bridge.Close(context.Background()))
	assert.ErrorIs(t, bridge.Publish(context.Background(), event.PublishRequest{
		Class:    event.ObserverDelivery,
		Envelope: event.NewEnvelope("service-a", "router.request", nil),
	}), event.ErrServiceEventBridgeClosed)
}

func TestServiceEventBridgeHandlerPanicDoesNotStopControlWorker(t *testing.T) {
	bridge := newTestServiceEventBridge(t, 2)
	var calls atomic.Int32
	cancel, err := bridge.Subscribe("cache.invalidate", func(*event.Envelope) {
		if calls.Add(1) == 1 {
			panic("handler failure")
		}
	})
	require.NoError(t, err)
	defer cancel()

	for _, value := range []string{"1", "2"} {
		env := event.NewEnvelope("service-a", "cache.invalidate", []byte(value))
		env.ShardKey = "route-a:key-a"
		require.NoError(t, bridge.Publish(context.Background(), event.PublishRequest{
			Class:    event.ControlDelivery,
			Envelope: env,
		}))
	}

	assert.Equal(t, int32(2), calls.Load())
}

type fakeExternalEventAdapter struct {
	subscribeCalls atomic.Int32
	publishErr     error
	reliableID     string
}

func (a *fakeExternalEventAdapter) Publish(context.Context, string, *event.Envelope) error {
	return a.publishErr
}
func (a *fakeExternalEventAdapter) Subscribe(context.Context, string) (func(), error) {
	a.subscribeCalls.Add(1)
	return func() {}, nil
}
func (a *fakeExternalEventAdapter) SubscribeReliable(_ context.Context, _, subscriberID string) (func(), error) {
	a.reliableID = subscriberID
	return func() {}, nil
}

func TestServiceEventBridgeSubscribesThroughExternalAdapter(t *testing.T) {
	bridge := newTestServiceEventBridge(t, 2)
	adapter := &fakeExternalEventAdapter{}
	bridge.SetExternalPublisher(adapter)

	cancel, err := bridge.SubscribeExternal(context.Background(), "service-a.routecache.invalidate")
	require.NoError(t, err)
	cancel()
	assert.Equal(t, int32(1), adapter.subscribeCalls.Load())
}

func TestServiceEventBridgeReliableSubscriptionUsesLogicalServiceName(t *testing.T) {
	bridge := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{
		SubscriberID: "user-service",
	})
	t.Cleanup(func() { require.NoError(t, bridge.Close(context.Background())) })
	adapter := &fakeExternalEventAdapter{}
	bridge.SetExternalPublisher(adapter)

	cancel, err := bridge.SubscribeExternalControl(context.Background(), "order.changed")
	require.NoError(t, err)
	cancel()
	assert.Equal(t, "user-service", adapter.reliableID)
}

func TestServiceEventBridgeControlWaitsForLocalDeliveryAndPropagatesExternalFailure(t *testing.T) {
	bridge := newTestServiceEventBridge(t, 2)
	externalErr := errors.New("external publish failed")
	bridge.SetExternalPublisher(&fakeExternalEventAdapter{publishErr: externalErr})
	started := make(chan struct{})
	release := make(chan struct{})
	cancel, err := bridge.Subscribe("auth.casdoor.identity.changed", func(*event.Envelope) {
		close(started)
		<-release
	})
	require.NoError(t, err)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- bridge.Publish(context.Background(), event.PublishRequest{
			Class: event.ControlDelivery, External: true, Subject: "shop.auth.casdoor.identity.changed",
			Envelope: event.NewEnvelope("shop", "auth.casdoor.identity.changed", nil),
		})
	}()
	<-started
	select {
	case <-done:
		t.Fatal("本地控制事件处理完成前Publish不得返回")
	default:
	}
	close(release)
	require.ErrorIs(t, <-done, externalErr)
}
