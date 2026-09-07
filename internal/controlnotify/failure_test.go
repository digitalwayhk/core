// 本文件验证内部广播的命名隔离、取消、容量拒绝及持久化冲突边界。
package controlnotify

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func TestInternalChannelNamespaceIsolation(t *testing.T) {
	base := config.MQConfig{Provider: "redis-stream", RedisStream: config.RedisStreamMQConfig{Prefix: "prod", DB: 0}}
	name := channelName(base, "users", "cache")
	variants := []config.MQConfig{base, base}
	variants[0].RedisStream.DB = 1
	variants[1].RedisStream.Prefix = "test"
	for _, cfg := range variants {
		if channelName(cfg, "users", "cache") == name {
			t.Fatal("DB or prefix not isolated")
		}
	}
	if channelName(base, "users.*", "cache") == name {
		t.Fatal("service not isolated")
	}
	if channelName(base, "users", "identity") == name {
		t.Fatal("kind not isolated")
	}
	if bytes.ContainsAny([]byte(channelName(base, "users.*>", "cache")), "*>") {
		t.Fatal("wildcard injection")
	}
}

func TestInternalNotificationsRejectOversizeAndClose(t *testing.T) {
	brokerConfigs(t, func(t *testing.T, cfg config.MQConfig) {
		x := testOpen(t, cfg, "users", "cache")
		if err := x.Publish(context.Background(), make([]byte, maxPayloadBytes+1)); err == nil {
			t.Fatal("oversize accepted")
		}
		if err := x.Publish(context.Background(), nil); err == nil {
			t.Fatal("empty accepted")
		}
		if err := x.Close(); err != nil {
			t.Fatal(err)
		}
		if err := x.Close(); err != nil {
			t.Fatal(err)
		}
		if err := x.Publish(context.Background(), []byte("closed")); err == nil {
			t.Fatal("publish after close accepted")
		}
		if _, err := x.Receive(context.Background()); err == nil {
			t.Fatal("receive after close accepted")
		}
	})
}

func TestInternalNotificationReceiveCancellation(t *testing.T) {
	brokerConfigs(t, func(t *testing.T, cfg config.MQConfig) {
		x := testOpen(t, cfg, "users", "cache")
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { _, err := x.Receive(ctx); done <- err }()
		cancel()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("canceled receive succeeded")
			}
		case <-time.After(time.Second):
			t.Fatal("cancellation did not unblock")
		}
	})
}

func TestInternalNotificationsRemainAliveWhileIdle(t *testing.T) {
	brokerConfigs(t, func(t *testing.T, cfg config.MQConfig) {
		x := testOpen(t, cfg, "users", "identity")
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		done := make(chan error, 1)
		go func() {
			data, err := x.Receive(ctx)
			if err == nil && string(data) != "after-idle" {
				err = errors.New("wrong data")
			}
			done <- err
		}()
		// 超过读取超时仍必须由心跳维持连续性；之后用真实通知证明连接有效。
		timer := time.NewTimer(4 * time.Second)
		defer timer.Stop()
		select {
		case err := <-done:
			t.Fatalf("idle receiver exited early: %v", err)
		case <-timer.C:
		}
		if err := x.Publish(ctx, []byte("after-idle")); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	})
}

func TestNATSInternalNotificationsRejectStreamCapture(t *testing.T) {
	brokerConfigs(t, func(t *testing.T, cfg config.MQConfig) {
		if cfg.Provider != "nats-jetstream" {
			t.Skip("NATS-specific capture check")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		nc, err := nats.Connect(cfg.NATSJetStream.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer nc.Close()
		js, err := jetstream.New(nc)
		if err != nil {
			t.Fatal(err)
		}
		x := testOpen(t, cfg, "users", "identity")
		name := cfg.NATSJetStream.StreamPrefix
		_, err = js.CreateStream(ctx, jetstream.StreamConfig{Name: name, Subjects: []string{channelName(cfg, "users", "identity")}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			c, e := nats.Connect(cfg.NATSJetStream.URL)
			if e == nil {
				defer c.Close()
				j, e := jetstream.New(c)
				if e == nil {
					_ = j.DeleteStream(ctx, name)
				}
			}
		})
		if err := x.Publish(ctx, []byte("must-not-persist")); err == nil {
			t.Fatal("captured publish accepted")
		}
		stream, err := js.Stream(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		info, err := stream.Info(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if info.State.Msgs != 0 {
			t.Fatal("rejected notification was persisted")
		}
		if opened, err := Open(ctx, cfg, "users", "identity"); err == nil {
			_ = opened.Close()
			t.Fatal("captured subscription accepted")
		}
	})
}

// 读取者停止处理时不能无限积累心跳，必须显式失效供上层补偿。
func TestRedisStoppedReaderLosesContinuity(t *testing.T) {
	brokerConfigs(t, func(t *testing.T, cfg config.MQConfig) {
		if cfg.Provider != "redis-stream" {
			t.Skip("Redis-specific reader watchdog")
		}
		x := testOpen(t, cfg, "users", "cache")
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		<-timer.C
		if err := x.Publish(context.Background(), []byte("stalled")); err == nil {
			t.Fatal("stopped reader remained healthy beyond watchdog budget")
		}
	})
}

// 使用真实连接并注入原生异步错误；即使之后没有新消息，阻塞接收也必须退出。
func TestNATSAsyncFailureUnblocksIdleReceiver(t *testing.T) {
	brokerConfigs(t, func(t *testing.T, cfg config.MQConfig) {
		if cfg.Provider != "nats-jetstream" {
			t.Skip("NATS-specific async error")
		}
		x := testOpen(t, cfg, "users", "identity").(*natsTransport)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := x.Receive(ctx); done <- err }()
		time.Sleep(50 * time.Millisecond)
		x.conn.Opts.AsyncErrorCB(x.conn, x.sub, nats.ErrSlowConsumer)
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("failed receive succeeded")
			}
		case <-time.After(time.Second):
			t.Fatal("async failure left receive blocked without a new message")
		}
	})
}
