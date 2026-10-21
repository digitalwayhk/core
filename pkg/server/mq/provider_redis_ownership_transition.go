// 本文件有界枚举原PEL及其执行租约，给mode切换提供可原子复核的维护快照。
package mq

import (
	"context"
	"encoding/json"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/redis/go-redis/v9"
)

type redisOwnershipTransitionPending struct {
	Stream string
	IDs    []string
}

func (r *RedisStreamProvider) ownershipTransitionSnapshot(ctx context.Context, m ownership.Manifest) (string, []string, error) {
	rows := []redisOwnershipTransitionPending{}
	locks := []string{}
	seen := map[string]bool{}
	for _, subject := range m.Subjects {
		key := r.streamKey(subject)
		pending, err := r.client.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: key, Group: m.Group, Start: "-", End: "+", Count: m.Window + 1}).Result()
		if err != nil {
			return "", nil, err
		}
		if int64(len(pending)) > m.Window {
			return "", nil, ownership.ErrBusy
		}
		row := redisOwnershipTransitionPending{Stream: key, IDs: []string{}}
		for _, item := range pending {
			body, err := r.client.XRangeN(ctx, key, item.ID, item.ID, 1).Result()
			if err != nil {
				return "", nil, err
			}
			if len(body) != 1 {
				return "", nil, ownership.ErrUnknown
			}
			ordering := string(redisMessageData(body[0].Values["ordering_key"]))
			lock := r.sharedOrderingKeyLock(subject, m.Group, ordering)
			if !seen[lock] {
				seen[lock] = true
				locks = append(locks, lock)
			}
			row.IDs = append(row.IDs, item.ID)
		}
		rows = append(rows, row)
	}
	data, err := json.Marshal(rows)
	return string(data), locks, err
}
