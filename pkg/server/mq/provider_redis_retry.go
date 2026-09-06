package mq

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var redisReliableAckScript = redis.NewScript(`
local retryType = redis.call("TYPE", KEYS[2])["ok"]
if retryType ~= "none" and retryType ~= "hash" then
  return redis.error_reply("RETRY_STATE_WRONG_TYPE")
end
local acked = redis.call("XACK", KEYS[1], ARGV[1], ARGV[2])
redis.call("HDEL", KEYS[2], ARGV[2])
return acked
`)

var redisReliableDeadLetterScript = redis.NewScript(`
local retryType = redis.call("TYPE", KEYS[2])["ok"]
local dedupeType = redis.call("TYPE", KEYS[3])["ok"]
local dlqType = redis.call("TYPE", KEYS[4])["ok"]
if retryType ~= "none" and retryType ~= "hash" then
  return redis.error_reply("RETRY_STATE_WRONG_TYPE")
end
if dedupeType ~= "none" and dedupeType ~= "hash" then
  return redis.error_reply("DLQ_DEDUPE_WRONG_TYPE")
end
if dlqType ~= "none" and dlqType ~= "stream" then
  return redis.error_reply("DLQ_WRONG_TYPE")
end
if redis.call("HEXISTS", KEYS[3], ARGV[3]) == 0 then
  redis.call("XADD", KEYS[4], "*",
    "data", ARGV[4],
    "original_message_id", ARGV[2],
    "original_subject", ARGV[5],
    "consumer_group", ARGV[1],
    "ordering_key", ARGV[6],
    "idempotency_key", ARGV[7],
    "failure_class", "handler_failed")
  redis.call("HSET", KEYS[3], ARGV[3], "1")
end
redis.call("XACK", KEYS[1], ARGV[1], ARGV[2])
redis.call("HDEL", KEYS[2], ARGV[2])
return 1
`)

type redisReliableRetryError struct {
	cause error
	delay time.Duration
}

func (e *redisReliableRetryError) Error() string { return e.cause.Error() }
func (e *redisReliableRetryError) Unwrap() error { return e.cause }

func redisReliableRetryDelay(err error) time.Duration {
	var retry *redisReliableRetryError
	if errors.As(err, &retry) && retry.delay > 0 {
		return retry.delay
	}
	return 50 * time.Millisecond
}

func nextRedisReliableRetry(now time.Time, failures []reliableKeyedFailure) time.Time {
	next := now
	for _, failure := range failures {
		candidate := now.Add(redisReliableRetryDelay(failure.err))
		if candidate.After(next) {
			next = candidate
		}
	}
	return next
}

func redisRetryDelay(policy RetryPolicy, attempt int64) time.Duration {
	if len(policy.Backoff) == 0 {
		return 50 * time.Millisecond
	}
	index := int(attempt) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(policy.Backoff) {
		index = len(policy.Backoff) - 1
	}
	return policy.Backoff[index]
}

func (r *RedisStreamProvider) redisReliableReadCount(
	ctx context.Context,
	streamKey string,
	options ReliableSubscribeOptions,
	requested int64,
) (int64, bool) {
	if requested <= 0 {
		requested = 1
	}
	if options.lifecycle == nil || options.lifecycle.Retry.MaxAckPending <= 0 {
		return requested, true
	}
	pending, err := r.client.XPending(ctx, streamKey, options.Group).Result()
	if err != nil {
		return 0, false
	}
	remaining := int64(options.lifecycle.Retry.MaxAckPending) - pending.Count
	if remaining <= 0 {
		return 0, false
	}
	if requested > remaining {
		requested = remaining
	}
	return requested, true
}

