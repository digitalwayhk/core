package mq

import (
	"context"
	"fmt"
	"strings"
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
	wg     sync.WaitGroup
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
	r.wg.Wait()
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

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
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

// SubscribeReliable 使用调用方提供的逻辑服务组消费。处理失败的消息保持 pending，
// 并由同组存活消费者在 MinIdle 后重新认领。
func (r *RedisStreamProvider) SubscribeReliable(
	ctx context.Context,
	subject string,
	options ReliableSubscribeOptions,
	handler func(*Message) error,
) (func(), error) {
	if r.client == nil {
		return nil, ErrNotConnected
	}
	if options.Group == "" || handler == nil {
		return nil, fmt.Errorf("redis-stream: reliable group and handler are required")
	}
	if options.Consumer == "" {
		options.Consumer = fmt.Sprintf("consumer-%d", time.Now().UnixNano())
	}
	if options.MinIdle <= 0 {
		options.MinIdle = 30 * time.Second
	}
	if options.ClaimInterval <= 0 {
		options.ClaimInterval = options.MinIdle / 2
	}
	if options.ClaimInterval < 50*time.Millisecond {
		options.ClaimInterval = 50 * time.Millisecond
	}
	if options.Count <= 0 {
		options.Count = 10
	}
	key := r.streamKey(subject)
	err := r.client.XGroupCreateMkStream(ctx, key, options.Group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return nil, fmt.Errorf("redis-stream: create group %s: %w", options.Group, err)
	}
	cctx, cancel := context.WithCancel(ctx)
	subscriptionKey := subject + "|" + options.Group + "|" + options.Consumer
	r.mu.Lock()
	r.subs[subscriptionKey] = cancel
	r.mu.Unlock()
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.runReliableSubscriber(cctx, key, subject, options, handler)
	}()
	return func() {
		cancel()
		r.mu.Lock()
		delete(r.subs, subscriptionKey)
		r.mu.Unlock()
	}, nil
}

func (r *RedisStreamProvider) runReliableSubscriber(
	ctx context.Context,
	key, subject string,
	options ReliableSubscribeOptions,
	handler func(*Message) error,
) {
	ticker := time.NewTicker(options.ClaimInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reclaimPending(ctx, key, subject, options, handler)
		default:
		}
		entries, err := r.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: options.Group, Consumer: options.Consumer,
			Streams: []string{key, ">"}, Count: options.Count, Block: 200 * time.Millisecond,
		}).Result()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		for _, stream := range entries {
			r.handleReliableMessages(ctx, key, subject, options.Group, stream.Messages, handler)
		}
	}
}

func (r *RedisStreamProvider) reclaimPending(
	ctx context.Context,
	key, subject string,
	options ReliableSubscribeOptions,
	handler func(*Message) error,
) {
	start := "0-0"
	for {
		messages, next, err := r.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream: key, Group: options.Group, Consumer: options.Consumer,
			MinIdle: options.MinIdle, Start: start, Count: options.Count,
		}).Result()
		if err != nil || len(messages) == 0 {
			return
		}
		r.handleReliableMessages(ctx, key, subject, options.Group, messages, handler)
		if next == "0-0" || next == start {
			return
		}
		start = next
	}
}

func (r *RedisStreamProvider) handleReliableMessages(
	ctx context.Context,
	key, subject, group string,
	messages []redis.XMessage,
	handler func(*Message) error,
) {
	for _, item := range messages {
		data := redisMessageData(item.Values["data"])
		messageID := item.ID
		message := &Message{ID: messageID, Subject: subject, Data: data}
		message.Ack = func() error { return r.client.XAck(ctx, key, group, messageID).Err() }
		if err := handler(message); err == nil {
			_ = message.Ack()
		}
	}
}

func redisMessageData(value interface{}) []byte {
	switch typed := value.(type) {
	case string:
		return []byte(typed)
	case []byte:
		return append([]byte(nil), typed...)
	default:
		return []byte(fmt.Sprint(typed))
	}
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
