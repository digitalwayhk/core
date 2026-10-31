// 本文件验证新批次入口的Broker读取、调度、handler与ACK观测互不混用，不记录业务键或正文。
package mq

import (
	"context"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"strings"
	"testing"
	"time"
)

func TestRedisOwnedBatchTraceAndMetricsSeparateStages(t *testing.T) {
	previous := otel.GetTracerProvider()
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = tp.Shutdown(context.Background()) })
	p, _, m := ownershipFixture(t)
	ctx := context.Background()
	require.NoError(t, p.BindOwnership(ctx, m))
	runtime, err := p.SubscribeOwnedBatch(ctx, OwnedReliableOptions{Manifest: m, InstanceID: "instance", SessionID: "boot", MaxOwned: 1, NoDueTasks: true, Key: func(*Message) (string, error) { return "A", nil }, Activate: func(context.Context, ownership.Credential) error { return nil }}, func(context.Context, ownership.Credential, []*Message) error { return nil })
	require.NoError(t, err)
	defer runtime.Close(ctx)
	require.NoError(t, p.Publish(ctx, "one", []byte("never-trace-body"), &PublishOptions{OrderingKey: "never-trace-user"}))
	spans := map[string]sdktrace.ReadOnlySpan{}
	require.Eventually(t, func() bool {
		for _, s := range recorder.Ended() {
			spans[s.Name()] = s
		}
		return spans["redis.owned.batch"] != nil && spans["redis.owned.handler"] != nil && spans["redis.owned.ack"] != nil && spans["redis.reliable.read"] != nil
	}, 2*time.Second, time.Millisecond)
	require.False(t, spans["redis.owned.ack"].StartTime().Before(spans["redis.owned.handler"].EndTime()))
	for _, s := range recorder.Ended() {
		data := fmt.Sprint(s.Attributes(), s.Events(), s.Status())
		require.False(t, strings.Contains(data, "never-trace-body"))
		require.False(t, strings.Contains(data, "never-trace-user"))
	}
	require.EqualValues(t, 1, p.deliveryMetrics.newRead.Load())
	require.Positive(t, p.deliveryMetrics.ackNanos.Load())
	require.Positive(t, p.deliveryMetrics.scheduleWaitNanos.Load())
	require.Positive(t, p.deliveryMetrics.handlerNanos.Load())
}
