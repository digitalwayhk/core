// 本文件提供 Redis 生命周期扫描前的跨周期租约与轻量容量读取。
package mq

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

func (r *RedisStreamProvider) releaseLifecycleRound(ctx context.Context, policy LifecyclePolicy, owner string) error {
	return redisOwnerReleaseScript.Run(ctx, r.client, []string{r.lifecycleOwnerKey(policy.Subject)}, owner).Err()
}

func (r *RedisStreamProvider) acquireLifecycleRound(ctx context.Context, policy LifecyclePolicy, owner string) (context.Context, bool, error) {
	if r == nil || r.client == nil {
		return ctx, false, ErrNotConnected
	}
	key := r.lifecycleOwnerKey(policy.Subject)
	current, err := r.client.Get(ctx, key).Result()
	if err != nil && err != redis.Nil {
		return ctx, false, err
	}
	if err == nil && current != owner {
		return ctx, false, nil
	}
	acquired := 0
	if err == redis.Nil {
		ok, acquireErr := r.client.SetNX(ctx, key, owner, lifecycleWorkerLeaseTTL(policy)).Result()
		err = acquireErr
		if ok {
			acquired = 1
		}
	} else {
		// GET 仅过滤 standby；续约仍原子复核，不能依赖前一个读操作的结果。
		acquired, err = redisOwnerRefreshScript.Run(ctx, r.client, []string{key}, owner, lifecycleWorkerLeaseTTL(policy).Milliseconds()).Int()
	}
	if err != nil || acquired != 1 {
		return ctx, false, err
	}
	lease := lifecycleRoundLease{provider: r, subject: policy.Subject, owner: owner}
	return context.WithValue(ctx, lifecycleRoundKey{}, lease), true, nil
}

func (r *RedisStreamProvider) inspectLifecycleCapacity(ctx context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error) {
	if r == nil || r.client == nil {
		return LifecycleSnapshot{}, ErrNotConnected
	}
	// XLEN 对不存在的 key 返回零，必须另行确认类型和策略，不能把数据丢失当空闲。
	pipe := r.client.Pipeline()
	kind := pipe.Type(ctx, r.streamKey(policy.Subject))
	fingerprint := pipe.HGet(ctx, r.lifecycleMetaKey(policy.Subject), "fingerprint")
	count := pipe.XLen(ctx, r.streamKey(policy.Subject))
	size := pipe.MemoryUsage(ctx, r.streamKey(policy.Subject))
	if _, err := pipe.Exec(ctx); err != nil {
		return LifecycleSnapshot{}, err
	}
	if kind.Val() != "stream" {
		return LifecycleSnapshot{}, ErrLifecycleStateUncertain
	}
	if fingerprint.Val() != policy.Fingerprint() {
		return LifecycleSnapshot{}, fmt.Errorf("%w: capacity fingerprint", ErrLifecyclePolicyConflict)
	}
	retained, bytes := count.Val(), size.Val()
	return LifecycleSnapshot{Subject: policy.Subject, PolicyFingerprint: policy.Fingerprint(), ObservedAt: time.Now(), State: LifecycleStatePartial, RetainedMessages: &retained, RetainedBytes: &bytes}, nil
}
