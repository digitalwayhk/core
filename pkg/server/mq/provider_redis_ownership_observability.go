// 本文件记录真实批次的调度、handler与ACK独立时钟，正文和业务key不进入观测。
package mq

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"time"
)

func (r *redisOwnedReader) runBatch(ctx context.Context, c ownership.Credential, rows []*Message) (result error) {
	admitted := time.Now()
	ctx, batch := otel.Tracer("github.com/digitalwayhk/core/mq").Start(ctx, "redis.owned.batch", trace.WithAttributes(attribute.Int("messaging.batch.count", len(rows)), attribute.String("messaging.consumer.instance", r.options.InstanceID)))
	defer func() {
		if result != nil {
			batch.SetStatus(codes.Error, "batch_failed_or_unknown")
		}
		batch.End()
	}()
	batch.AddEvent("scheduler.admit", trace.WithTimestamp(admitted))
	for _, row := range rows {
		batch.AddEvent("redis.claim", trace.WithAttributes(attribute.String("messaging.message.id", row.ID), attribute.String("messaging.redis.stream_hash", redisLifecycleSubjectHash(r.provider.streamKey(row.Subject)))))
	}
	r.metrics.prefetched.Add(int64(len(rows)))
	defer r.metrics.prefetched.Add(-int64(len(rows)))
	r.metrics.scheduled.Add(int64(len(rows)))
	if err := func() (err error) {
		handlerCtx, span := otel.Tracer("github.com/digitalwayhk/core/mq").Start(ctx, "redis.owned.handler")
		started := time.Now()
		batch.AddEvent("handler.start", trace.WithTimestamp(started))
		r.metrics.scheduleWaitNanos.Add(started.Sub(admitted).Nanoseconds())
		r.metrics.handlerCalls.Add(1)
		active := r.metrics.inflight.Add(1)
		for old := r.metrics.peak.Load(); active > old && !r.metrics.peak.CompareAndSwap(old, active); old = r.metrics.peak.Load() {
		}
		defer func() {
			r.metrics.inflight.Add(-1)
			r.metrics.handlerNanos.Add(time.Since(started).Nanoseconds())
			if err != nil {
				span.SetStatus(codes.Error, "handler_failed_or_unknown")
			}
			span.End()
			batch.AddEvent("handler.end")
		}()
		return r.handler(handlerCtx, c, rows)
	}(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	ackCtx, ackSpan := otel.Tracer("github.com/digitalwayhk/core/mq").Start(ctx, "redis.owned.ack")
	started := time.Now()
	err := r.ack(ackCtx, c, rows)
	r.metrics.ackNanos.Add(time.Since(started).Nanoseconds())
	if err != nil {
		ackSpan.SetStatus(codes.Error, "ack_failed_or_unknown")
	}
	ackSpan.End()
	return err
}
