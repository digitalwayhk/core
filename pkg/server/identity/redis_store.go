// 本文件使用独立 Redis 客户端和持久单键 CAS 保存编号权威。
package identity

import (
	"context"
	"errors"
	"github.com/redis/go-redis/v9"
)

// RedisStore 不使用 lease，也不与发现 Provider 共享 Close 所有权。
type RedisStore struct {
	client *redis.Client
	key    string
}

// NewRedisStore 创建独立客户端，连接错误由有界权威检查返回。
func NewRedisStore(addr string, db int, key string) *RedisStore {
	return &RedisStore{client: redis.NewClient(&redis.Options{Addr: addr, DB: db, ContextTimeoutEnabled: true}), key: key}
}
func (s *RedisStore) Load(ctx context.Context) (string, error) {
	raw, err := s.client.Get(ctx, s.key).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrAuthorityMissing
	}
	return raw, err
}

var ledgerCAS = redis.NewScript(`
local old=redis.call('GET',KEYS[1])
if (not old and ARGV[1]=='') or old==ARGV[1] then
 redis.call('SET',KEYS[1],ARGV[2])
 return 1
end
return 0
`)

func (s *RedisStore) CompareAndSwap(ctx context.Context, old, next string) (bool, error) {
	n, err := ledgerCAS.Run(ctx, s.client, []string{s.key}, old, next).Int()
	return n == 1, err
}
func (s *RedisStore) Close() error { return s.client.Close() }
