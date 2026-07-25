package routecache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	redisv9 "github.com/redis/go-redis/v9"
	zeroredis "github.com/zeromicro/go-zero/core/stores/redis"
)

type RedisClient interface {
	GetCtx(context.Context, string) (string, error)
	SetexCtx(context.Context, string, string, int) error
	SetnxCtx(context.Context, string, string) (bool, error)
	IncrCtx(context.Context, string) (int64, error)
	DelCtx(context.Context, ...string) (int, error)
	PingCtx(context.Context) bool
}

// RedisL3 是共享模式的事实缓存。L1/L2 只保存它的短期副本，不能在 Redis
// 不可用或跨节点失效订阅未就绪时继续提供命中。
type RedisL3 struct {
	client RedisClient
	prefix string
}

func NewRedisL3(client RedisClient, cfg config.RouteCacheRedisConfig) *RedisL3 {
	return &RedisL3{client: client, prefix: strings.TrimSuffix(cfg.Prefix, ":")}
}

func newConfiguredRedisL3(cfg config.RouteCacheRedisConfig) (*RedisL3, error) {
	if strings.TrimSpace(cfg.Addr) == "" {
		return nil, errors.New("routeCache.redis.addr is required in shared mode")
	}
	client, err := zeroredis.NewRedis(zeroredis.RedisConf{
		Host:        cfg.Addr,
		Type:        zeroredis.NodeType,
		Pass:        cfg.Password,
		NonBlock:    false,
		PingTimeout: 2 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	return NewRedisL3(client, cfg), nil
}

func (r *RedisL3) Get(ctx context.Context, key string) (json.RawMessage, bool, error) {
	encoded, err := r.client.GetCtx(ctx, r.key(key))
	if errors.Is(err, redisv9.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	envelope := cacheEnvelope{}
	if err := json.Unmarshal([]byte(encoded), &envelope); err != nil {
		return nil, false, err
	}
	if envelope.Version != 1 {
		return nil, false, errors.New("unsupported redis route cache envelope version")
	}
	if envelope.ExpiresAt <= time.Now().UnixNano() {
		_ = r.Delete(ctx, key)
		return nil, false, nil
	}
	return append(json.RawMessage(nil), envelope.Data...), true, nil
}

func (r *RedisL3) Set(ctx context.Context, key string, value json.RawMessage, ttl time.Duration) error {
	if ttl <= 0 {
		return errors.New("redis route cache ttl must be positive")
	}
	encoded, err := json.Marshal(cacheEnvelope{
		Version:   1,
		ExpiresAt: time.Now().Add(ttl).UnixNano(),
		Data:      append(json.RawMessage(nil), value...),
	})
	if err != nil {
		return err
	}
	seconds := int((ttl + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return r.client.SetexCtx(ctx, r.key(key), string(encoded), seconds)
}

func (r *RedisL3) Delete(ctx context.Context, key string) error {
	_, err := r.client.DelCtx(ctx, r.key(key))
	return err
}

func (r *RedisL3) Ping(ctx context.Context) bool {
	return r != nil && r.client != nil && r.client.PingCtx(ctx)
}

func (r *RedisL3) Generation(ctx context.Context, service, route string) (uint64, error) {
	key := r.key(generationKey(service, route))
	value, err := r.client.GetCtx(ctx, key)
	// go-zero Redis.GetCtx 将 redis.Nil 转换为空字符串和 nil。
	if errors.Is(err, redisv9.Nil) || (err == nil && value == "") {
		created, setErr := r.client.SetnxCtx(ctx, key, "1")
		if setErr != nil {
			return 0, setErr
		}
		if created {
			return 1, nil
		}
		value, err = r.client.GetCtx(ctx, key)
	}
	if err != nil {
		return 0, err
	}
	return parseGeneration(value)
}

func (r *RedisL3) IncrementGeneration(ctx context.Context, service, route string) (uint64, error) {
	if _, err := r.Generation(ctx, service, route); err != nil {
		return 0, err
	}
	value, err := r.client.IncrCtx(ctx, r.key(generationKey(service, route)))
	if err != nil {
		return 0, err
	}
	if value <= 0 {
		return 0, errors.New("redis route cache generation must be positive")
	}
	return uint64(value), nil
}

func generationKey(service, route string) string {
	return "__meta:generation:" + service + ":" + route
}

func parseGeneration(value string) (uint64, error) {
	generation, err := strconv.ParseUint(value, 10, 64)
	if err != nil || generation == 0 {
		return 0, fmt.Errorf("invalid redis route cache generation")
	}
	return generation, nil
}

func (r *RedisL3) key(key string) string {
	if r.prefix == "" {
		return key
	}
	return r.prefix + ":" + key
}
