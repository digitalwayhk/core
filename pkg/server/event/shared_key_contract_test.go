package event_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/stretchr/testify/require"
)

func TestSharedKeySubscriptionCannotSilentlyBecomeLocalOrBroadcast(t *testing.T) {
	for _, data := range []string{`{"Subject":"fills","Reliable":true,"SharedKeys":true}`, `{"Subject":"fills","SharedKeys":true}`, `{"Subject":"fills","Reliable":true,"SharedKeys":true,"Broadcast":true}`} {
		t.Run(data, func(t *testing.T) {
			b := event.NewServiceEventBridge(nil, event.ServiceEventBridgeOptions{SubscriberID: "users"})
			defer b.Close(context.Background())
			var sub event.Subscription
			require.NoError(t, json.Unmarshal([]byte(data), &sub))
			sub.Handler = func(context.Context, *event.Envelope) error { return nil }
			_, err := b.SubscribeEvent(sub)
			require.Error(t, err)
		})
	}
}

type sharedExternalAdapter struct {
	keyedExternalEventAdapter
	shared   bool
	prefetch interface{}
}

func (*sharedExternalAdapter) EnsureSharedKeyReliable() error { return nil }
func (a *sharedExternalAdapter) SubscribeReliableWithOptions(_ context.Context, _, group string, o event.ReliableExternalSubscribeOptions) (func(), error) {
	a.shared = o.SharedKeys
	data, _ := json.Marshal(o)
	var values map[string]interface{}
	_ = json.Unmarshal(data, &values)
	a.prefetch = values["Prefetch"]
	a.reliableID = group
	return func() {}, nil
}
func TestSharedKeyBridgePreservesLogicalGroupAndRejectsConflictingReferences(t *testing.T) {
	b := event.NewServiceEventBridge(nil, event.ServiceEventBridgeOptions{SubscriberID: "users"})
	defer b.Close(context.Background())
	adapter := &sharedExternalAdapter{}
	b.SetExternalPublisher(adapter)
	stop, err := b.SubscribeEvent(event.Subscription{Subject: "fills", Reliable: true, SharedKeys: true, Handler: func(context.Context, *event.Envelope) error { return nil }})
	require.NoError(t, err)
	defer stop()
	require.True(t, adapter.shared)
	require.Equal(t, "users", adapter.reliableID)
	_, err = b.SubscribeEvent(event.Subscription{Subject: "fills", Reliable: true, Handler: func(context.Context, *event.Envelope) error { return nil }})
	require.Error(t, err, "相同 subject 不能混用共享与旧模式")
}

func TestSharedKeyBridgePropagatesExplicitPrefetchBudget(t *testing.T) {
	b := event.NewServiceEventBridge(nil, event.ServiceEventBridgeOptions{SubscriberID: "users"})
	defer b.Close(context.Background())
	adapter := &sharedExternalAdapter{}
	b.SetExternalPublisher(adapter)
	var sub event.Subscription
	require.NoError(t, json.Unmarshal([]byte(`{"Subject":"fills","Reliable":true,"SharedKeys":true,"Prefetch":8}`), &sub))
	sub.Handler = func(context.Context, *event.Envelope) error { return nil }
	stop, err := b.SubscribeEvent(sub)
	require.NoError(t, err)
	defer stop()
	require.Equal(t, float64(8), adapter.prefetch)
}
