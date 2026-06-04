package mq

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStreamProvider implements MQProvider using Redis Streams.
// It is the default development-friendly provider requiring only Redis.
type RedisStreamProvider struct {
	addr   string
	db     int
	prefix string
	client *redis.Client
	mu     sync.Mutex
	subs   map[string]context.CancelFunc
}

// NewRedisStreamProvider creates a provider targeting the given Redis address.
// prefix is prepended to all stream keys to namespace keys (e.g. "digitalway-core").
func NewRedisStreamProvider(addr, prefix string, db int) *RedisStreamProvider {
	if prefix == "" {
		prefix = "digitalway-core"
	}
	return &RedisStreamProvider{
		addr:   addr,
		prefix: prefix,
		db:     db,
		subs:   make(map[string]context.CancelFunc),
	}
}

func (r *RedisStreamProvider) Name() string { return "redis-stream" }

// Connect initialises the Redis client. A PING is issued to verify connectivity.
func (r *RedisStreamProvider) Connect(ctx context.Context) error {
	r.client = redis.NewClient(&redis.Options{
		Addr: r.addr,
		DB:   r.db,
	})
	if err := r.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis-stream: connect to %s: %w", r.addr, err)
	}
	return nil
}

// Close unsubscribes all consumers and closes the Redis connection.
func (r *RedisStreamProvider) Close() error {
	r.mu.Lock()
	for _, cancel := range r.subs {
		cancel()
	}
	r.subs = make(map[string]context.CancelFunc)
	r.mu.Unlock()
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}

// Publish appends data to the Redis Stream identified by subject.
func (r *RedisStreamProvider) Publish(ctx context.Context, subject string, data []byte, opts *PublishOptions) error {
	if r.client == nil {
		return ErrNotConnected
	}
	key := r.streamKey(subject)
	values := map[string]interface{}{
		"data": data,
	}
	if opts != nil && opts.IdempotencyKey != "" {
		values["idempotency_key"] = opts.IdempotencyKey
	}
	args := &redis.XAddArgs{
		Stream: key,
		Values: values,
		MaxLen: 0,
	}
	return r.client.XAdd(ctx, args).Err()
}

// Subscribe starts a consumer goroutine that reads from the Redis Stream.
// Each consumer group is named after the subject to allow multiple subscribers.
func (r *RedisStreamProvider) Subscribe(ctx context.Context, subject string, handler func(*Message)) (func(), error) {
	if r.client == nil {
		return nil, ErrNotConnected
	}
	key := r.streamKey(subject)
	group := r.prefix + "-" + subject
	consumer := fmt.Sprintf("consumer-%d", time.Now().UnixNano())

	// Create consumer group if it does not exist.
	_ = r.client.XGroupCreateMkStream(ctx, key, group, "$").Err()

	cctx, cancel := context.WithCancel(ctx)

	r.mu.Lock()
	r.subs[subject] = cancel
	r.mu.Unlock()

	go func() {
		for {
			select {
			case <-cctx.Done():
				return
			default:
			}
			entries, err := r.client.XReadGroup(cctx, &redis.XReadGroupArgs{
				Group:    group,
				Consumer: consumer,
				Streams:  []string{key, ">"},
				Count:    10,
				Block:    2 * time.Second,
			}).Result()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				continue
			}
			for _, stream := range entries {
				for _, msg := range stream.Messages {
					data, _ := msg.Values["data"].(string)
					m := &Message{
						ID:      msg.ID,
						Subject: subject,
						Data:    []byte(data),
						Ack: func() error {
							return r.client.XAck(ctx, key, group, msg.ID).Err()
						},
					}
					handler(m)
				}
			}
		}
	}()
	return cancel, nil
}

// Health verifies the Redis connection is alive.
func (r *RedisStreamProvider) Health(ctx context.Context) error {
	if r.client == nil {
		return ErrNotConnected
	}
	return r.client.Ping(ctx).Err()
}

func (r *RedisStreamProvider) streamKey(subject string) string {
	return r.prefix + ":" + subject
}
