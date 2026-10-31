package mq

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRedisReliableKeyedTraceClocksCorrelateWithoutPayloadOrKey(t *testing.T) {
	previous := otel.GetTracerProvider()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); require.NoError(t, provider.Shutdown(context.Background())) })
	p := newWindowRedisProvider(t)
	ctx := context.Background()
	stop, err := p.SubscribeReliable(ctx, "fills", ReliableSubscribeOptions{Group: "g", KeyConcurrency: 2, Count: 4}, func(*Message) error { return nil })
	require.NoError(t, err)
	defer stop()
	require.NoError(t, p.Publish(ctx, "fills", []byte("secret-body"), &PublishOptions{OrderingKey: "secret-user-key", IdempotencyKey: "secret-business-id"}))
	spans := map[string]sdktrace.ReadOnlySpan{}
	require.Eventually(t, func() bool {
		for _, span := range recorder.Ended() {
			spans[span.Name()] = span
		}
		return spans["redis.publish"] != nil && spans["redis.reliable.read"] != nil && spans["redis.reliable.delivery"] != nil && spans["redis.reliable.handler"] != nil
	}, 2*time.Second, time.Millisecond)
	var id string
	for _, attr := range spans["redis.publish"].Attributes() {
		if attr.Key == "messaging.message.id" {
			id = attr.Value.AsString()
		}
	}
	require.NotEmpty(t, id)
	var streamHash string
	for _, span := range spans {
		for _, attr := range span.Attributes() {
			if attr.Key == "messaging.redis.stream_hash" {
				if streamHash == "" {
					streamHash = attr.Value.AsString()
				}
				require.Equal(t, streamHash, attr.Value.AsString())
			}
		}
	}
	require.NotEmpty(t, streamHash, "Redis message ID 只在 stream 内唯一，关联必须包含 stream 身份")
	for _, name := range []string{"redis.reliable.delivery", "redis.reliable.handler"} {
		found := false
		for _, attr := range spans[name].Attributes() {
			if attr.Key == "messaging.message.id" && attr.Value.AsString() == id {
				found = true
			}
		}
		require.True(t, found, name)
	}
	events := map[string]time.Time{}
	for _, event := range spans["redis.reliable.delivery"].Events() {
		events[event.Name] = event.Time
	}
	for _, name := range []string{"redis.read", "redis.claim", "scheduler.admit", "delivery.start", "delivery.end"} {
		require.False(t, events[name].IsZero(), name)
	}
	require.False(t, events["redis.claim"].Before(events["redis.read"]))
	require.False(t, events["delivery.start"].Before(events["scheduler.admit"]))
	require.False(t, spans["redis.reliable.handler"].StartTime().Before(events["delivery.start"]))
	require.False(t, events["delivery.end"].Before(spans["redis.reliable.handler"].EndTime()))
	for _, span := range recorder.Ended() {
		data := fmt.Sprint(span.Attributes(), span.Events(), span.Status())
		for _, secret := range []string{"secret-body", "secret-user-key", "secret-business-id"} {
			require.False(t, strings.Contains(data, secret))
		}
	}
	metrics := p.RuntimeMetricSnapshot(ctx)
	require.Equal(t, float64(1), metrics.Counters["reliable_new_read_total"])
	require.Greater(t, metrics.Counters["reliable_schedule_wait_seconds_total"], float64(0))
	require.Greater(t, metrics.Counters["reliable_publish_to_read_seconds_total"], float64(0))
}
