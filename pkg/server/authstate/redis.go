package authstate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/types"
	zeroredis "github.com/zeromicro/go-zero/core/stores/redis"
)

type RedisEvalClient interface {
	EvalCtx(context.Context, string, []string, ...interface{}) (interface{}, error)
}

const redisApplyScript = `-- authstate_apply_v1
local eventKey = KEYS[1]
local stateKey = KEYS[2]
if redis.call('EXISTS', eventKey) == 1 then
  local fingerprint = redis.call('HGET', eventKey, 'fingerprint')
  local values = redis.call('HMGET', eventKey, 'applied', 'generation', 'blocked', 'event_order', 'uid', 'control_published')
  if fingerprint ~= ARGV[6] then return {-1, values[2] or '0', values[3] or '0', values[4] or '0', values[5] or '', values[6] or '0'} end
  return {0, values[2] or '0', values[3] or '0', values[4] or '0', values[5] or '', values[6] or '0'}
end
local current = redis.call('HMGET', stateKey, 'generation', 'blocked', 'event_order', 'uid')
local generation = tonumber(current[1]) or 0
local blocked = tonumber(current[2]) or 0
local currentOrder = tonumber(current[3]) or 0
local uid = current[4] or ''
local incomingOrder = tonumber(ARGV[3]) or 0
local applied = 1
if incomingOrder > 0 and currentOrder > 0 and incomingOrder <= currentOrder then
  applied = 0
else
  if tonumber(ARGV[2]) == 1 then
    generation = redis.call('HINCRBY', stateKey, 'generation', 1)
  end
  if tonumber(ARGV[4]) == 1 then blocked = 1 end
  if incomingOrder > 0 then currentOrder = incomingOrder end
  if ARGV[5] ~= '' then uid = ARGV[5] end
  redis.call('HSET', stateKey, 'generation', generation, 'blocked', blocked, 'event_order', currentOrder, 'uid', uid)
end
redis.call('HSET', eventKey, 'fingerprint', ARGV[6], 'applied', applied, 'generation', generation, 'blocked', blocked, 'event_order', currentOrder, 'uid', uid, 'control_published', 0)
redis.call('EXPIRE', eventKey, ARGV[1])
return {applied, generation, blocked, currentOrder, uid, 0}`

const redisCurrentScript = `-- authstate_current_v1
local values = redis.call('HMGET', KEYS[1], 'generation', 'blocked', 'event_order', 'uid')
return {values[1] or '0', values[2] or '0', values[3] or '0', values[4] or ''}`

const redisConfirmScript = `-- authstate_confirm_v1
local generation = tonumber(redis.call('HGET', KEYS[1], 'generation')) or 0
if generation ~= tonumber(ARGV[1]) then return {0, generation} end
redis.call('HSET', KEYS[1], 'generation', generation, 'blocked', 0)
local values = redis.call('HMGET', KEYS[1], 'event_order', 'uid')
return {1, generation, values[1] or '0', values[2] or ''}`

const redisMarkPublishedScript = `-- authstate_mark_published_v1
if redis.call('EXISTS', KEYS[1]) == 0 then return 0 end
redis.call('HSET', KEYS[1], 'control_published', 1)
return 1`

// RedisStore 使用 go-zero Redis 客户端和 Lua 作为共享模式的权威存储。
type RedisStore struct {
	client RedisEvalClient
	prefix string
}

func NewRedisStore(client RedisEvalClient, prefix string) *RedisStore {
	return &RedisStore{client: client, prefix: strings.Trim(strings.TrimSpace(prefix), ":")}
}

func NewConfiguredRedisStore(cfg config.AuthRevocationRedisConfig) (*RedisStore, error) {
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
	return NewRedisStore(client, cfg.Prefix), nil
}

