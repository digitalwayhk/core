// 本文件提供受信应用的原子追加目录；不从业务消息或Gateway请求隐式注册。
package mq

import (
	"context"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/redis/go-redis/v9"
	"strconv"
	"strings"
)

// DynamicOwnedReliableMQProvider 区分固定范围归属与运行期追加目录能力。
type DynamicOwnedReliableMQProvider interface {
	OwnedReliableMQProvider
	RegisterOwnershipKey(context.Context, ownership.Manifest, string) error
	MigrateOwnershipKeys(context.Context, ownership.Manifest, ownership.Manifest, int64) error
}

const redisOwnershipDirectoryGuard = `
local seq=redis.call("GET",KEYS[2])
if not seq or not string.match(seq,"^[1-9][0-9]*$") or #seq>19 then return redis.error_reply("OWNER_UNKNOWN") end
if redis.call("HGET",KEYS[1],"fingerprint")~=ARGV[1] or redis.call("HGET",KEYS[1],"directory_mode")~="dynamic-v1" then return redis.error_reply("OWNER_UNKNOWN") end
local count=redis.call("HGET",KEYS[1],"directory_count")
local capacity=redis.call("HGET",KEYS[1],"directory_capacity")
if not count or not string.match(count,"^%d+$") or not capacity or tonumber(capacity)~=tonumber(ARGV[2]) or tonumber(count)>tonumber(capacity) or redis.call("LLEN",KEYS[3])~=tonumber(count) then return redis.error_reply("OWNER_UNKNOWN") end
`

// RegisterOwnershipKey 只追加不透明key；原子幂等，冲突/未知/容量拒绝均不伪装成功。
func (r *RedisStreamProvider) RegisterOwnershipKey(ctx context.Context, m ownership.Manifest, key string) error {
	s, err := r.OwnershipStore(ctx)
	if err != nil {
		return err
	}
	return s.(ownership.KeyRegistry).RegisterKey(ctx, m, key)
}

func (s *redisOwnershipStore) RegisterKey(ctx context.Context, m ownership.Manifest, key string) (result error) {
	defer func() {
		if result == nil {
			s.provider.ownershipRegistered.Add(1)
		} else {
			s.provider.ownershipRegistrationFailures.Add(1)
			if result == ownership.ErrCapacity {
				s.provider.ownershipRegistrationCapacity.Add(1)
			}
		}
	}()
	m = m.Normalize()
	if err := m.Validate(); err != nil {
		return err
	}
	if m.DynamicKeys == nil {
		return ownership.ErrUnsupported
	}
	if strings.TrimSpace(key) == "" || len(key) > 512 {
		return ownership.ErrUnknown
	}
	base := s.provider.ownershipBase(m.Scope)
	return ownershipRedisError(redis.NewScript(redisOwnershipDirectoryGuard+`
if redis.call("HGET",KEYS[1],"enabled")~="1" then return redis.error_reply("OWNER_NOT_READY") end
local current=redis.call("HGET",KEYS[1],ARGV[3])
if current then if current~=ARGV[4] or not redis.call("LPOS",KEYS[3],ARGV[3]) then return redis.error_reply("OWNER_UNKNOWN") end;return 1 end
if tonumber(count)>=tonumber(capacity) then return redis.error_reply("OWNER_CAPACITY") end
redis.call("HSET",KEYS[1],ARGV[3],ARGV[4]);redis.call("RPUSH",KEYS[3],ARGV[3]);redis.call("HINCRBY",KEYS[1],"directory_count",1);return 1
`).Run(ctx, s.provider.client, []string{base + ":meta", base + ":sequence", base + ":directory"}, m.Fingerprint(), m.DynamicKeys.MaxKeys, "key:"+ownershipDigest(key), key).Err())
}

func (s *redisOwnershipStore) ListKeys(ctx context.Context, m ownership.Manifest, after, limit int) (ownership.KeyPage, error) {
	m = m.Normalize()
	if err := m.Validate(); err != nil {
		return ownership.KeyPage{}, err
	}
	if m.DynamicKeys == nil {
		return ownership.KeyPage{}, ownership.ErrUnsupported
	}
	if after < 0 || after > m.DynamicKeys.MaxKeys || limit < 1 || limit > m.DynamicKeys.PageSize {
		return ownership.KeyPage{}, ownership.ErrCapacity
	}
	base := s.provider.ownershipBase(m.Scope)
	rows, err := redis.NewScript(redisOwnershipDirectoryGuard+`
if tonumber(ARGV[3])>tonumber(count) then return redis.error_reply("OWNER_UNKNOWN") end
local fields=redis.call("LRANGE",KEYS[3],ARGV[3],tonumber(ARGV[3])+tonumber(ARGV[4])-1);local result={count}
for i,field in ipairs(fields) do if redis.call("LPOS",KEYS[3],field)~=tonumber(ARGV[3])+i-1 then return redis.error_reply("OWNER_UNKNOWN") end;local value=redis.call("HGET",KEYS[1],field);if not value or string.sub(field,1,4)~="key:" then return redis.error_reply("OWNER_UNKNOWN") end;table.insert(result,value) end
return result`).Run(ctx, s.provider.client, []string{base + ":meta", base + ":sequence", base + ":directory"}, m.Fingerprint(), m.DynamicKeys.MaxKeys, after, limit).Slice()
	if err != nil {
		return ownership.KeyPage{}, ownershipRedisError(err)
	}
	if len(rows) < 1 {
		return ownership.KeyPage{}, ownership.ErrUnknown
	}
	total, err := strconv.Atoi(fmt.Sprint(rows[0]))
	if err != nil {
		return ownership.KeyPage{}, ownership.ErrUnknown
	}
	page := ownership.KeyPage{Total: total, Next: after}
	for _, row := range rows[1:] {
		key, ok := row.(string)
		if !ok || strings.TrimSpace(key) == "" || len(key) > 512 {
			return ownership.KeyPage{}, ownership.ErrUnknown
		}
		page.Keys = append(page.Keys, key)
		page.Next++
	}
	return page, nil
}

// 维护操作在同一Lua命令内核对整个有限目录，不遗漏后来注册key的活动租约。
const redisOwnershipDynamicNoLeases = `
local raw=redis.call("HGET",KEYS[1],"manifest");local valid,manifest=pcall(cjson.decode,raw or "")
if not valid or type(manifest)~="table" then return redis.error_reply("OWNER_UNKNOWN") end
if manifest.DynamicKeys then
 if redis.call("HGET",KEYS[1],"directory_mode")~="dynamic-v1" then return redis.error_reply("OWNER_UNKNOWN") end
 local base=string.sub(KEYS[1],1,-6)
 local count=redis.call("HGET",KEYS[1],"directory_count");local capacity=redis.call("HGET",KEYS[1],"directory_capacity")
 if not count or not capacity or not tonumber(count) or not tonumber(capacity) or tonumber(count)>tonumber(capacity) or redis.call("LLEN",base..":directory")~=tonumber(count) then return redis.error_reply("OWNER_UNKNOWN") end
 for _,field in ipairs(redis.call("LRANGE",base..":directory",0,-1)) do
  if not redis.call("HGET",KEYS[1],field) then return redis.error_reply("OWNER_UNKNOWN") end
  if redis.call("EXISTS",base..":lease:"..string.sub(field,5))~=0 then return redis.error_reply("OWNER_BUSY") end
 end
end
`
