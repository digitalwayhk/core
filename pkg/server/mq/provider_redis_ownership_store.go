// 本文件实现持续归属存储；复用Provider命令池，不向应用暴露Redis键或脚本。
package mq

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/redis/go-redis/v9"
	"strconv"
	"strings"
	"time"
)

type redisOwnershipStore struct{ provider *RedisStreamProvider }

func ownershipDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func (r *RedisStreamProvider) ownershipBase(scope string) string {
	return r.prefix + ":ownership:" + ownershipDigest(scope)
}
func (r *RedisStreamProvider) ownershipLease(scope, key string) string {
	return r.ownershipBase(scope) + ":lease:" + ownershipDigest(key)
}
func (r *RedisStreamProvider) ownershipRefs(scope, key string) string {
	return r.ownershipBase(scope) + ":refs:" + ownershipDigest(key)
}
func (r *RedisStreamProvider) ownershipGateway(id string) string {
	return r.prefix + ":ownership-gateway:" + ownershipDigest(id)
}

func ownershipRedisError(err error) error {
	if err == nil {
		return nil
	}
	for marker, target := range map[string]error{"OWNER_UNKNOWN": ownership.ErrUnknown, "OWNER_FENCED": ownership.ErrFenced, "OWNER_NOT_READY": ownership.ErrNotReady, "OWNER_BUSY": ownership.ErrBusy, "OWNER_CAPACITY": ownership.ErrCapacity, "OWNER_STALE_REFERENCE": ownership.ErrStaleSnapshot, "OWNER_MODE_CONFLICT": ErrReliableGroupModeConflict} {
		if strings.Contains(err.Error(), marker) {
			return target
		}
	}
	return err
}