func (s *RedisStore) Current(ctx context.Context, key IdentityKey) (State, error) {
	if err := key.validate(); err != nil {
		return State{}, err
	}
	reply, err := s.eval(ctx, redisCurrentScript, []string{s.stateKey(key)})
	if err != nil {
		return State{}, err
	}
	values, err := redisArray(reply, 4)
	if err != nil {
		return State{}, err
	}
	generation, err := redisUint64(values[0])
	if err != nil {
		return State{}, err
	}
	blocked, err := redisBool(values[1])
	if err != nil {
		return State{}, err
	}
	order, err := redisInt64(values[2])
	if err != nil {
		return State{}, err
	}
	uid, err := redisString(values[3])
	if err != nil {
		return State{}, err
	}
	return State{Key: key, Generation: generation, Blocked: blocked, EventOrder: order, UID: uid}, nil
}

func (s *RedisStore) Apply(ctx context.Context, event types.CasdoorEvent, retention time.Duration) (ApplyResult, error) {
	transition, err := validateEvent(event)
	if err != nil {
		return ApplyResult{}, err
	}
	if retention <= 0 {
		return ApplyResult{}, errors.New("Casdoor事件保留时间必须大于0")
	}
	seconds := int64((retention + time.Second - 1) / time.Second)
	increment := int64(0)
	if transition.increment {
		increment = 1
	}
	block := int64(0)
	if transition.block {
		block = 1
	}
	key := eventIdentityKey(event)
	fingerprint, err := eventFingerprint(event)
	if err != nil {
		return ApplyResult{}, err
	}
	reply, err := s.eval(ctx, redisApplyScript, []string{s.eventKey(event), s.stateKey(key)}, seconds, increment, event.EventOrder, block, event.UID, fingerprint)
	if err != nil {
		return ApplyResult{}, err
	}
	return parseRedisApplyResult(reply, key)
}

func (s *RedisStore) ConfirmActive(ctx context.Context, key IdentityKey, expectedGeneration uint64) (State, error) {
	if err := key.validate(); err != nil {
		return State{}, err
	}
	reply, err := s.eval(ctx, redisConfirmScript, []string{s.stateKey(key)}, strconv.FormatUint(expectedGeneration, 10))
	if err != nil {
		return State{}, err
	}
	values, err := redisArray(reply, 2)
	if err != nil {
		return State{}, err
	}
	confirmed, err := redisBool(values[0])
	if err != nil {
		return State{}, err
	}
	generation, err := redisUint64(values[1])
	if err != nil {
		return State{}, err
	}
	if !confirmed {
		return State{}, ErrGenerationChanged
	}
	state := State{Key: key, Generation: generation}
	if len(values) >= 4 {
		state.EventOrder, err = redisInt64(values[2])
		if err != nil {
			return State{}, err
		}
		state.UID, err = redisString(values[3])
		if err != nil {
			return State{}, err
		}
	}
	return state, nil
}

func (s *RedisStore) SaveSnapshot(context.Context, State) error {
	return errors.New("Redis权威存储不接受快照回写")
}

func (s *RedisStore) MarkControlPublished(ctx context.Context, event types.CasdoorEvent) error {
	reply, err := s.eval(ctx, redisMarkPublishedScript, []string{s.eventKey(event)})
	if err != nil {
		return err
	}
	marked, err := redisBool(reply)
	if err != nil {
		return err
	}
	if !marked {
		return ErrEventNotFound
	}
	return nil
}

func (*RedisStore) SavePendingHook(context.Context, PendingHook) error {
	return errors.New("Pending Hook必须保存到本地Badger")
}
func (*RedisStore) PendingHooks(context.Context, int) ([]PendingHook, error) {
	return nil, errors.New("Pending Hook必须从本地Badger读取")
}
func (*RedisStore) AckHook(context.Context, string) error {
	return errors.New("Pending Hook必须由本地Badger确认")
}
func (*RedisStore) Close() error { return nil }

