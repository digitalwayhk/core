package mq

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digitalwayhk/core/pkg/server/observability"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

// RedisStreamProvider implements MQProvider using Redis Streams.
// It is the default development-friendly provider requiring only Redis.
type RedisStreamProvider struct {
	addr            string
	db              int
	prefix          string
	client          *redis.Client
	mu              sync.Mutex
	subs            map[string]context.CancelFunc
	reliableMetrics map[string]*redisReliableSubscriptionMetrics
	wg              sync.WaitGroup
}

type redisReliableSubscriptionMetrics struct {
	configured  int64
	inflight    atomic.Int64
	peak        atomic.Int64
	activeKeys  atomic.Int64
	blockedKeys atomic.Int64
	pendingKeys atomic.Int64
}

// NewRedisStreamProvider creates a provider targeting the given Redis address.
// prefix is prepended to all stream keys to namespace keys (e.g. "digitalway-core").
func NewRedisStreamProvider(addr, prefix string, db int) *RedisStreamProvider {
	if prefix == "" {
		prefix = "digitalway-core"
	}
	return &RedisStreamProvider{
		addr:            addr,
		prefix:          prefix,
		db:              db,
		subs:            make(map[string]context.CancelFunc),
		reliableMetrics: make(map[string]*redisReliableSubscriptionMetrics),
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
// 当前实现以整条 stream 串行 + 失败阻断满足契约（比“仅同 key 阻断”更严）；
// 不同 OrderingKey 的并行优化可后续用 shard stream 加强，不削弱本声明。
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
	if options.lifecycle != nil {
		policy := options.lifecycle.Normalize()
		if err := policy.Validate(); err != nil {
			return nil, err
		}
		if policy.Subject != subject {
			return nil, fmt.Errorf("%w: reliable subject %q does not match lifecycle subject %q", ErrLifecyclePolicyInvalid, subject, policy.Subject)
		}
		declared := false
		for _, group := range policy.RequiredGroups {
			if group.Name == options.Group {
				declared = true
				break
			}
		}
		if !declared {
			return nil, fmt.Errorf("%w: subject %q group %q", ErrLifecycleRequiredGroupMismatch, subject, options.Group)
		}
		options.lifecycle = &policy
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
	// 零值/1 保持历史整 subject 串行。只有显式 opt-in 才进入 owner 内分键调度。
	if options.KeyConcurrency <= 1 {
		options.KeyConcurrency = 1
		options.Count = 1
	} else if options.Count <= 0 {
		options.Count = int64(options.KeyConcurrency * 4)
	}
	key := r.streamKey(subject)
	err := r.client.XGroupCreateMkStream(ctx, key, options.Group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return nil, fmt.Errorf("redis-stream: create group %s: %w", options.Group, err)
	}
	cctx, cancel := context.WithCancel(ctx)
	subscriptionKey := subject + "|" + options.Group + "|" + options.Consumer
	metrics := &redisReliableSubscriptionMetrics{configured: int64(options.KeyConcurrency)}
	observedHandler := func(message *Message) error {
		current := metrics.inflight.Add(1)
		for {
			peak := metrics.peak.Load()
			if current <= peak || metrics.peak.CompareAndSwap(peak, current) {
				break
			}
		}
		defer metrics.inflight.Add(-1)
		return handler(message)
	}
	r.mu.Lock()
	r.subs[subscriptionKey] = cancel
	r.reliableMetrics[subscriptionKey] = metrics
	r.mu.Unlock()
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		if options.KeyConcurrency > 1 {
			r.runReliableKeyedSubscriber(cctx, key, subject, options, observedHandler, metrics)
			return
		}
		r.runReliableSubscriber(cctx, key, subject, options, observedHandler)
	}()
	return func() {
		cancel()
		r.mu.Lock()
		delete(r.subs, subscriptionKey)
		delete(r.reliableMetrics, subscriptionKey)
		r.mu.Unlock()
	}, nil
}

// SupportsKeyedReliableConcurrency 声明 Redis provider 实现 owner 内分键调度。
func (*RedisStreamProvider) SupportsKeyedReliableConcurrency() bool { return true }

func (r *RedisStreamProvider) ownerLockKey(subject, group string) string {
	return r.prefix + ":ordered-owner:" + group + ":" + subject
}

// ownerLeaseTTL 与 MinIdle 解耦：lease 必须覆盖单条 handler 执行时间，
// 避免 handler 慢于 MinIdle 时旧 owner 仍在处理、新 owner 已开始读新消息导致越序。
// XAutoClaim 仍用 MinIdle 认领崩溃实例的 pending；lease 更长，正常慢 handler 不会丢 owner。
func ownerLeaseTTL(options ReliableSubscribeOptions) time.Duration {
	ttl := 3 * options.MinIdle
	if ttl < 2*time.Minute {
		ttl = 2 * time.Minute
	}
	if options.MinIdle > 0 && ttl < options.MinIdle+30*time.Second {
		ttl = options.MinIdle + 30*time.Second
	}
	return ttl
}

var redisOwnerRefreshScript = redis.NewScript(`
local cur = redis.call("GET", KEYS[1])
if not cur then
  return redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2]) and 1 or 0
end
if cur == ARGV[1] then
  return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0
`)

var redisOwnerReleaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

// 只读 fencing：不续约、不抢占，仅判断当前锁是否仍为本 consumer。
var redisOwnerCheckScript = redis.NewScript(`
local cur = redis.call("GET", KEYS[1])
if cur == ARGV[1] then
  return 1
end
return 0
`)

func (r *RedisStreamProvider) tryAcquireOwner(ctx context.Context, subject string, options ReliableSubscribeOptions) bool {
	ttl := ownerLeaseTTL(options)
	ok, err := r.client.SetNX(ctx, r.ownerLockKey(subject, options.Group), options.Consumer, ttl).Result()
	return err == nil && ok
}

// refreshOwner 原子续约：仅当仍为本 consumer 时 PEXPIRE；锁不存在则尝试抢占。
func (r *RedisStreamProvider) refreshOwner(ctx context.Context, subject string, options ReliableSubscribeOptions) bool {
	lockKey := r.ownerLockKey(subject, options.Group)
	ttlMs := ownerLeaseTTL(options).Milliseconds()
	n, err := redisOwnerRefreshScript.Run(ctx, r.client, []string{lockKey}, options.Consumer, ttlMs).Int()
	if err != nil {
		return false
	}
	return n == 1
}

func (r *RedisStreamProvider) releaseOwner(ctx context.Context, subject string, options ReliableSubscribeOptions) {
	lockKey := r.ownerLockKey(subject, options.Group)
	_ = redisOwnerReleaseScript.Run(ctx, r.client, []string{lockKey}, options.Consumer).Err()
}

// stillOwner 只读 fencing：已丢 owner 则不得 ACK。不续约、不抢占，避免与 refresh 语义混淆。
func (r *RedisStreamProvider) stillOwner(ctx context.Context, subject string, options ReliableSubscribeOptions) bool {
	lockKey := r.ownerLockKey(subject, options.Group)
	n, err := redisOwnerCheckScript.Run(ctx, r.client, []string{lockKey}, options.Consumer).Int()
	if err != nil {
		return false
	}
	return n == 1
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
				// 周期认领：MinIdle 控制，避免抢仍活跃的 in-flight。
				if delay := r.reclaimPending(ctx, key, subject, options, handler, options.MinIdle); delay > 0 {
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
		// 1) 先排空本 consumer 的 pending。
		if delay := r.processOwnPending(ctx, key, subject, options, handler); delay > 0 {
			// 仅失败时 backoff；成功排空 pending 不睡，避免 N×50ms 延迟税。
			if !r.stillOwner(ctx, subject, options) {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			continue
		}
		// 2) 仅仍为 owner 时 reclaim：防止 displaced owner 用 MinIdle=0 窃取新 owner 的 pending。
		if !r.stillOwner(ctx, subject, options) {
			continue
		}
		// 3) 接管后必须先回收其他 consumer 的 pending（MinIdle=0），再读新消息，
		//    避免 failover 时 > 越过旧 owner 未 ACK 的消息导致越序。
		if delay := r.reclaimPending(ctx, key, subject, options, handler, 0); delay > 0 {
			if !r.stillOwner(ctx, subject, options) {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			continue
		}
		// 4) 读新消息前再次确认 owner。
		if !r.stillOwner(ctx, subject, options) {
			continue
		}
		readCount, allowed := r.redisReliableReadCount(ctx, key, options, 1)
		if !allowed {
			select {
			case <-ctx.Done():
				return
			case <-time.After(options.ClaimInterval):
			}
			continue
		}
		entries, err := r.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: options.Group, Consumer: options.Consumer,
			Streams: []string{key, ">"}, Count: readCount, Block: 200 * time.Millisecond,
		}).Result()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		for _, stream := range entries {
			if delay := r.handleReliableMessages(ctx, key, subject, options, stream.Messages, handler); delay > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}
			}
		}
	}
}