// OwnershipStore 返回持续归属及框架WS引用存储；关闭的Provider不可重用。
func (r *RedisStreamProvider) OwnershipStore(context.Context) (ownership.Store, error) {
	if r == nil {
		return nil, ErrNotConnected
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.client == nil {
		return nil, ErrNotConnected
	}
	return &redisOwnershipStore{r}, nil
}

// ProvisionOwnership 是显式维护初始化；现有元数据不匹配或序列丢失时拒绝重建。
func (r *RedisStreamProvider) ProvisionOwnership(ctx context.Context, manifest ownership.Manifest, epochFloor int64) error {
	manifest = manifest.Normalize()
	if err := manifest.Validate(); err != nil {
		return err
	}
	if epochFloor < 0 {
		return ownership.ErrUnknown
	}
	if epochFloor == 0 {
		epochFloor = 1
	}
	if _, err := r.OwnershipStore(ctx); err != nil {
		return err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	base := r.ownershipBase(manifest.Scope)
	args := []any{manifest.Fingerprint(), string(data), strconv.FormatInt(epochFloor, 10), manifest.Service, manifest.Timing.Lease.Milliseconds(), manifest.MaxReferences}
	for _, key := range manifest.Keys {
		args = append(args, "key:"+ownershipDigest(key), key)
	}
	return ownershipRedisError(redis.NewScript(`
local meta=redis.call("EXISTS",KEYS[1]);local seq=redis.call("EXISTS",KEYS[2])
if meta~=0 or seq~=0 then
 if meta~=1 or seq~=1 or redis.call("HGET",KEYS[1],"fingerprint")~=ARGV[1] then return redis.error_reply("OWNER_UNKNOWN") end
 local value=redis.call("GET",KEYS[2]);if not value or not string.match(value,"^[1-9][0-9]*$") then return redis.error_reply("OWNER_UNKNOWN") end
 return 1
end
redis.call("SET",KEYS[2],ARGV[3])
redis.call("HSET",KEYS[1],"fingerprint",ARGV[1],"manifest",ARGV[2],"service",ARGV[4],"lease",ARGV[5],"references",ARGV[6],"enabled","1")
for i=7,#ARGV,2 do redis.call("HSET",KEYS[1],ARGV[i],ARGV[i+1]) end
return 1`).Run(ctx, r.client, []string{base + ":meta", base + ":sequence"}, args...).Err())
}

func (s *redisOwnershipStore) manifest(ctx context.Context, scope string) (ownership.Manifest, error) {
	var m ownership.Manifest
	base := s.provider.ownershipBase(scope)
	raw, err := redis.NewScript(`local sequence=redis.call("GET",KEYS[2]);if not sequence or not string.match(sequence,"^[1-9][0-9]*$") then return redis.error_reply("OWNER_UNKNOWN") end;local manifest=redis.call("HGET",KEYS[1],"manifest");if not manifest then return redis.error_reply("OWNER_UNKNOWN") end;return manifest`).Run(ctx, s.provider.client, []string{base + ":meta", base + ":sequence"}).Text()
	if errors.Is(err, redis.Nil) {
		return m, ownership.ErrUnknown
	}
	if err != nil {
		return m, ownershipRedisError(err)
	}
	if json.Unmarshal([]byte(raw), &m) != nil || m.Validate() != nil {
		return m, ownership.ErrUnknown
	}
	return m, nil
}

const redisOwnershipMetadataGuard = `
local sequence=redis.call("GET",KEYS[2])
if not sequence or not string.match(sequence,"^[1-9][0-9]*$") or redis.call("HGET",KEYS[1],ARGV[1])~=ARGV[2] then return redis.error_reply("OWNER_UNKNOWN") end
`
const redisOwnershipCredentialGuard = redisOwnershipMetadataGuard + `
if redis.call("HGET",KEYS[3],"token")~=ARGV[3] or redis.call("HGET",KEYS[3],"epoch")~=ARGV[4] then return redis.error_reply("OWNER_FENCED") end
if redis.call("HGET",KEYS[3],"instance")~=ARGV[5] or redis.call("HGET",KEYS[3],"session")~=ARGV[6] or redis.call("HGET",KEYS[3],"service")~=ARGV[7] then return redis.error_reply("OWNER_FENCED") end
if #sequence<#ARGV[4] or (#sequence==#ARGV[4] and sequence<ARGV[4]) then return redis.error_reply("OWNER_UNKNOWN") end
`

func (s *redisOwnershipStore) keys(c ownership.Credential) []string {
	base := s.provider.ownershipBase(c.Scope)
	return []string{base + ":meta", base + ":sequence", s.provider.ownershipLease(c.Scope, c.Key)}
}
func ownershipCredentialArgs(c ownership.Credential) []any {
	return []any{"key:" + ownershipDigest(c.Key), c.Key, c.Token, strconv.FormatInt(c.Epoch, 10), c.InstanceID, c.SessionID, c.Service}
}

// Acquire 原子取得声明键并生成正序列凭据，不自动恢复未知序列。
func (s *redisOwnershipStore) Acquire(ctx context.Context, c ownership.Credential, ttl time.Duration) (ownership.Credential, error) {
	if c.Epoch != 0 || c.Token != "" {
		return ownership.Credential{}, ownership.ErrUnknown
	}
	c.Epoch = 1
	c.Token = "candidate"
	if c.Validate() != nil {
		return ownership.Credential{}, ownership.ErrUnknown
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return ownership.Credential{}, err
	}
	c.Token = hex.EncodeToString(token[:])
	c.Epoch = 0
	args := []any{"key:" + ownershipDigest(c.Key), c.Key, c.Token, ttl.Milliseconds(), c.Scope, c.Service, c.InstanceID, c.SessionID}
	epoch, err := redis.NewScript(redisOwnershipMetadataGuard+`
if redis.call("HGET",KEYS[1],"service")~=ARGV[6] or redis.call("HGET",KEYS[1],"lease")~=ARGV[4] then return redis.error_reply("OWNER_UNKNOWN") end
if redis.call("HGET",KEYS[1],"enabled")~="1" then return redis.error_reply("OWNER_NOT_READY") end
if redis.call("EXISTS",KEYS[3])~=0 then return redis.error_reply("OWNER_BUSY") end
redis.call("INCR",KEYS[2]);local epoch=redis.call("GET",KEYS[2])
redis.call("HSET",KEYS[3],"scope",ARGV[5],"key",ARGV[2],"token",ARGV[3],"epoch",epoch,"service",ARGV[6],"instance",ARGV[7],"session",ARGV[8],"ready","0")
redis.call("PEXPIRE",KEYS[3],ARGV[4]);return epoch`).Run(ctx, s.provider.client, s.keys(c), args...).Text()
	if err != nil {
		return ownership.Credential{}, ownershipRedisError(err)
	}
	c.Epoch, err = strconv.ParseInt(epoch, 10, 64)
	if err != nil || c.Validate() != nil {
		return ownership.Credential{}, ownership.ErrUnknown
	}
	return c, nil
}

// Renew 只为完整匹配的当前凭据续租。
func (s *redisOwnershipStore) Renew(ctx context.Context, c ownership.Credential, ttl time.Duration) error {
	if c.Validate() != nil {
		return ownership.ErrUnknown
	}
	args := append(ownershipCredentialArgs(c), ttl.Milliseconds())
	return ownershipRedisError(redis.NewScript(redisOwnershipCredentialGuard+`
if redis.call("HGET",KEYS[1],"lease")~=ARGV[8] then return redis.error_reply("OWNER_UNKNOWN") end
return redis.call("PEXPIRE",KEYS[3],ARGV[8])`).Run(ctx, s.provider.client, s.keys(c), args...).Err())
}

// Ready 在恢复完成后公布当前凭据。
func (s *redisOwnershipStore) Ready(ctx context.Context, c ownership.Credential) error {
	if c.Validate() != nil {
		return ownership.ErrUnknown
	}
	return ownershipRedisError(redis.NewScript(redisOwnershipCredentialGuard+`return redis.call("HSET",KEYS[3],"ready","1")`).Run(ctx, s.provider.client, s.keys(c), ownershipCredentialArgs(c)...).Err())
}

// Validate 校验当前凭据，按调用方要求同时校验Ready。
func (s *redisOwnershipStore) Validate(ctx context.Context, c ownership.Credential, ready bool) error {
	if c.Validate() != nil {
		return ownership.ErrUnknown
	}
	want := 0
	if ready {
		want = 1
	}
	args := append(ownershipCredentialArgs(c), want)
	return ownershipRedisError(redis.NewScript(redisOwnershipCredentialGuard+`
if ARGV[8]=="1" and redis.call("HGET",KEYS[3],"ready")~="1" then return redis.error_reply("OWNER_NOT_READY") end
return 1`).Run(ctx, s.provider.client, s.keys(c), args...).Err())
}

// Release 只删除当前完整凭据匹配的租约。
func (s *redisOwnershipStore) Release(ctx context.Context, c ownership.Credential) error {
	if c.Validate() != nil {
		return ownership.ErrUnknown
	}
	return ownershipRedisError(redis.NewScript(redisOwnershipCredentialGuard+`return redis.call("DEL",KEYS[3])`).Run(ctx, s.provider.client, s.keys(c), ownershipCredentialArgs(c)...).Err())
}

// Resolve 解析并复核Ready凭据，未知状态明确失败。
func (s *redisOwnershipStore) Resolve(ctx context.Context, scope, key string) (ownership.Credential, error) {
	c := ownership.Credential{Scope: scope, Key: key}
	values, err := redis.NewScript(redisOwnershipMetadataGuard+`
if redis.call("HGET",KEYS[3],"ready")~="1" then return redis.error_reply("OWNER_NOT_READY") end
return redis.call("HMGET",KEYS[3],"service","instance","session","epoch","token")`).Run(ctx, s.provider.client, s.keys(c), "key:"+ownershipDigest(key), key).Slice()
	if err != nil {
		return c, ownershipRedisError(err)
	}
	if len(values) != 5 {
		return c, ownership.ErrUnknown
	}
	text := func(v any) string {
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
	c.Service = text(values[0])
	c.InstanceID = text(values[1])
	c.SessionID = text(values[2])
	c.Epoch, err = strconv.ParseInt(text(values[3]), 10, 64)
	c.Token = text(values[4])
	if err != nil || c.Validate() != nil {
		return ownership.Credential{}, ownership.ErrUnknown
	}
	if err := s.Validate(ctx, c, true); err != nil {
		return ownership.Credential{}, err
	}
	return c, nil
}