func (s *RedisStore) eval(ctx context.Context, script string, keys []string, args ...interface{}) (interface{}, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("Redis撤销客户端不可用")
	}
	return s.client.EvalCtx(ctx, script, keys, args...)
}

func (s *RedisStore) stateKey(key IdentityKey) string {
	tag := identityHash(key)
	return s.key("{" + tag + "}:state:v1")
}

func (s *RedisStore) eventKey(event types.CasdoorEvent) string {
	tag := identityHash(eventIdentityKey(event))
	return s.key("{" + tag + "}:event:v1:" + hex.EncodeToString([]byte(event.ID)))
}

func (s *RedisStore) key(suffix string) string {
	if s.prefix == "" {
		return suffix
	}
	return s.prefix + ":" + suffix
}

func identityHash(key IdentityKey) string {
	sum := sha256.Sum256([]byte(key.encoded()))
	return hex.EncodeToString(sum[:16])
}

func parseRedisApplyResult(reply interface{}, key IdentityKey) (ApplyResult, error) {
	values, err := redisArray(reply, 6)
	if err != nil {
		return ApplyResult{}, err
	}
	appliedValue, err := redisInt64(values[0])
	if err != nil {
		return ApplyResult{}, err
	}
	if appliedValue == -1 {
		return ApplyResult{}, ErrInvalidEvent
	}
	if appliedValue != 0 && appliedValue != 1 {
		return ApplyResult{}, errors.New("Redis撤销应用状态无效")
	}
	applied := appliedValue == 1
	generation, err := redisUint64(values[1])
	if err != nil {
		return ApplyResult{}, err
	}
	blocked, err := redisBool(values[2])
	if err != nil {
		return ApplyResult{}, err
	}
	order, err := redisInt64(values[3])
	if err != nil {
		return ApplyResult{}, err
	}
	uid, err := redisString(values[4])
	if err != nil {
		return ApplyResult{}, err
	}
	published, err := redisBool(values[5])
	if err != nil {
		return ApplyResult{}, err
	}
	state := State{Key: key, Generation: generation, Blocked: blocked, EventOrder: order, UID: uid}
	return ApplyResult{Applied: applied, Generation: generation, ControlPublished: published, State: state}, nil
}

func redisArray(value interface{}, minimum int) ([]interface{}, error) {
	values, ok := value.([]interface{})
	if !ok || len(values) < minimum {
		return nil, errors.New("Redis撤销脚本返回值无效")
	}
	return values, nil
}

func redisString(value interface{}) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case []byte:
		return string(typed), nil
	case nil:
		return "", nil
	default:
		return "", errors.New("Redis撤销字符串返回值无效")
	}
}

func redisInt64(value interface{}) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case int:
		return int64(typed), nil
	case uint64:
		if typed > uint64(^uint64(0)>>1) {
			return 0, errors.New("Redis撤销整数越界")
		}
		return int64(typed), nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	case []byte:
		return strconv.ParseInt(string(typed), 10, 64)
	default:
		return 0, errors.New("Redis撤销整数返回值无效")
	}
}

func redisUint64(value interface{}) (uint64, error) {
	switch typed := value.(type) {
	case int64:
		if typed < 0 {
			return 0, errors.New("Redis撤销世代不能为负数")
		}
		return uint64(typed), nil
	case int:
		if typed < 0 {
			return 0, errors.New("Redis撤销世代不能为负数")
		}
		return uint64(typed), nil
	case string:
		return strconv.ParseUint(typed, 10, 64)
	case []byte:
		return strconv.ParseUint(string(typed), 10, 64)
	default:
		return 0, errors.New("Redis撤销世代返回值无效")
	}
}

func redisBool(value interface{}) (bool, error) {
	number, err := redisInt64(value)
	if err != nil {
		return false, err
	}
	if number != 0 && number != 1 {
		return false, errors.New("Redis撤销布尔返回值无效")
	}
	return number == 1, nil
}
