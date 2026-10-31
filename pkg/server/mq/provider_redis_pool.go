package mq

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/redis/go-redis/v9"
)

// NewRedisStreamProviderWithConfig 为单实例显式划分命令/阻塞读预算，零值兼容旧构造器。
func NewRedisStreamProviderWithConfig(cfg config.RedisStreamMQConfig) *RedisStreamProvider {
	p := NewRedisStreamProvider(cfg.Addr, cfg.Prefix, cfg.DB)
	p.commandPoolSize, p.blockingReadPoolSize = cfg.CommandPoolSize, cfg.BlockingReadPoolSize
	return p
}

func (r *RedisStreamProvider) connectRedisPools(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrNotConnected
	}
	if r.client != nil {
		return r.client.Ping(ctx).Err()
	}
	cfg := config.RedisStreamMQConfig{CommandPoolSize: r.commandPoolSize, BlockingReadPoolSize: r.blockingReadPoolSize}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrProviderConfiguration, err)
	}
	commands, reads := cfg.CommandPoolSize, cfg.BlockingReadPoolSize
	if commands == 0 {
		commands = config.DefaultRedisStreamCommandPoolSize
	}
	if reads == 0 {
		reads = config.DefaultRedisStreamBlockingReadPoolSize
	}
	makeClient := func(size int) *redis.Client {
		return redis.NewClient(&redis.Options{Addr: r.addr, DB: r.db, ContextTimeoutEnabled: true, PoolSize: size, MaxActiveConns: size})
	}
	client := makeClient(commands)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return fmt.Errorf("redis-stream: connect to %s: %w", r.addr, err)
	}
	// 阻塞池按需建连，不为每个订阅新建Client或预热全部连接。
	r.client, r.blockingClient = client, makeClient(reads)
	return nil
}

func (r *RedisStreamProvider) closeRedisPools() error {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		for _, cancel := range r.subs {
			cancel()
		}
		r.subs = make(map[string]context.CancelFunc)
		r.reliableMetrics = make(map[string]*redisReliableSubscriptionMetrics)
		blocking, commands := r.blockingClient, r.client
		r.mu.Unlock()
		// 先打断阻塞I/O；owner释放等收尾仍使用命令池。
		var readErr, commandErr error
		if blocking != nil {
			readErr = blocking.Close()
		}
		r.wg.Wait()
		if commands != nil {
			commandErr = commands.Close()
		}
		r.closeErr = errors.Join(readErr, commandErr)
	})
	return r.closeErr
}

// 登记和WaitGroup.Add在同一锁内，Close置closed后不再准入新工作。
func (r *RedisStreamProvider) registerRedisSubscription(key string, cancel context.CancelFunc, metrics *redisReliableSubscriptionMetrics) (func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		cancel()
		return nil, ErrNotConnected
	}
	if _, exists := r.subs[key]; exists {
		cancel()
		return nil, fmt.Errorf("redis-stream: duplicate subscription consumer")
	}
	r.subs[key] = cancel
	if metrics != nil {
		r.reliableMetrics[key] = metrics
	}
	r.wg.Add(1)
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			r.mu.Lock()
			delete(r.subs, key)
			delete(r.reliableMetrics, key)
			r.mu.Unlock()
		})
	}, nil
}

func redisPoolMetrics(role string, client *redis.Client, gauges, counters map[string]float64) {
	if client == nil {
		return
	}
	stats := client.PoolStats()
	if stats == nil {
		return
	}
	prefix := "redis_" + role + "_pool_"
	gauges[prefix+"limit"] = float64(client.Options().PoolSize)
	gauges[prefix+"total"] = float64(stats.TotalConns)
	gauges[prefix+"idle"] = float64(stats.IdleConns)
	gauges[prefix+"in_use"] = float64(max(0, int64(stats.TotalConns)-int64(stats.IdleConns)))
	counters[prefix+"wait_total"] = float64(stats.WaitCount)
	counters[prefix+"wait_seconds_total"] = float64(stats.WaitDurationNs) / 1e9
	counters[prefix+"timeouts_total"] = float64(stats.Timeouts)
}
