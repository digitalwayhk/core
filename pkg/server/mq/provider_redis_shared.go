package mq

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisSharedModePrefix = "shared-key-v1:"

var errSharedKeyBusy = errors.New("redis-stream: shared key busy or already completed")

var redisLegacyAdmissionScript = redis.NewScript(`
local current=redis.call("GET",KEYS[1])
if current and string.sub(current,1,string.len(ARGV[1]))==ARGV[1] then return 0 end
if not current then redis.call("SET",KEYS[1],ARGV[2],"PX",ARGV[3]) end
return 1
`)

var redisSharedModeScript = redis.NewScript(`
local current=redis.call("GET",KEYS[1])
if current and current~=ARGV[1] then return 0 end
redis.call("SET",KEYS[1],ARGV[1])
return 1
`)

// 分页只观察 PEL，不夺走其他 key 的归属。执行时才 fenced XCLAIM。
const redisSharedPendingScript = `
if redis.call("GET",KEYS[2])~=ARGV[4] then return redis.error_reply("SHARED_GROUP_MODE_LOST") end
local start="-"
if ARGV[2]~="0-0" then start="("..ARGV[2] end
local pending=redis.call("XPENDING",KEYS[1],ARGV[1],start,"+",ARGV[3])
local messages={}
local next="0-0"
for _,entry in ipairs(pending) do
  local rows=redis.call("XRANGE",KEYS[1],entry[1],entry[1],"COUNT",1)
  if #rows==0 then return redis.error_reply("SHARED_PENDING_BODY_MISSING") end
  table.insert(messages,rows[1])
end
if #pending==tonumber(ARGV[3]) then next=pending[#pending][1] end
return {next,messages,{}}
`

// PEL 数检查与读取在同一命令内，多个实例不能各自突破共享预取预算。
const redisSharedReadScript = `
if redis.call("GET",KEYS[2])~=ARGV[4] then return redis.error_reply("SHARED_GROUP_MODE_LOST") end
local pending=redis.call("XPENDING",KEYS[1],ARGV[1])
local count=math.min(tonumber(ARGV[3]),tonumber(ARGV[5])-pending[1])
if count<=0 then return nil end
return redis.call("XREADGROUP","GROUP",ARGV[1],ARGV[2],"COUNT",count,"STREAMS",KEYS[1],">")
`

var redisSharedAcquireScript = redis.NewScript(`
if redis.call("GET",KEYS[3])~=ARGV[6] then return redis.error_reply("SHARED_GROUP_MODE_LOST") end
local current=redis.call("GET",KEYS[2])
if current and current~=ARGV[3] then return 0 end
local pending=redis.call("XPENDING",KEYS[1],ARGV[1],ARGV[2],ARGV[2],1)
if #pending==0 then return 2 end
redis.call("SET",KEYS[2],ARGV[3],"PX",ARGV[4])
local claimed=redis.call("XCLAIM",KEYS[1],ARGV[1],ARGV[5],0,ARGV[2])
if #claimed~=1 then return redis.error_reply("SHARED_PENDING_BODY_MISSING") end
return 1
`)

var redisSharedReleaseScript = redis.NewScript(`
if redis.call("GET",KEYS[1])~=ARGV[1] then return 0 end
if tonumber(ARGV[2])>0 then return redis.call("PEXPIRE",KEYS[1],ARGV[2]) end
return redis.call("DEL",KEYS[1])
`)

func (*RedisStreamProvider) SharedKeyReliableInfo() SharedKeyReliableCapability {
	return DefaultSharedKeyReliableCapability()
}

func sharedRedisWindow(options ReliableSubscribeOptions) int64 {
	window := options.Count
	if options.lifecycle != nil && options.lifecycle.Retry.MaxAckPending > 0 && int64(options.lifecycle.Retry.MaxAckPending) < window {
		window = int64(options.lifecycle.Retry.MaxAckPending)
	}
	return window
}
func redisSharedMarker(options ReliableSubscribeOptions) string {
	if options.scopeMarker != "" {
		return options.scopeMarker
	}
	return fmt.Sprintf("%s%d", redisSharedModePrefix, sharedRedisWindow(options))
}

func (r *RedisStreamProvider) ensureReliableGroupMode(ctx context.Context, subject string, options ReliableSubscribeOptions) error {
	key := r.ownerLockKey(subject, options.Group)
	if !options.SharedKeys {
		n, err := redisLegacyAdmissionScript.Run(ctx, r.client, []string{key}, redisSharedModePrefix, options.Consumer, ownerLeaseTTL(options).Milliseconds()).Int()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrReliableGroupModeConflict
		}
		return nil
	}
	n, err := redisSharedModeScript.Run(ctx, r.client, []string{key}, redisSharedMarker(options)).Int()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrReliableGroupModeConflict
	}
	return nil
}

func (r *RedisStreamProvider) sharedOrderingKeyLock(subject, group, key string) string {
	digest := sha256.Sum256([]byte(key))
	return r.ownerLockKey(subject, group) + ":key:" + hex.EncodeToString(digest[:])
}
func (r *RedisStreamProvider) reliableOwnerKey(subject string, options ReliableSubscribeOptions) string {
	if options.ownerKey != "" {
		return options.ownerKey
	}
	return r.ownerLockKey(subject, options.Group)
}
func reliableOwnerToken(options ReliableSubscribeOptions) string {
	if options.ownerToken != "" {
		return options.ownerToken
	}
	return options.Consumer
}

// 每次实际执行持有独立 token；等待中的 work 不占分键租约，其他实例可以接手。
func (r *RedisStreamProvider) processSharedReliableMessage(ctx context.Context, streamKey, subject string, options ReliableSubscribeOptions, item redis.XMessage, handler func(*Message) error) (result error) {
	key := string(redisMessageData(item.Values["ordering_key"]))
	options.ownerKey = r.sharedOrderingKeyLock(subject, options.Group, key)
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return err
	}
	options.ownerToken = hex.EncodeToString(token[:])
	ttl := ownerLeaseTTL(options)
	acquired, err := redisSharedAcquireScript.Run(ctx, r.client, []string{streamKey, options.ownerKey, r.ownerLockKey(subject, options.Group)}, options.Group, item.ID, options.ownerToken, ttl.Milliseconds(), options.Consumer, redisSharedMarker(options)).Int()
	if err != nil {
		return err
	}
	if acquired != 1 {
		r.deliveryMetrics.sharedContended.Add(1)
		return &redisReliableRetryError{cause: errSharedKeyBusy, delay: 50 * time.Millisecond}
	}
	r.deliveryMetrics.sharedAcquired.Add(1)
	r.deliveryMetrics.sharedLeases.Add(1)
	defer r.deliveryMetrics.sharedLeases.Add(-1)
	workCtx, cancel := context.WithCancel(ctx)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-workCtx.Done():
				return
			case <-ticker.C:
				if !r.renewOwner(workCtx, subject, options) {
					r.deliveryMetrics.sharedOwnerLost.Add(1)
					cancel()
					return
				}
			}
		}
	}()
	defer func() {
		cancel()
		<-stopped
		releaseCtx, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		delay := int64(0)
		// 失败退避也保存在 Broker lease 上，其他副本不能绕过本次 Backoff。
		if result != nil {
			delay = redisReliableRetryDelay(result).Milliseconds()
		}
		_ = redisSharedReleaseScript.Run(releaseCtx, r.client, []string{options.ownerKey}, options.ownerToken, delay).Err()
	}()
	return r.processReliableMessage(workCtx, streamKey, subject, options, item, handler)
}