func (r *RedisStreamProvider) processReliableMessage(
	ctx context.Context,
	streamKey, subject string,
	options ReliableSubscribeOptions,
	item redis.XMessage,
	handler func(*Message) error,
) error {
	messageID := item.ID
	data := redisMessageData(item.Values["data"])
	orderingKey := redisMessageData(item.Values["ordering_key"])
	idempotencyKey := redisMessageData(item.Values["idempotency_key"])
	message := &Message{ID: messageID, Subject: subject, Data: data}

	policy := options.lifecycle
	if policy == nil || policy.Retry.MaxDeliveries == 0 {
		message.Ack = func() error {
			return r.client.XAck(ctx, streamKey, options.Group, messageID).Err()
		}
		if err := callReliableHandler(ctx, 0, handler, message); err != nil {
			return err
		}
		if !r.refreshOwner(ctx, subject, options) {
			return errors.New("redis-stream: reliable owner lost before ack")
		}
		return r.client.XAck(ctx, streamKey, options.Group, messageID).Err()
	}

	retryKey := r.redisReliableRetryKey(subject, options.Group)
	attempt, err := r.client.HIncrBy(ctx, retryKey, messageID, 1).Result()
	if err != nil {
		return fmt.Errorf("redis-stream: record delivery attempt: %w", err)
	}
	if attempt > 1 {
		r.lifecycleMetrics.redelivered.Add(1)
	}
	if attempt <= int64(policy.Retry.MaxDeliveries) {
		err = callReliableHandler(ctx, policy.Retry.HandlerTimeout, handler, message)
		if err == nil {
			if !r.refreshOwner(ctx, subject, options) {
				return errors.New("redis-stream: reliable owner lost before ack")
			}
			if ackErr := redisReliableAckScript.Run(ctx, r.client, []string{streamKey, retryKey}, options.Group, messageID).Err(); ackErr != nil {
				return fmt.Errorf("redis-stream: ack successful delivery: %w", ackErr)
			}
			return nil
		}
		if attempt < int64(policy.Retry.MaxDeliveries) {
			return &redisReliableRetryError{cause: err, delay: redisRetryDelay(policy.Retry, attempt)}
		}
	}

	if !r.refreshOwner(ctx, subject, options) {
		return errors.New("redis-stream: reliable owner lost before dead letter")
	}
	dedupeID := redisDeadLetterDedupeID(subject, options.Group, messageID)
	err = redisReliableDeadLetterScript.Run(ctx, r.client, []string{
		streamKey,
		retryKey,
		r.redisReliableDeadLetterDedupeKey(subject, options.Group),
		r.streamKey(policy.Retry.DeadLetterSubject),
	}, options.Group, messageID, dedupeID, data, subject, orderingKey, idempotencyKey).Err()
	if err != nil {
		r.lifecycleMetrics.deadLetterFailed.Add(1)
		return fmt.Errorf("redis-stream: dead letter transfer: %w", err)
	}
	r.lifecycleMetrics.deadLetters.Add(1)
	return nil
}

func callReliableHandler(ctx context.Context, timeout time.Duration, handler func(*Message) error, message *Message) (err error) {
	if timeout <= 0 {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("handler panic: %v", recovered)
			}
		}()
		return handler(message)
	}
	result := make(chan error, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				result <- fmt.Errorf("handler panic: %v", recovered)
			}
		}()
		result <- handler(message)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-result:
		return err
	case <-timer.C:
		return fmt.Errorf("handler timeout after %s", timeout)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *RedisStreamProvider) redisReliableRetryKey(subject, group string) string {
	return r.prefix + ":lifecycle:" + redisLifecycleSubjectHash(subject) + ":retry:" + redisLifecycleGroupHash(group)
}

func (r *RedisStreamProvider) redisReliableDeadLetterDedupeKey(subject, group string) string {
	return r.prefix + ":lifecycle:" + redisLifecycleSubjectHash(subject) + ":dlq:" + redisLifecycleGroupHash(group)
}

func redisLifecycleGroupHash(group string) string {
	digest := sha256.Sum256([]byte(group))
	return hex.EncodeToString(digest[:8])
}

func redisDeadLetterDedupeID(subject, group, messageID string) string {
	digest := sha256.Sum256([]byte(subject + "\x00" + group + "\x00" + messageID))
	return hex.EncodeToString(digest[:])
}
