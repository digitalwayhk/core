// 本文件验证框架专用通知桥与业务 EventBridge 隔离，并对非法内部消息保守失效。
package router

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/stretchr/testify/require"
)

func TestInternalNotificationBridgeBroadcastsWithoutLocalEcho(t *testing.T) {
	for _, provider := range []string{"redis-stream", "nats-jetstream"} {
		t.Run(provider, func(t *testing.T) {
			cfg := config.MQConfig{Mode: "on", Provider: provider}
			cfg.ApplyDefaults()
			cfg.RedisStream.Addr = os.Getenv("CORE_TEST_REDIS_ADDR")
			cfg.NATSJetStream.URL = os.Getenv("CORE_TEST_NATS_URL")
			if provider == "redis-stream" && cfg.RedisStream.Addr == "" || provider == "nats-jetstream" && cfg.NATSJetStream.URL == "" {
				t.Skip("NOT RUN: real Broker not configured")
			}
			service := "notifybridge" + time.Now().Format("150405.000000000")
			var bridges []*internalNotificationBridge
			var received []chan *event.Envelope
			for i := 0; i < 2; i++ {
				local := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{})
				t.Cleanup(func() { _ = local.Close(context.Background()) })
				b, err := newInternalNotificationBridge(context.Background(), cfg, local, service, "cache")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = b.Close() })
				ch := make(chan *event.Envelope, 8)
				if _, err := b.Subscribe("routecache.invalidate."+service, func(e *event.Envelope) { ch <- e }); err != nil {
					t.Fatal(err)
				}
				if _, err := b.SubscribeExternal(context.Background(), service+".routecache.invalidate"); err != nil {
					t.Fatal(err)
				}
				bridges = append(bridges, b)
				received = append(received, ch)
			}
			data, _ := json.Marshal(map[string]any{"service": service, "route": "/api/products", "generation": 1})
			env := event.NewEnvelope(service, "routecache.invalidate."+service, data)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := bridges[0].Publish(ctx, event.PublishRequest{Class: event.ControlDelivery, External: true, Subject: service + ".routecache.invalidate", Envelope: env}); err != nil {
				t.Fatal(err)
			}
			for _, ch := range received {
				select {
				case got := <-ch:
					if got.ID != env.ID {
						t.Fatal("wrong envelope")
					}
				case <-ctx.Done():
					t.Fatal("missing broadcast")
				}
			}
			for _, ch := range received {
				select {
				case <-ch:
					t.Fatal("local echo duplicate")
				case <-time.After(50 * time.Millisecond):
				}
			}
			if _, err := bridges[0].SubscribeExternal(ctx, "business.order.created"); err == nil {
				t.Fatal("business subscription hijacked")
			}
			if err := bridges[0].Publish(ctx, event.PublishRequest{External: true, Subject: "business.order.created", Envelope: env}); err == nil {
				t.Fatal("business publish hijacked")
			}
		})
	}
}

func TestInternalNotificationPublishBoundsBlockedLocalHandler(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT RUN: 真实 Redis 未配置")
	}
	cfg := config.MQConfig{Mode: "on", Provider: "redis-stream"}
	cfg.ApplyDefaults()
	cfg.RedisStream.Addr = addr
	local := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{})
	defer local.Close(context.Background())
	service := "blocked-notify-" + time.Now().Format("150405.000000000")
	b, err := newInternalNotificationBridge(context.Background(), cfg, local, service, "cache")
	require.NoError(t, err)
	defer b.Close()
	release := make(chan struct{})
	defer close(release)
	_, err = b.Subscribe(b.eventType, func(*event.Envelope) { <-release })
	require.NoError(t, err)
	data, _ := json.Marshal(map[string]string{"service": service, "route": "/api/items"})
	env := event.NewEnvelope(service, b.eventType, data)
	done := make(chan error, 1)
	go func() {
		done <- b.Publish(context.Background(), event.PublishRequest{Class: event.ControlDelivery, External: true, Subject: b.subject, Envelope: env})
	}()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(4 * time.Second):
		t.Fatal("内部通知本地处理超预算后仍阻塞发布方")
	}
}