// runReliableKeyedSubscriber 保持单 active owner，只在 owner 内并行不同 ordering key。
// pendingKeys 来自最近一轮完整 PEL 扫描：仍有旧 pending 的 key 不读取后到消息，
// 但其他新 key 不必等待 poison key 排空。
func (r *RedisStreamProvider) runReliableKeyedSubscriber(
	ctx context.Context,
	key, subject string,
	options ReliableSubscribeOptions,
	handler func(*Message) error,
	metrics *redisReliableSubscriptionMetrics,
) {
	defer r.releaseOwner(context.Background(), subject, options)
	blocked := make(map[string]reliableKeyedWork)
	pendingKeys := make(map[string]struct{})
	seenPendingKeys := make(map[string]struct{})
	claimCursor := "0-0"
	initialPendingScanComplete := false
	nextBlockedRetry := time.Time{}
	window := options.Count
	if window < int64(options.KeyConcurrency) {
		window = int64(options.KeyConcurrency)
	}
	if window <= 0 {
		window = 1
	}

	for {
		if ctx.Err() != nil {
			return
		}
		if !r.refreshOwner(ctx, subject, options) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
			continue
		}

		// 每轮先重试各 blocked key 的最早失败消息；成功后该 key 才可能继续。
		if len(blocked) > 0 && !time.Now().Before(nextBlockedRetry) {
			retries := make([]reliableKeyedWork, 0, len(blocked))
			for _, work := range blocked {
				retries = append(retries, work)
			}
			sort.Slice(retries, func(i, j int) bool { return retries[i].id < retries[j].id })
			failures := runReliableKeyedBatch(ctx, options.KeyConcurrency, retries)
			blocked = make(map[string]reliableKeyedWork, len(failures))
			for _, failure := range failures {
				blocked[failure.work.key] = failure.work
				r.logReliableKeyedFailure(subject, failure)
			}
			if len(blocked) > 0 {
				nextBlockedRetry = nextRedisReliableRetry(time.Now(), failures)
			}
			metrics.blockedKeys.Store(int64(len(blocked)))
		}

		// PEL 用 XAUTOCLAIM cursor 做有界分页；不要求先 ACK/排空整个 PEL。
		pending, next, err := r.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream: key, Group: options.Group, Consumer: options.Consumer,
			MinIdle: 0, Start: claimCursor, Count: window,
		}).Result()
		if err != nil && ctx.Err() != nil {
			return
		}
		if err == nil {
			works := r.reliableKeyedWorks(ctx, key, subject, options, pending, handler)
			for _, work := range works {
				seenPendingKeys[work.key] = struct{}{}
			}
			admitted := admitReliableKeyedWork(works, blocked, 1)
			metrics.activeKeys.Store(int64(len(admitted)))
			failures := runReliableKeyedBatch(ctx, options.KeyConcurrency, admitted)
			metrics.activeKeys.Store(0)
			for _, failure := range failures {
				blocked[failure.work.key] = failure.work
				r.logReliableKeyedFailure(subject, failure)
			}
			if len(failures) > 0 {
				nextBlockedRetry = nextRedisReliableRetry(time.Now(), failures)
			}
			claimCursor = next
			if next == "" || next == "0-0" {
				pendingKeys = seenPendingKeys
				seenPendingKeys = make(map[string]struct{})
				for blockedKey := range blocked {
					pendingKeys[blockedKey] = struct{}{}
				}
				claimCursor = "0-0"
				initialPendingScanComplete = true
				metrics.pendingKeys.Store(int64(len(pendingKeys)))
			}
		}

		if !initialPendingScanComplete || !r.stillOwner(ctx, subject, options) {
			continue
		}

		readCount, allowed := r.redisReliableReadCount(ctx, key, options, window)
		if !allowed {
			select {
			case <-ctx.Done():
				return
			case <-time.After(options.ClaimInterval):
			}
			continue
		}
		entries, err := r.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: options.Group, Consumer: options.Consumer,
			Streams: []string{key, ">"}, Count: readCount, Block: 100 * time.Millisecond,
		}).Result()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		for _, stream := range entries {
			works := r.reliableKeyedWorks(ctx, key, subject, options, stream.Messages, handler)
			eligible := make([]reliableKeyedWork, 0, len(works))
			counts := make(map[string]int)
			for _, work := range works {
				counts[work.key]++
				if _, pending := pendingKeys[work.key]; pending {
					continue
				}
				if _, failed := blocked[work.key]; failed {
					continue
				}
				eligible = append(eligible, work)
			}
			admitted := admitReliableKeyedWork(eligible, blocked, 1)
			metrics.activeKeys.Store(int64(len(admitted)))
			for workKey, count := range counts {
				if count > 1 || len(admitted) == 0 {
					pendingKeys[workKey] = struct{}{}
				}
			}
			failures := runReliableKeyedBatch(ctx, options.KeyConcurrency, admitted)
			metrics.activeKeys.Store(0)
			for _, failure := range failures {
				blocked[failure.work.key] = failure.work
				pendingKeys[failure.work.key] = struct{}{}
				r.logReliableKeyedFailure(subject, failure)
			}
			if len(failures) > 0 {
				nextBlockedRetry = nextRedisReliableRetry(time.Now(), failures)
			}
			metrics.blockedKeys.Store(int64(len(blocked)))
			metrics.pendingKeys.Store(int64(len(pendingKeys)))
		}
	}
}

