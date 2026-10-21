// 本文件把业务批次的claim/ACK与持续归属及原组mode在Redis同一命令内隔离。
package mq

import (
	"context"
	"encoding/json"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/redis/go-redis/v9"
)

const redisOwnedBatchGuard = redisOwnershipCredentialGuard + `
if redis.call("HGET",KEYS[3],"ready")~="1" then return redis.error_reply("OWNER_NOT_READY") end
for i=4,#KEYS,2 do
 if redis.call("GET",KEYS[i])~=ARGV[8] then return redis.error_reply("OWNER_MODE_CONFLICT") end
 redis.call("XPENDING",KEYS[i+1],ARGV[9])
end
local messages=cjson.decode(ARGV[11])
`

const redisOwnedBatchACK = redisOwnedBatchGuard + `
local acked=0
for _,item in ipairs(messages) do acked=acked+redis.call("XACK",item[1],ARGV[9],item[2]) end
return acked`

func (r *redisOwnedReader) batchArguments(c ownership.Credential, messages []*Message) ([]string, []any, error) {
	store := &redisOwnershipStore{r.provider}
	keys := store.keys(c)
	for _, subject := range r.options.Manifest.Subjects {
		keys = append(keys, r.provider.ownerLockKey(subject, r.options.Manifest.Group), r.provider.streamKey(subject))
	}
	// Stream和Group只能由已冻结的框架声明提供，不能取自消息正文。
	allowed := map[string]bool{}
	for _, subject := range r.options.Manifest.Subjects {
		allowed[subject] = true
	}
	rows := make([][]string, 0, len(messages))
	for _, message := range messages {
		if !allowed[message.Subject] {
			return nil, nil, ownership.ErrUnknown
		}
		rows = append(rows, []string{r.provider.streamKey(message.Subject), message.ID})
	}
	data, err := json.Marshal(rows)
	if err != nil {
		return nil, nil, err
	}
	args := append(ownershipCredentialArgs(c), ownedRedisMarker(r.options.Manifest), r.options.Manifest.Group, r.subscribeOptions().Consumer, string(data))
	return keys, args, nil
}

func (r *redisOwnedReader) claim(ctx context.Context, c ownership.Credential, messages []*Message) error {
	keys, args, err := r.batchArguments(c, messages)
	if err != nil {
		return err
	}
	return ownershipRedisError(redis.NewScript(redisOwnedBatchGuard+`
for _,item in ipairs(messages) do if #redis.call("XPENDING",item[1],ARGV[9],item[2],item[2],1)~=1 then return redis.error_reply("OWNER_UNKNOWN") end end
for _,item in ipairs(messages) do redis.call("XCLAIM",item[1],ARGV[9],ARGV[10],0,item[2]) end
return 1`).Run(ctx, r.provider.client, keys, args...).Err())
}

func (r *redisOwnedReader) ack(ctx context.Context, c ownership.Credential, messages []*Message) error {
	keys, args, err := r.batchArguments(c, messages)
	if err != nil {
		return err
	}
	count, err := redis.NewScript(redisOwnedBatchACK).Run(ctx, r.provider.client, keys, args...).Int64()
	if err != nil {
		r.provider.deliveryMetrics.ackFailed.Add(1)
		return ownershipRedisError(err)
	}
	r.provider.deliveryMetrics.ackSucceeded.Add(count)
	return nil
}
