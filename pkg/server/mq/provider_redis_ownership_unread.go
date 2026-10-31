// 本文件在Broker端限制纯未读观测的正文大小，观测不建立PEL或推进游标。
package mq

import (
	"context"
	"github.com/redis/go-redis/v9"
)

func (r *redisOwnedReader) unread(ctx context.Context, subject, last string) ([]redis.XMessage, error) {
	const script = `
local rows=redis.call("XRANGE",KEYS[1],ARGV[1],"+","COUNT",ARGV[2])
local limit=tonumber(ARGV[3])
for _,row in ipairs(rows) do
 for i=1,#row[2],2 do if row[2][i]=="data" and string.len(row[2][i+1])>limit then return redis.error_reply("OWNER_CAPACITY") end end
end
return rows`
	m := r.options.Manifest
	cmd := redis.NewXMessageSliceCmd(ctx, "eval", script, 1, r.provider.streamKey(subject), "("+last, m.Window+1, m.MaxMessageBytes)
	_ = r.provider.client.Process(ctx, cmd)
	rows, err := cmd.Result()
	return rows, ownershipRedisError(err)
}