func (*RedisStreamProvider) ComponentName() string { return "mq" }

func (r *RedisStreamProvider) RuntimeMetricSnapshot(context.Context) observability.RuntimeComponentSnapshot {
	if r == nil || r.client == nil {
		return observability.RuntimeComponentSnapshot{Component: "mq", State: "unavailable"}
	}
	r.mu.Lock()
	metrics := make([]*redisReliableSubscriptionMetrics, 0, len(r.reliableMetrics))
	for _, item := range r.reliableMetrics {
		metrics = append(metrics, item)
	}
	r.mu.Unlock()
	gauges := map[string]float64{
		"key_concurrency": 0, "handler_inflight": 0, "handler_peak": 0,
		"active_keys": 0, "blocked_keys": 0, "pending_keys": 0,
	}
	for _, item := range metrics {
		gauges["key_concurrency"] += float64(item.configured)
		gauges["handler_inflight"] += float64(item.inflight.Load())
		gauges["handler_peak"] += float64(item.peak.Load())
		gauges["active_keys"] += float64(item.activeKeys.Load())
		gauges["blocked_keys"] += float64(item.blockedKeys.Load())
		gauges["pending_keys"] += float64(item.pendingKeys.Load())
	}
	return observability.RuntimeComponentSnapshot{Component: "mq", State: "ok", Gauges: gauges}
}

