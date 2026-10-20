package event_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/stretchr/testify/require"
)

func TestMQBridgeRedisReliableNextWindowReachesRegisteredControlHandler(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: CORE_TEST_REDIS_ADDR")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider := mq.NewRedisStreamProvider(addr, fmt.Sprintf("core:bridge:window:%d", time.Now().UnixNano()), 0)
	require.NoError(t, provider.Connect(ctx))
	manager := mq.NewManager()
	manager.Register(provider)
	require.NoError(t, manager.SetCurrent(provider.Name()))
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	stream := event.NewStream()
	bridge := event.NewMQBridge(stream, manager)
	require.NoError(t, bridge.EnsureOrderedReliable())
	hold := make(chan struct{})
	defer close(hold)
	aStarted, bFirst, bNext := make(chan struct{}), make(chan struct{}), make(chan struct{})
	stopHandler, err := stream.SubscribeControl("changed", func(env *event.Envelope) error {
		switch string(env.Data) {
		case "A1":
			close(aStarted)
			<-hold
		case "B1":
			close(bFirst)
		case "B2":
			close(bNext)
		}
		return nil
	})
	require.NoError(t, err)
	defer stopHandler()
	publish := func(key, body string) {
		env := event.NewEnvelope("source", "changed", []byte(body))
		env.ShardKey = key
		require.NoError(t, bridge.Publish(ctx, "fills", env))
	}
	publish("A", "A1")
	publish("B", "B1")
	stop, err := bridge.SubscribeReliableWithOptions(ctx, "fills", "users", event.ReliableExternalSubscribeOptions{KeyConcurrency: 2})
	require.NoError(t, err)
	defer stop()
	for _, signal := range []chan struct{}{aStarted, bFirst} {
		select {
		case <-signal:
		case <-ctx.Done():
			t.Fatal("正式可靠桥接链未进入首窗口 handler")
		}
	}
	publish("B", "B2")
	select {
	case <-bNext:
	case <-time.After(time.Second):
		t.Fatal("正式 Redis → MQBridge → PublishControl → 注册 handler 链仍有跨窗口屏障")
	}
}
