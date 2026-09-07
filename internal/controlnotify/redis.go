// 本文件使用 Redis 原生广播；不创建 Stream、消费组或历史数据键。
package controlnotify

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/redis/go-redis/v9"
)

type redisTransport struct {
	client   *redis.Client
	sub      *redis.PubSub
	channel  string
	failed   atomic.Bool
	lastRead atomic.Int64
	once     sync.Once
	cancel   context.CancelFunc
	done     chan struct{}
	closeErr error
}

func openRedis(ctx context.Context, cfg config.RedisStreamMQConfig, channel string) (*redisTransport, error) {
	client := redis.NewClient(&redis.Options{Addr: cfg.Addr, DB: cfg.DB, Protocol: 2, MaxRetries: -1,
		DialTimeout: operationTimeout, ReadTimeout: operationTimeout, WriteTimeout: operationTimeout, ContextTimeoutEnabled: true})
	sub := client.Subscribe(ctx, channel)
	first, err := sub.ReceiveTimeout(ctx, operationTimeout)
	if err == nil {
		confirmation, ok := first.(*redis.Subscription)
		if !ok || confirmation.Kind != "subscribe" || confirmation.Channel != channel {
			err = ErrUnavailable
		}
	}
	if err != nil {
		_ = sub.Close()
		_ = client.Close()
		return nil, err
	}
	lifetime, cancel := context.WithCancel(context.Background())
	x := &redisTransport{client: client, sub: sub, channel: channel, cancel: cancel, done: make(chan struct{})}
	x.lastRead.Store(time.Now().UnixNano())
	go x.heartbeat(lifetime)
	return x, nil
}

func (x *redisTransport) heartbeat(ctx context.Context) {
	defer close(x.done)
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if time.Since(time.Unix(0, x.lastRead.Load())) > operationTimeout {
				x.fail()
				return
			}
			pingCtx, cancel := context.WithTimeout(ctx, operationTimeout)
			err := x.sub.Ping(pingCtx)
			cancel()
			if err != nil {
				x.fail()
				return
			}
		}
	}
}

func (x *redisTransport) fail() {
	x.failed.Store(true)
	x.cancel()
	_ = x.sub.Close()
}

func (x *redisTransport) Publish(ctx context.Context, data []byte) error {
	if err := validatePayload(data); err != nil {
		return err
	}
	if x.failed.Load() {
		return ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if err := x.client.Publish(ctx, x.channel, data).Err(); err != nil {
		x.fail()
		return ErrUnavailable
	}
	return nil
}

func (x *redisTransport) Receive(ctx context.Context) ([]byte, error) {
	// 取消即破坏本轮连续性，关闭底层连接打断阻塞读，不静默重连。
	stop := context.AfterFunc(ctx, x.fail)
	defer stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if x.failed.Load() {
			return nil, ErrUnavailable
		}
		value, err := x.sub.ReceiveTimeout(ctx, operationTimeout)
		if err != nil {
			x.fail()
			return nil, ErrUnavailable
		}
		x.lastRead.Store(time.Now().UnixNano())
		switch message := value.(type) {
		case *redis.Message:
			if message.Channel != x.channel || validatePayload([]byte(message.Payload)) != nil {
				x.fail()
				return nil, ErrUnavailable
			}
			if x.failed.Load() {
				return nil, ErrUnavailable
			}
			return []byte(message.Payload), nil
		case *redis.Pong:
			continue
		default:
			// 包括客户端透明重连后的 subscribe 确认：必须向上层报告间隙。
			x.fail()
			return nil, ErrUnavailable
		}
	}
}

func (x *redisTransport) Close() error {
	x.once.Do(func() {
		x.fail()
		<-x.done
		x.closeErr = x.client.Close()
		if errors.Is(x.closeErr, redis.ErrClosed) {
			x.closeErr = nil
		}
	})
	return x.closeErr
}
