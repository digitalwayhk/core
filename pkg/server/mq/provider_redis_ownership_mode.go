// 本文件显式维护绑定原SharedKeys组到持续归属；不删除mode、PEL、group或重置游标。
package mq

import (
	"context"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/redis/go-redis/v9"
	"strings"
)

// BindOwnership 只能在停止原消费者且实际handler退出的维护窗口调用。
func (r *RedisStreamProvider) BindOwnership(ctx context.Context, m ownership.Manifest) error {
	m = m.Normalize()
	if err := m.Validate(); err != nil {
		return err
	}
	store, err := r.OwnershipStore(ctx)
	if err != nil {
		return err
	}
	actual, err := store.(*redisOwnershipStore).manifest(ctx, m.Scope)
	if err != nil {
		return err
	}
	if actual.Fingerprint() != m.Fingerprint() {
		return ownership.ErrUnknown
	}
	for _, subject := range m.Subjects {
		err := r.client.XGroupCreateMkStream(ctx, r.streamKey(subject), m.Group, "0").Err()
		if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
			return err
		}
	}
	base := r.ownershipBase(m.Scope)
	keys := []string{base + ":meta", base + ":sequence"}
	for _, subject := range m.Subjects {
		keys = append(keys, r.ownerLockKey(subject, m.Group))
	}
	snapshot, legacyLocks, err := r.ownershipTransitionSnapshot(ctx, m)
	if err != nil {
		return err
	}
	keys = append(keys, legacyLocks...)
	return ownershipRedisError(redis.NewScript(`
if redis.call("HGET",KEYS[1],"fingerprint")~=ARGV[1] or redis.call("EXISTS",KEYS[2])~=1 then return redis.error_reply("OWNER_UNKNOWN") end
local last=2+tonumber(ARGV[4]);local allBound=true
for i=3,last do local marker=redis.call("GET",KEYS[i]);if marker and marker~=ARGV[2] and marker~=ARGV[3] then return redis.error_reply("OWNER_MODE_CONFLICT") end;if marker~=ARGV[3] then allBound=false end end
if allBound and redis.call("HGET",KEYS[1],"enabled")=="1" then return 1 end
local snapshot=cjson.decode(ARGV[5])
for _,entry in ipairs(snapshot) do
 local current=redis.call("XPENDING",entry.Stream,ARGV[6],"-","+",tonumber(ARGV[7])+1)
 if #current~=#entry.IDs then return redis.error_reply("OWNER_BUSY") end
 for i=1,#current do if current[i][1]~=entry.IDs[i] then return redis.error_reply("OWNER_BUSY") end end
end
for i=last+1,#KEYS do if redis.call("EXISTS",KEYS[i])~=0 then return redis.error_reply("OWNER_BUSY") end end
for i=3,last do redis.call("SET",KEYS[i],ARGV[3]) end
redis.call("HSET",KEYS[1],"enabled","1");return 1`).Run(ctx, r.client, keys, m.Fingerprint(), fmt.Sprintf("%s%d", redisSharedModePrefix, m.Window), ownedRedisMarker(m), len(m.Subjects), snapshot, m.Group, m.Window).Err())
}

// UnbindOwnership 要求无持续归属租约，回到原shared-v1/W标记，不回退游标。
func (r *RedisStreamProvider) UnbindOwnership(ctx context.Context, m ownership.Manifest) error {
	m = m.Normalize()
	if err := m.Validate(); err != nil {
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
if redis.call("HGET",KEYS[1],"fingerprint")~=ARGV[1] or redis.call("EXISTS",KEYS[2])~=1 then return redis.error_reply("OWNER_UNKNOWN") end
local last=2+tonumber(ARGV[4])
for i=3,last do if redis.call("GET",KEYS[i])~=ARGV[2] then return redis.error_reply("OWNER_MODE_CONFLICT") end end
for i=last+1,#KEYS do if redis.call("EXISTS",KEYS[i])~=0 then return redis.error_reply("OWNER_BUSY") end end
redis.call("HSET",KEYS[1],"enabled","0")
for i=3,last do redis.call("SET",KEYS[i],ARGV[3]) end
return 1`).Run(ctx, r.client, keys, m.Fingerprint(), ownedRedisMarker(m), fmt.Sprintf("%s%d", redisSharedModePrefix, m.Window), len(m.Subjects)).Err())
}

// SubscribeOwnedBatch 在持续归属下有界接收同范围多主题业务批次，成功后才ACK。
func (r *RedisStreamProvider) SubscribeOwnedBatch(ctx context.Context, options OwnedReliableOptions, handler func(context.Context, ownership.Credential, []*Message) error) (*ownership.Runtime, error) {
	options.Manifest = options.Manifest.Normalize()
	m := options.Manifest
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if handler == nil || options.Key == nil || options.Activate == nil || options.InstanceID == "" || options.SessionID == "" || (options.Due == nil && !options.NoDueTasks) || (options.Due != nil && options.NoDueTasks) {
		return nil, ownership.ErrUnknown
	}
	if options.MaxOwned == 0 {
		options.MaxOwned = 4
	}
	store, err := r.OwnershipStore(ctx)
	if err != nil {
		return nil, err
	}
	actual, err := store.(*redisOwnershipStore).manifest(ctx, m.Scope)
	if err != nil {
		return nil, err
	}
	if actual.Fingerprint() != m.Fingerprint() {
		return nil, ownership.ErrUnknown
	}
	for _, subject := range m.Subjects {
		marker, err := r.client.Get(ctx, r.ownerLockKey(subject, m.Group)).Result()
		if err != nil {
			return nil, err
		}
		if marker != ownedRedisMarker(m) {
			return nil, ErrReliableGroupModeConflict
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	metrics := &redisReliableSubscriptionMetrics{configured: int64(options.MaxOwned), redisReliableDeliveryMetrics: &r.deliveryMetrics}
	metrics.prefetchCapacity.Store(int64(len(m.Subjects)) * m.Window)
	stop, err := r.registerRedisSubscription("ownership|"+m.Scope+"|"+options.InstanceID+"|"+options.SessionID, cancel, metrics)
	if err != nil {
		return nil, err
	}
	reader := newRedisOwnedReader(r, options, handler, metrics)
	runtime, err := ownership.StartRuntime(ctx, store, ownership.Options{Manifest: m, InstanceID: options.InstanceID, SessionID: options.SessionID, MaxOwned: options.MaxOwned, Activate: options.Activate, Restore: options.Restore, Step: reader.step, Observe: reader.observe})
	if err != nil {
		stop()
		r.wg.Done()
		return nil, err
	}
	go func() { defer r.wg.Done(); defer stop(); <-runtime.Done() }()
	return runtime, nil
}

// SupportsScopedOwnership 明确声明持续租约、批次和fenced ACK的可选能力。
func (*RedisStreamProvider) SupportsScopedOwnership() bool { return true }

func ownedRedisMarker(m ownership.Manifest) string {
	return fmt.Sprintf("%s%d:scope:%s", redisSharedModePrefix, m.Window, m.Fingerprint())
}
