// 本文件仅保留有限PEL身份提示；正文准入时从原Stream读取，绝不以提示确认消息。
package mq

import (
	"context"
	"errors"
	"github.com/redis/go-redis/v9"
)

// 已知项仍返回原PEL ID，供调用方删除消失提示；占位仅存在本次命令响应，不写Broker。
const redisPendingHintMaxKeyBytes = 4096

const redisSharedPendingHintScript = `
if redis.call("GET",KEYS[2])~=ARGV[4] then return redis.error_reply("SHARED_GROUP_MODE_LOST") end
local start="-"
if ARGV[2]~="0-0" then start="("..ARGV[2] end
local pending=redis.call("XPENDING",KEYS[1],ARGV[1],start,"+",ARGV[3])
local messages={};local skip={};local skipCount=tonumber(ARGV[5]) or 0
for i=1,skipCount do skip[ARGV[5+i]]=true end
for _,entry in ipairs(pending) do
 if skip[entry[1]] then table.insert(messages,{entry[1],{"__core_pending_hint","1"}})
 else
  local rows=redis.call("XRANGE",KEYS[1],entry[1],entry[1],"COUNT",1)
  if #rows==0 then return redis.error_reply("SHARED_PENDING_BODY_MISSING") end
  table.insert(messages,rows[1])
 end
end
local next="0-0"
if #pending==tonumber(ARGV[3]) then next=pending[#pending][1] end
return {next,messages,{}}
`

func redisPendingHint(row redis.XMessage) bool {
	_, body := row.Values["data"]
	return !body && row.Values["__core_pending_hint"] == "1"
}

// pendingBody保留Redis端原件缺失错误与成熟XAutoClaim响应解码，不改变PEL。
func (r *RedisStreamProvider) pendingBody(ctx context.Context, stream, id string) (redis.XMessage, error) {
	const script = `local rows=redis.call("XRANGE",KEYS[1],ARGV[1],ARGV[1],"COUNT",1)
 if #rows~=1 then return redis.error_reply("SHARED_PENDING_BODY_MISSING") end
 return {"0-0",rows,{}}`
	cmd := redis.NewXAutoClaimCmd(ctx, "eval", script, 1, stream, id)
	_ = r.client.Process(ctx, cmd)
	rows, _, err := cmd.Result()
	if err != nil {
		return redis.XMessage{}, err
	}
	if len(rows) != 1 || rows[0].ID != id {
		return redis.XMessage{}, errors.New("SHARED_PENDING_BODY_MISSING")
	}
	return rows[0], nil
}
