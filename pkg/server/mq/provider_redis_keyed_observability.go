package mq

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func observeRedisKeyedRead(ctx context.Context, startedAt, readAt time.Time, source, streamKey, group string, messages []redis.XMessage) {
	if len(messages) == 0 {
		return
	}
	_, span := otel.Tracer("github.com/digitalwayhk/core/mq").Start(ctx, "redis.reliable.read",
		trace.WithTimestamp(startedAt), trace.WithAttributes(attribute.String("messaging.redis.read_source", source),
			attribute.String("messaging.redis.stream_hash", redisLifecycleSubjectHash(streamKey)),
			attribute.String("messaging.redis.group_hash", redisLifecycleGroupHash(group))))
	defer span.End(trace.WithTimestamp(readAt))
	for _, message := range messages {
		span.AddEvent("redis.read", trace.WithTimestamp(readAt), trace.WithAttributes(
			attribute.String("messaging.message.id", message.ID),
			attribute.Int64("messaging.redis.broker_age_ms", redisMessageAge(message.ID, readAt).Milliseconds()),
		))
	}
}

func redisMessageAge(id string, now time.Time) time.Duration {
	ms, err := strconv.ParseInt(strings.SplitN(id, "-", 2)[0], 10, 64)
	if err != nil || ms <= 0 {
		return 0
	}
	age := now.Sub(time.UnixMilli(ms))
	if age < 0 {
		return 0
	}
	return age
}

func (r *RedisStreamProvider) observeRedisKeyedWork(
	ctx context.Context, work reliableKeyedWork, streamKey, group string, firstRead, claimedAt time.Time,
	fresh bool, metrics *redisReliableSubscriptionMetrics,
) reliableKeyedWork {
	run := work.run
	admittedAt := time.Now()
	queuedAt := admittedAt
	if fresh {
		metrics.pelWaitNanos.Add(claimedAt.Sub(firstRead).Nanoseconds())
		metrics.pelWaitSamples.Add(1)
	}
	work.run = func() error {
		metrics.scheduled.Add(1)
		startedAt := time.Now()
		metrics.scheduleWaitNanos.Add(startedAt.Sub(queuedAt).Nanoseconds())
		defer func() { queuedAt = time.Now() }()
		source := "reclaim"
		if fresh {
			source = "new"
		}
		spanStart := claimedAt
		if fresh {
			spanStart = firstRead
		}
		_, span := otel.Tracer("github.com/digitalwayhk/core/mq").Start(ctx, "redis.reliable.delivery",
			trace.WithTimestamp(spanStart), trace.WithAttributes(
				attribute.String("messaging.message.id", work.id),
				attribute.String("messaging.redis.stream_hash", redisLifecycleSubjectHash(streamKey)),
				attribute.String("messaging.redis.group_hash", redisLifecycleGroupHash(group)),
				attribute.String("messaging.redis.read_source", source),
				attribute.Bool("messaging.redis.first_read_known", fresh),
			))
		defer span.End()
		if fresh {
			span.AddEvent("redis.read", trace.WithTimestamp(firstRead))
		}
		span.AddEvent("redis.claim", trace.WithTimestamp(claimedAt))
		span.AddEvent("scheduler.admit", trace.WithTimestamp(admittedAt))
		span.AddEvent("scheduler.wait", trace.WithTimestamp(queuedAt))
		span.AddEvent("delivery.start", trace.WithTimestamp(startedAt))
		// processReliableMessage 内的观测 handler 区分业务耗时与 ACK/重试耗时。
		err := run()
		if err != nil {
			// 不将业务错误正文写入 span，可能含 payload、DSN 或用户信息。
			span.SetStatus(codes.Error, "handler_or_delivery_failed")
		}
		span.AddEvent("delivery.end")
		return err
	}
	return work
}