func (r *RedisStreamProvider) reliableKeyedWorks(
	ctx context.Context,
	streamKey, subject string,
	options ReliableSubscribeOptions,
	messages []redis.XMessage,
	handler func(*Message) error,
) []reliableKeyedWork {
	works := make([]reliableKeyedWork, 0, len(messages))
	for _, item := range messages {
		item := item
		orderingKey := string(redisMessageData(item.Values["ordering_key"]))
		workKey := orderingKey
		if workKey == "" {
			workKey = "__missing_ordering_key__"
		}
		works = append(works, reliableKeyedWork{
			key: workKey,
			id:  item.ID,
			run: func() error {
				if orderingKey == "" {
					return ErrOrderingKeyRequired
				}
				return r.processReliableMessage(ctx, streamKey, subject, options, item, handler)
			},
		})
	}
	return works
}

func (r *RedisStreamProvider) logReliableKeyedFailure(subject string, failure reliableKeyedFailure) {
	logx.Errorw("mq_redis_reliable_keyed_handler_failed",
		logx.Field("subject", subject),
		logx.Field("error", failure.err),
	)
}

// processOwnPending 处理当前 consumer 的 PEL。返回正数表示失败后的下一次重试等待。
func (r *RedisStreamProvider) processOwnPending(
	ctx context.Context,
	key, subject string,
	options ReliableSubscribeOptions,
	handler func(*Message) error,
) time.Duration {
	// 连续排空 pending；每条成功后在 handle 内 refreshOwner 续约，避免长 drain 丢 lease。
	for {
		entries, err := r.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: options.Group, Consumer: options.Consumer,
			Streams: []string{key, "0"}, Count: 1, Block: 0,
		}).Result()
		if err != nil || len(entries) == 0 {
			return 0
		}
		has := false
		for _, stream := range entries {
			if len(stream.Messages) == 0 {
				continue
			}
			has = true
			if delay := r.handleReliableMessages(ctx, key, subject, options, stream.Messages, handler); delay > 0 {
				return delay
			}
		}
		if !has {
			return 0
		}
	}
}

