// 本文件提供保全原消息历史的显式维护恢复；不把缺失序列在运行中自动补成0。
package mq

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/redis/go-redis/v9"
	"strconv"
)

// RecoverOwnership 要求原合同、原绑定模式及无活动归属，恢复到不低于SQL权威的正下界。
// 调用方须先停止全部owner并确认真实SQL/远端调用退出；租约缺失本身不能证明这一步。
func (r *RedisStreamProvider) RecoverOwnership(ctx context.Context, m ownership.Manifest, floor int64) error {
	m = m.Normalize()
	if err := m.Validate(); err != nil {
		return err
	}
	if floor <= 0 {
		return ownership.ErrUnknown
	}
	if _, err := r.OwnershipStore(ctx); err != nil {
		return err
	}
	base := r.ownershipBase(m.Scope)
	keys := []string{base + ":meta", base + ":sequence"}
	for _, subject := range m.Subjects {
		keys = append(keys, r.ownerLockKey(subject, m.Group))
	}
	for _, key := range m.Keys {
		keys = append(keys, r.ownershipLease(m.Scope, key))
	}
	return ownershipRedisError(redis.NewScript(`
if redis.call("HGET",KEYS[1],"fingerprint")~=ARGV[1] or redis.call("HGET",KEYS[1],"enabled")~="1" then return redis.error_reply("OWNER_UNKNOWN") end
local last=2+tonumber(ARGV[4])
for i=3,last do if redis.call("GET",KEYS[i])~=ARGV[2] then return redis.error_reply("OWNER_MODE_CONFLICT") end end
for i=last+1,#KEYS do if redis.call("EXISTS",KEYS[i])~=0 then return redis.error_reply("OWNER_BUSY") end end
local current=redis.call("GET",KEYS[2]);local floor=ARGV[3]
if current then
 if not string.match(current,"^[1-9][0-9]*$") then return redis.error_reply("OWNER_UNKNOWN") end
 if #current>#floor or (#current==#floor and current>=floor) then return 1 end
end
redis.call("SET",KEYS[2],floor);return 1`).Run(ctx, r.client, keys, m.Fingerprint(), ownedRedisMarker(m), strconv.FormatInt(floor, 10), len(m.Subjects)).Err())
}
