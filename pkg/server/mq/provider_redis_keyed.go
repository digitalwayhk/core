package mq

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// owner 校验与 PEL 认领/新消息读取必须在同一 Redis 命令内完成。
const redisKeyedClaimScript = `
if redis.call("GET", KEYS[2]) ~= ARGV[2] then
  return redis.error_reply("RELIABLE_OWNER_LOST")
end
return redis.call("XAUTOCLAIM", KEYS[1], ARGV[1], ARGV[2], 0, ARGV[3], "COUNT", ARGV[4])
`

const redisKeyedReadScript = `
if redis.call("GET", KEYS[2]) ~= ARGV[2] then
  return redis.error_reply("RELIABLE_OWNER_LOST")
end
return redis.call("XREADGROUP", "GROUP", ARGV[1], ARGV[2], "COUNT", ARGV[3], "STREAMS", KEYS[1], ">")
`

func (r *RedisStreamProvider) claimReliableKeyed(ctx context.Context, key, subject string, options ReliableSubscribeOptions, cursor string, count int64) ([]redis.XMessage, string, error) {
	script := redisKeyedClaimScript
	args := []interface{}{options.Group, options.Consumer, cursor, count}
	if options.SharedKeys {
		script = redisSharedPendingScript
		args = []interface{}{options.Group, cursor, count, redisSharedMarker(options)}
		if options.scopeMarker != "" {
			script = redisOwnedSizeBoundScript(script, true)
			args = append(args, options.scopeMessageLimit)
		}
	}
	command := []interface{}{"eval", script, 2, key, r.ownerLockKey(subject, options.Group)}
	cmd := redis.NewXAutoClaimCmd(ctx, append(command, args...)...)
	_ = r.client.Process(ctx, cmd)
	return cmd.Result()
}

func (r *RedisStreamProvider) readReliableKeyed(ctx context.Context, key, subject string, options ReliableSubscribeOptions, count int64) ([]redis.XStream, error) {
	script := redisKeyedReadScript
	args := []interface{}{options.Group, options.Consumer, count}
	if options.SharedKeys {
		script = redisSharedReadScript
		args = append(args, redisSharedMarker(options), sharedRedisWindow(options))
		if options.scopeMarker != "" {
			script = redisOwnedSizeBoundScript(script, false)
			args = append(args, options.scopeMessageLimit)
		}
	}
	command := []interface{}{"eval", script, 2, key, r.ownerLockKey(subject, options.Group)}
	cmd := redis.NewXStreamSliceCmd(ctx, append(command, args...)...)
	_ = r.client.Process(ctx, cmd)
	return cmd.Result()
}

type redisKeyedTask struct {
	work     reliableKeyedWork
	running  bool
	retryAt  time.Time
	sequence uint64
}

