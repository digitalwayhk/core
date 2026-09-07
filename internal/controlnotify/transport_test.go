// 本文件用真实 Broker 验证框架内部通知的广播与无历史存储契约。
package controlnotify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/go-redis/v9"
)

func brokerConfigs(t *testing.T, run func(*testing.T, config.MQConfig)) {
	t.Helper()
	for _, provider := range []string{"redis-stream", "nats-jetstream"} {
		t.Run(provider, func(t *testing.T) {
			prefix := fmt.Sprintf("notifytest%d", time.Now().UnixNano())
			cfg := config.MQConfig{Mode: "on", Provider: provider}
			cfg.ApplyDefaults()
			cfg.RedisStream = config.RedisStreamMQConfig{Addr: os.Getenv("CORE_TEST_REDIS_ADDR"), Prefix: prefix, DB: 0}
			cfg.NATSJetStream = config.NATSJetStreamMQConfig{URL: os.Getenv("CORE_TEST_NATS_URL"), StreamPrefix: prefix, DurablePrefix: prefix}
			if provider == "redis-stream" && cfg.RedisStream.Addr == "" || provider == "nats-jetstream" && cfg.NATSJetStream.URL == "" {
				t.Skip("NOT RUN: real Broker address not configured")
			}
			run(t, cfg)
		})
	}
}

func testOpen(t *testing.T, cfg config.MQConfig, service, kind string) Transport {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	x, err := Open(ctx, cfg, service, kind)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.Close() })
	return x
}

// 已消费的内部通知不能留下 Stream 历史；两个内置 Provider 使用相同契约。
func TestInternalNotificationsDoNotPersistHistory(t *testing.T) {
	brokerConfigs(t, func(t *testing.T, cfg config.MQConfig) {
		service := cfg.RedisStream.Prefix
		for _, kind := range []string{"cache", "identity"} {
			x := testOpen(t, cfg, service, kind)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			for i := 0; i < 32; i++ {
				if err := x.Publish(ctx, []byte("notification")); err != nil {
					t.Fatal(err)
				}
				if _, err := x.Receive(ctx); err != nil {
					t.Fatal(err)
				}
			}
		}
		assertNoHistory(t, cfg)
	})
}

func assertNoHistory(t *testing.T, cfg config.MQConfig) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if cfg.Provider == "redis-stream" {
		client := redis.NewClient(&redis.Options{Addr: cfg.RedisStream.Addr, DB: cfg.RedisStream.DB})
		defer client.Close()
		keys, err := client.Keys(ctx, "*"+cfg.RedisStream.Prefix+"*").Result()
		if err != nil {
			t.Fatal(err)
		}
		if len(keys) != 0 {
			t.Fatalf("internal notifications retained Redis history: %v", keys)
		}
		for _, kind := range []string{"cache", "identity"} {
			key := channelName(cfg, cfg.RedisStream.Prefix, kind)
			if value, err := client.Type(ctx, key).Result(); err != nil || value != "none" {
				t.Fatalf("notification channel persisted: type=%s error=%v", value, err)
			}
		}
		return
	}
	nc, err := nats.Connect(cfg.NATSJetStream.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	names := js.StreamNames(ctx)
	for name := range names.Name() {
		if strings.Contains(name, cfg.NATSJetStream.StreamPrefix) {
			t.Errorf("internal notifications retained JetStream history: %s", name)
		}
	}
	if err := names.Err(); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"cache", "identity"} {
		if _, err := js.StreamNameBySubject(ctx, channelName(cfg, cfg.RedisStream.Prefix, kind)); !errors.Is(err, jetstream.ErrStreamNotFound) {
			t.Fatalf("notification subject persisted: %v", err)
		}
	}
}

// 同服务两个活动副本各收到一次通知，不允许共享消费组竞争。
func TestInternalNotificationsReachEveryReplica(t *testing.T) {
	brokerConfigs(t, func(t *testing.T, cfg config.MQConfig) {
		a := testOpen(t, cfg, cfg.RedisStream.Prefix, "cache")
		b := testOpen(t, cfg, cfg.RedisStream.Prefix, "cache")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := a.Publish(ctx, []byte("invalidate")); err != nil {
			t.Fatal(err)
		}
		for index, x := range []Transport{a, b} {
			data, err := x.Receive(ctx)
			if err != nil {
				t.Fatalf("replica %d did not receive broadcast: %v", index, err)
			}
			if string(data) != "invalidate" {
				t.Fatalf("unexpected notification: %q", data)
			}
		}
	})
}