// reclaimPending 认领其他 consumer 的 pending。
// minIdle=0：接管后立即回收，防止 > 越过旧 owner 未 ACK 消息；
// minIdle=options.MinIdle：周期回收，给崩溃实例一点宽限。
// 返回正数表示处理失败后的下一次重试等待。
func (r *RedisStreamProvider) reclaimPending(
	ctx context.Context,
	key, subject string,
	options ReliableSubscribeOptions,
	handler func(*Message) error,
	minIdle time.Duration,
) time.Duration {
	start := "0-0"
	for {
		// 长 reclaim 期间保持 lease，避免被 standby 抢占后仍继续 XAutoClaim。
		if !r.refreshOwner(ctx, subject, options) {
			return 50 * time.Millisecond
		}
		messages, next, err := r.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream: key, Group: options.Group, Consumer: options.Consumer,
			MinIdle: minIdle, Start: start, Count: 1,
		}).Result()
		if err != nil || len(messages) == 0 {
			return 0
		}
		if delay := r.handleReliableMessages(ctx, key, subject, options, messages, handler); delay > 0 {
			return delay
		}
		if next == "0-0" || next == start {
			return 0
		}
		start = next
	}
}

// handleReliableMessages 按序处理；任一条失败则停止后续（失败屏障），并返回重试等待。
// handler 成功后：refreshOwner 续约并 fencing；已丢 owner 则不 ACK，留给新 owner 重投。
func (r *RedisStreamProvider) handleReliableMessages(
	ctx context.Context,
	key, subject string,
	options ReliableSubscribeOptions,
	messages []redis.XMessage,
	handler func(*Message) error,
) time.Duration {
	for _, item := range messages {
		if err := r.processReliableMessage(ctx, key, subject, options, item, handler); err != nil {
			logx.Errorw("mq_redis_reliable_handler_failed",
				logx.Field("subject", subject),
				logx.Field("error", err),
			)
			return redisReliableRetryDelay(err)
		}
	}
	return 0
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
