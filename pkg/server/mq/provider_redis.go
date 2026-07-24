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

// OrderedReliableInfo 声明 Redis Streams 在单 active owner 下的 ordered-reliable 能力。
func (r *RedisStreamProvider) OrderedReliableInfo() OrderedReliableCapability {
	return DefaultOrderedReliableCapability()
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
	if opts != nil && opts.OrderingKey != "" {
		values["ordering_key"] = opts.OrderingKey
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
	// ordered-reliable：单条处理，失败阻断同 stream 后续消息；多实例靠 owner lease 保证单 active。
	if options.Count <= 0 {
		options.Count = 1
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

func (r *RedisStreamProvider) ownerLockKey(subject, group string) string {
	return r.prefix + ":ordered-owner:" + group + ":" + subject
}

func (r *RedisStreamProvider) tryAcquireOwner(ctx context.Context, subject string, options ReliableSubscribeOptions) bool {
	// TTL 略长于 claim 周期，持有者需周期性续约；崩溃后由其他实例接管。
	ttl := options.MinIdle
	if ttl < 2*time.Second {
		ttl = 2 * time.Second
	}
	ok, err := r.client.SetNX(ctx, r.ownerLockKey(subject, options.Group), options.Consumer, ttl).Result()
	return err == nil && ok
}

func (r *RedisStreamProvider) refreshOwner(ctx context.Context, subject string, options ReliableSubscribeOptions) bool {
	lockKey := r.ownerLockKey(subject, options.Group)
	val, err := r.client.Get(ctx, lockKey).Result()
	if err != nil {
		return r.tryAcquireOwner(ctx, subject, options)
	}
	if val != options.Consumer {
		return false
	}
	ttl := options.MinIdle
	if ttl < 2*time.Second {
		ttl = 2 * time.Second
	}
	_ = r.client.Expire(ctx, lockKey, ttl).Err()
	return true
}

func (r *RedisStreamProvider) releaseOwner(ctx context.Context, subject string, options ReliableSubscribeOptions) {
	lockKey := r.ownerLockKey(subject, options.Group)
	val, err := r.client.Get(ctx, lockKey).Result()
	if err == nil && val == options.Consumer {
		_ = r.client.Del(ctx, lockKey).Err()
	}
}

func (r *RedisStreamProvider) runReliableSubscriber(
	ctx context.Context,
	key, subject string,
	options ReliableSubscribeOptions,
	handler func(*Message) error,
) {
	defer r.releaseOwner(context.Background(), subject, options)
	ticker := time.NewTicker(options.ClaimInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if r.refreshOwner(ctx, subject, options) {
				// 仅 owner 认领超时 pending，避免多 active 并行越序。
				if blocked := r.reclaimPending(ctx, key, subject, options, handler); blocked {
					continue
				}
			}
		default:
		}
		if !r.refreshOwner(ctx, subject, options) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
			continue
		}
		// 先排空本 consumer 的 pending，失败则不读新消息（同 key / 同 stream 失败屏障）。
		if blocked := r.processOwnPending(ctx, key, subject, options, handler); blocked {
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
			continue
		}
		entries, err := r.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: options.Group, Consumer: options.Consumer,
			Streams: []string{key, ">"}, Count: 1, Block: 200 * time.Millisecond,
		}).Result()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		for _, stream := range entries {
			_ = r.handleReliableMessages(ctx, key, subject, options.Group, stream.Messages, handler)
		}
	}
}

// processOwnPending 处理当前 consumer 的 PEL。返回 true 表示存在未成功消息，应阻断新消息。
func (r *RedisStreamProvider) processOwnPending(
	ctx context.Context,
	key, subject string,
	options ReliableSubscribeOptions,
	handler func(*Message) error,
) bool {
	entries, err := r.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: options.Group, Consumer: options.Consumer,
		Streams: []string{key, "0"}, Count: 1, Block: 0,
	}).Result()
	if err != nil || len(entries) == 0 {
		return false
	}
	for _, stream := range entries {
		if len(stream.Messages) == 0 {
			return false
		}
		if blocked := r.handleReliableMessages(ctx, key, subject, options.Group, stream.Messages, handler); blocked {
			return true
		}
		// 还有 pending 时继续由上层循环拉取。
		return true
	}
	return false
}

// reclaimPending 认领超时 pending。返回 true 表示处理失败应阻断。
func (r *RedisStreamProvider) reclaimPending(
	ctx context.Context,
	key, subject string,
	options ReliableSubscribeOptions,
	handler func(*Message) error,
) bool {
	start := "0-0"
	for {
		messages, next, err := r.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream: key, Group: options.Group, Consumer: options.Consumer,
			MinIdle: options.MinIdle, Start: start, Count: 1,
		}).Result()
		if err != nil || len(messages) == 0 {
			return false
		}
		if blocked := r.handleReliableMessages(ctx, key, subject, options.Group, messages, handler); blocked {
			return true
		}
		if next == "0-0" || next == start {
			return false
		}
		start = next
	}
}

// handleReliableMessages 按序处理；任一条失败则停止后续（失败屏障），返回 true。
func (r *RedisStreamProvider) handleReliableMessages(
	ctx context.Context,
	key, subject, group string,
	messages []redis.XMessage,
	handler func(*Message) error,
) bool {
	for _, item := range messages {
		data := redisMessageData(item.Values["data"])
		messageID := item.ID
		message := &Message{ID: messageID, Subject: subject, Data: data}
		message.Ack = func() error { return r.client.XAck(ctx, key, group, messageID).Err() }
		if err := handler(message); err != nil {
			return true
		}
		_ = message.Ack()
	}
	return false
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