// runReliableKeyedSubscriber 的 worker 生命周期属于一个 owner 会话，而非读取批次。
// 每 key 只保留最早未完成消息，其余消息留在 PEL；工作和失败屏障均受 window 限制。
func (r *RedisStreamProvider) runReliableKeyedSubscriber(
	ctx context.Context, key, subject string, options ReliableSubscribeOptions,
	handler func(*Message) error, metrics *redisReliableSubscriptionMetrics,
) {
	if options.SharedKeys {
		r.runReliableKeyedOwner(ctx, key, subject, options, handler, metrics)
		return
	}
	defer r.releaseOwner(context.Background(), subject, options)
	for ctx.Err() == nil {
		if r.refreshOwner(ctx, subject, options) {
			r.runReliableKeyedOwner(ctx, key, subject, options, handler, metrics)
		}
		select {
		case <-ctx.Done():
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (r *RedisStreamProvider) runReliableKeyedOwner(
	parent context.Context, key, subject string, options ReliableSubscribeOptions,
	handler func(*Message) error, metrics *redisReliableSubscriptionMetrics,
) {
	ctx, cancel := context.WithCancel(parent)
	if metrics.redisReliableDeliveryMetrics == nil {
		metrics.redisReliableDeliveryMetrics = &r.deliveryMetrics
	}
	window := options.Count
	if window < int64(options.KeyConcurrency) {
		window = int64(options.KeyConcurrency)
	}
	if options.SharedKeys {
		window = sharedRedisWindow(options)
	}
	metrics.prefetchCapacity.Store(window)
	jobs := make(chan reliableKeyedWork)
	results := make(chan reliableKeyedFailure, options.KeyConcurrency)
	var workers sync.WaitGroup
	for i := 0; i < options.KeyConcurrency; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case work := <-jobs:
					if ctx.Err() != nil {
						return
					}
					err := work.run()
					select {
					case results <- reliableKeyedFailure{work: work, err: err}:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	defer func() {
		cancel()
		// 无法强制终止不合作的业务 handler；不得遗弃它再重投同 key。
		workers.Wait()
		metrics.activeKeys.Store(0)
		metrics.blockedKeys.Store(0)
		metrics.pendingKeys.Store(0)
		metrics.queuedWork.Store(0)
	}()
	tasks := make(map[string]*redisKeyedTask)
	readTimes := make(map[string]time.Time)
	cursor := "0-0"
	running := 0
	var sequence uint64
	complete := func(result reliableKeyedFailure) {
		task := tasks[result.work.key]
		running--
		task.running = false
		if result.err == nil {
			delete(tasks, result.work.key)
			// 此前扫过但因该 key 正在执行而跳过的旧消息必须先于后页消息。
			cursor = "0-0"
			return
		}
		task.retryAt = time.Now().Add(redisReliableRetryDelay(result.err))
		r.logReliableKeyedFailure(subject, result)
	}
	for ctx.Err() == nil {
		for draining := true; draining; {
			select {
			case result := <-results:
				complete(result)
			default:
				draining = false
			}
		}
		// 会话内只续约，绝不因租约丢失而重新占用并确认旧工作。
		if !options.SharedKeys && !r.renewOwner(ctx, subject, options) {
			return
		}
		blocked := int64(0)
		ready := make([]*redisKeyedTask, 0, len(tasks))
		delay := 100 * time.Millisecond
		for _, task := range tasks {
			if !task.retryAt.IsZero() {
				blocked++
			}
			if remaining := time.Until(task.retryAt); !task.running && remaining > 0 && remaining < delay {
				delay = remaining
			}
			if !task.running && !time.Now().Before(task.retryAt) {
				ready = append(ready, task)
			}
		}
		// 已准入的 key 按准入顺序竞争空闲 worker，避免 map 遍历造成饥饿。
		sort.Slice(ready, func(i, j int) bool { return ready[i].sequence < ready[j].sequence })
		for _, task := range ready {
			if running < options.KeyConcurrency {
				if !task.retryAt.IsZero() {
					// ACK 可能已执行但响应丢失。只有仍在 PEL 中的工作才能重投。
					pending, err := r.client.XPendingExt(ctx, &redis.XPendingExtArgs{
						Stream: key, Group: options.Group, Start: task.work.id, End: task.work.id, Count: 1,
					}).Result()
					if err != nil {
						continue
					}
					if len(pending) == 0 {
						delete(tasks, task.work.key)
						cursor = "0-0"
						continue
					}
				}
				select {
				case jobs <- task.work:
					task.running = true
					running++
				case <-ctx.Done():
					return
				}
			}
		}
		metrics.activeKeys.Store(int64(running))
		metrics.pendingKeys.Store(int64(len(tasks)))
		metrics.blockedKeys.Store(blocked)
		metrics.queuedWork.Store(int64(len(tasks) - running))
		morePending := false
		admitted := false
		if int64(len(tasks)) < window {
			startedAt := time.Now()
			pending, next, err := r.claimReliableKeyed(ctx, key, subject, options, cursor, window)
			readAt := time.Now()
			if err != nil {
				if !waitRedisKeyed(ctx, results, complete, delay) {
					return
				}
				continue
			}
			metrics.reclaimRead.Add(int64(len(pending)))
			observeRedisKeyedRead(ctx, startedAt, readAt, "reclaim", key, options.Group, pending)
			for _, work := range r.reliableKeyedWorks(ctx, key, subject, options, pending, handler) {
				if _, exists := tasks[work.key]; exists {
					continue
				}
				if int64(len(tasks)) == window {
					// 没有准入的消息不能因推进 cursor 而被同 key 后页越过。
					next = "0-0"
					break
				}
				firstRead, fresh := readTimes[work.id]
				delete(readTimes, work.id)
				work = r.observeRedisKeyedWork(ctx, work, key, options.Group, firstRead, readAt, fresh, metrics)
				sequence++
				tasks[work.key] = &redisKeyedTask{work: work, sequence: sequence}
				admitted = true
			}
			cursor = next
			if cursor == "" {
				cursor = "0-0"
			}
			morePending = cursor != "0-0"
		}

		// PEL 与新读取每轮各最多一页。预取上限同时约束本 owner 新增的 PEL，
		// 接管时已存在的超限 PEL 只分页恢复，不继续扩大。
		pending, err := r.client.XPending(ctx, key, options.Group).Result()
		if err == nil {
			metrics.prefetched.Store(pending.Count)
			count, allowed := r.redisReliableReadCount(ctx, key, options, window-pending.Count)
			if allowed && pending.Count < window {
				startedAt := time.Now()
				entries, readErr := r.readReliableKeyed(ctx, key, subject, options, count)
				if readErr == nil {
					readAt := time.Now()
					for _, entry := range entries {
						observeRedisKeyedRead(ctx, startedAt, readAt, "new", key, options.Group, entry.Messages)
						for _, message := range entry.Messages {
							if int64(len(readTimes)) < window {
								readTimes[message.ID] = readAt
							}
							metrics.newRead.Add(1)
							metrics.publishToReadNanos.Add(redisMessageAge(message.ID, readAt).Nanoseconds())
						}
					}
					// 新消息也先进入 PEL，只有最早消息可准入，不能绕过旧 pending。
					cursor = "0-0"
					continue
				}
			}
		}
		if morePending && int64(len(tasks)) < window {
			continue
		}
		if admitted && running < options.KeyConcurrency {
			continue
		}
		if !waitRedisKeyed(ctx, results, complete, delay) {
			return
		}
	}
}

func waitRedisKeyed(ctx context.Context, results <-chan reliableKeyedFailure, complete func(reliableKeyedFailure), delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case result := <-results:
		complete(result)
	case <-timer.C:
	}
	return true
}
