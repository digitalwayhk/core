// 本文件仅提供停稳后的固定→动态单向迁移，保留原Stream/PEL/游标和正epoch下界。
package mq

import (
	"context"
	"encoding/json"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/redis/go-redis/v9"
	"strconv"
)

// MigrateOwnershipKeys 必须在全部旧Runtime及真实SQL/回调退出后调用，禁止新旧模式混跑。
func (r *RedisStreamProvider) MigrateOwnershipKeys(ctx context.Context, from, to ownership.Manifest, sqlFloor int64) error {
	from = from.Normalize()
	to = to.Normalize()
	if err := from.Validate(); err != nil {
		return err
	}
	if err := to.Validate(); err != nil {
		return err
	}
	if from.DynamicKeys != nil || to.DynamicKeys == nil || sqlFloor <= 0 || len(from.Keys) > to.DynamicKeys.MaxKeys {
		return ownership.ErrUnknown
	}
	same := to
	same.DynamicKeys = nil
	same.Keys = from.Keys
	if same.Fingerprint() != from.Fingerprint() {
		return ownership.ErrUnknown
	}
	if _, err := r.OwnershipStore(ctx); err != nil {
		return err
	}
	raw, err := json.Marshal(to)
	if err != nil {
		return err
	}
	base := r.ownershipBase(from.Scope)
	keys := []string{base + ":meta", base + ":sequence", base + ":directory"}
	for _, subject := range from.Subjects {
		keys = append(keys, r.ownerLockKey(subject, from.Group))
	}
	for _, key := range from.Keys {
		keys = append(keys, r.ownershipLease(from.Scope, key))
	}
	args := []any{from.Fingerprint(), to.Fingerprint(), string(raw), ownedRedisMarker(from), ownedRedisMarker(to), len(from.Subjects), strconv.FormatInt(sqlFloor, 10), to.DynamicKeys.MaxKeys}
	for _, key := range from.Keys {
		args = append(args, "key:"+ownershipDigest(key), key)
	}
	return ownershipRedisError(redis.NewScript(`
local seq=redis.call("GET",KEYS[2]);local floor=ARGV[7]
if not seq or not string.match(seq,"^[1-9][0-9]*$") or #seq>19 then return redis.error_reply("OWNER_UNKNOWN") end
if #seq<#floor or (#seq==#floor and seq<floor) then return redis.error_reply("OWNER_UNKNOWN") end
if redis.call("HGET",KEYS[1],"enabled")~="1" then return redis.error_reply("OWNER_NOT_READY") end
local fp=redis.call("HGET",KEYS[1],"fingerprint");local last=3+tonumber(ARGV[6])
if fp==ARGV[2] then
 if redis.call("HGET",KEYS[1],"directory_mode")~="dynamic-v1" then return redis.error_reply("OWNER_UNKNOWN") end
 for i=4,last do if redis.call("GET",KEYS[i])~=ARGV[5] then return redis.error_reply("OWNER_MODE_CONFLICT") end end
 return 1
end
if fp~=ARGV[1] or redis.call("EXISTS",KEYS[3])~=0 then return redis.error_reply("OWNER_UNKNOWN") end
for i=4,last do if redis.call("GET",KEYS[i])~=ARGV[4] then return redis.error_reply("OWNER_MODE_CONFLICT") end end
for i=last+1,#KEYS do if redis.call("EXISTS",KEYS[i])~=0 then return redis.error_reply("OWNER_BUSY") end end
for i=9,#ARGV,2 do if redis.call("HGET",KEYS[1],ARGV[i])~=ARGV[i+1] then return redis.error_reply("OWNER_UNKNOWN") end end
for i=9,#ARGV,2 do redis.call("RPUSH",KEYS[3],ARGV[i]) end
redis.call("HSET",KEYS[1],"fingerprint",ARGV[2],"manifest",ARGV[3],"directory_mode","dynamic-v1","directory_capacity",ARGV[8],"directory_count",(#ARGV-8)/2)
for i=4,last do redis.call("SET",KEYS[i],ARGV[5]) end
return 1`).Run(ctx, r.client, keys, args...).Err())
}
