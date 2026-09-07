// 本文件验证持续通知不创建历史，同时保留升级前的显式历史资源。
package controlnotify

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/go-redis/v9"
)

func TestInternalNotificationSustainedHistoryAndMigration(t *testing.T) {
	brokerConfigs(t, func(t *testing.T, cfg config.MQConfig) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var count func() uint64
		var checkNew func()
		if cfg.Provider == "redis-stream" {
			c := redis.NewClient(&redis.Options{Addr: cfg.RedisStream.Addr, DB: cfg.RedisStream.DB})
			defer c.Close()
			key := cfg.RedisStream.Prefix + ":users.routecache.invalidate"
			if err := c.XAdd(ctx, &redis.XAddArgs{Stream: key, Values: map[string]any{"data": "legacy"}}).Err(); err != nil {
				t.Fatal(err)
			}
			defer c.Del(context.Background(), key)
			count = func() uint64 {
				n, err := c.XLen(ctx, key).Result()
				if err != nil {
					t.Fatal(err)
				}
				return uint64(n)
			}
			checkNew = func() {
				kind, err := c.Type(ctx, channelName(cfg, "users", "cache")).Result()
				if err != nil || kind != "none" {
					t.Fatalf("new channel persisted: %s %v", kind, err)
				}
			}
		} else {
			c, err := nats.Connect(cfg.NATSJetStream.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			js, err := jetstream.New(c)
			if err != nil {
				t.Fatal(err)
			}
			name := cfg.NATSJetStream.StreamPrefix + "_legacy"
			subject := cfg.NATSJetStream.StreamPrefix + ".users.routecache.invalidate"
			stream, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: name, Subjects: []string{subject}})
			if err != nil {
				t.Fatal(err)
			}
			defer js.DeleteStream(context.Background(), name)
			if _, err := js.Publish(ctx, subject, []byte("legacy")); err != nil {
				t.Fatal(err)
			}
			count = func() uint64 {
				info, err := stream.Info(ctx)
				if err != nil {
					t.Fatal(err)
				}
				return info.State.Msgs
			}
			checkNew = func() {
				if _, err := js.StreamNameBySubject(ctx, channelName(cfg, "users", "cache")); err != jetstream.ErrStreamNotFound {
					t.Fatalf("new channel capture: %v", err)
				}
			}
		}
		x := testOpen(t, cfg, "users", "cache")
		started := time.Now()
		for i := 0; i < 10240; i++ {
			if err := x.Publish(ctx, []byte("notification")); err != nil {
				t.Fatal(err)
			}
			if _, err := x.Receive(ctx); err != nil {
				t.Fatal(err)
			}
			if i == 1023 || i == 10239 {
				if got := count(); got != 1 {
					t.Fatalf("legacy history changed: %d", got)
				}
				checkNew()
			}
		}
		t.Logf("连续 10240 条，耗时 %s；旧历史始终 1 条；新通知主题无持久化配置", time.Since(started))
	})
}

func TestNATSSlowConsumerFailsClosed(t *testing.T) {
	brokerConfigs(t, func(t *testing.T, cfg config.MQConfig) {
		if cfg.Provider != "nats-jetstream" {
			t.Skip("NATS native pending limit")
		}
		x := testOpen(t, cfg, "users", "cache").(*natsTransport)
		producer, err := nats.Connect(cfg.NATSJetStream.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer producer.Close()
		for i := 0; i < 1024; i++ {
			if err := producer.Publish(x.channel, []byte("burst")); err != nil {
				t.Fatal(err)
			}
		}
		if err := producer.FlushTimeout(time.Second); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for !x.failed.Load() && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if !x.failed.Load() {
			t.Fatal("pending overflow did not fail closed")
		}
		if _, err := x.Receive(context.Background()); err == nil {
			t.Fatal("slow consumer silently resumed after dropping messages")
		}
	})
}
