// 本文件在既有Redis命令池上存储有界WS引用快照，版本拒绝迟到退订覆盖。
package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/redis/go-redis/v9"
	"sort"
	"strconv"
	"time"
)

type redisOwnershipReference struct {
	KeyField   string
	KeyValue   string
	Meta       string
	Sequence   string
	Refs       string
	Field      string
	GatewayKey string
	Reference  ownership.Reference
}

// OpenGateway 开启有界Gateway会话，不为每个订阅新建连接。
func (s *redisOwnershipStore) OpenGateway(ctx context.Context, id string, ttl time.Duration) error {
	if id == "" || len(id) > 256 || ttl <= 0 || ttl > time.Hour {
		return ownership.ErrUnknown
	}
	return ownershipRedisError(redis.NewScript(`
local now=redis.call("TIME");local ms=now[1]*1000+math.floor(now[2]/1000)
redis.call("ZREMRANGEBYSCORE",KEYS[2],"-inf",ms)
if redis.call("EXISTS",KEYS[1])~=0 then return redis.error_reply("OWNER_BUSY") end
if redis.call("ZCARD",KEYS[2])>=1024 then return redis.error_reply("OWNER_CAPACITY") end
redis.call("HSET",KEYS[1],"revision","0","digest","")
redis.call("PEXPIRE",KEYS[1],ARGV[1]);redis.call("ZADD",KEYS[2],ms+tonumber(ARGV[1]),KEYS[1]);return 1`).Run(ctx, s.provider.client, []string{s.provider.ownershipGateway(id), s.provider.prefix + ":ownership-gateways"}, ttl.Milliseconds()).Err())
}

// ReplaceGateway 按版本原子替换一个Gateway的全量语义引用。
func (s *redisOwnershipStore) ReplaceGateway(ctx context.Context, snapshot ownership.GatewaySnapshot) error {
	if snapshot.GatewayID == "" || len(snapshot.GatewayID) > 256 || snapshot.Revision <= 0 || snapshot.Lease <= 0 || snapshot.Lease > time.Hour || len(snapshot.References) > 1024 {
		return ownership.ErrCapacity
	}
	values := make([]redisOwnershipReference, 0, len(snapshot.References))
	seen := map[string]bool{}
	for _, ref := range snapshot.References {
		if ref.Scope == "" || ref.Key == "" || ref.SessionID == "" || ref.Name == "" || len(ref.SessionID) > 256 || len(ref.Name) > 512 {
			return ownership.ErrUnknown
		}
		base := s.provider.ownershipBase(ref.Scope)
		field := ownershipDigest(snapshot.GatewayID + "\x00" + ref.SessionID + "\x00" + ref.Name)
		refs := s.provider.ownershipRefs(ref.Scope, ref.Key)
		id := refs + ":" + field
		if seen[id] {
			return ownership.ErrUnknown
		}
		seen[id] = true
		values = append(values, redisOwnershipReference{KeyField: "key:" + ownershipDigest(ref.Key), KeyValue: ref.Key, Meta: base + ":meta", Sequence: base + ":sequence", Refs: refs, Field: field, GatewayKey: s.provider.ownershipGateway(snapshot.GatewayID), Reference: ownership.Reference{GatewayID: snapshot.GatewayID, SessionID: ref.SessionID, Name: ref.Name}})
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Refs+values[i].Field < values[j].Refs+values[j].Field })
	raw, err := json.Marshal(values)
	if err != nil {
		return err
	}
	key := s.provider.ownershipGateway(snapshot.GatewayID)
	return ownershipRedisError(redis.NewScript(`
if redis.call("EXISTS",KEYS[1])~=1 then return redis.error_reply("OWNER_UNKNOWN") end
local revision=redis.call("HGET",KEYS[1],"revision")
if #revision>#ARGV[1] or (#revision==#ARGV[1] and revision>ARGV[1]) then return redis.error_reply("OWNER_STALE_REFERENCE") end
if revision==ARGV[1] and redis.call("HGET",KEYS[1],"digest")~=ARGV[2] then return redis.error_reply("OWNER_STALE_REFERENCE") end
local now=redis.call("TIME");local ms=now[1]*1000+math.floor(now[2]/1000)
if revision==ARGV[1] then redis.call("PEXPIRE",KEYS[1],ARGV[3]);redis.call("PEXPIRE",KEYS[2],tonumber(ARGV[3])*2);redis.call("ZADD",KEYS[3],ms+tonumber(ARGV[3]),KEYS[1]);return 1 end
local incoming=cjson.decode(ARGV[4]);local desired={};local counts={};local limits={};local sequences={}
local function validsequence(key)
 local v=redis.call("GET",key);return v and string.match(v,"^[1-9][0-9]*$") and #v<=19
end
for _,item in ipairs(incoming) do
 if redis.call("EXISTS",item.Meta)~=1 or not validsequence(item.Sequence) or redis.call("HGET",item.Meta,item.KeyField)~=item.KeyValue then return redis.error_reply("OWNER_UNKNOWN") end
 local id=item.Refs..":"..item.Field;desired[id]=item;sequences[item.Sequence]=true
 if not counts[item.Refs] then counts[item.Refs]=redis.call("HLEN",item.Refs) end
 limits[item.Refs]=tonumber(redis.call("HGET",item.Meta,"references"))
 if redis.call("HEXISTS",item.Refs,item.Field)==0 then counts[item.Refs]=counts[item.Refs]+1 end
end
local previous=redis.call("HGETALL",KEYS[2])
for i=1,#previous,2 do
 local item=cjson.decode(previous[i+1]);if redis.call("EXISTS",item.Meta)~=1 or not validsequence(item.Sequence) then return redis.error_reply("OWNER_UNKNOWN") end
 sequences[item.Sequence]=true
 if counts[item.Refs] and not desired[previous[i]] and redis.call("HEXISTS",item.Refs,item.Field)==1 then counts[item.Refs]=counts[item.Refs]-1 end
end
for refs,count in pairs(counts) do if not limits[refs] or count>limits[refs] then return redis.error_reply("OWNER_CAPACITY") end end
-- 先预留序列；溢出至多留下epoch间隙，不会部分覆盖引用。
for sequence,_ in pairs(sequences) do redis.call("INCR",sequence) end
for i=1,#previous,2 do if not desired[previous[i]] then local item=cjson.decode(previous[i+1]);redis.call("HDEL",item.Refs,item.Field) end end
redis.call("DEL",KEYS[2])
for id,item in pairs(desired) do local value=cjson.encode(item);redis.call("HSET",item.Refs,item.Field,value);redis.call("HSET",KEYS[2],id,value) end
redis.call("HSET",KEYS[1],"revision",ARGV[1],"digest",ARGV[2]);redis.call("PEXPIRE",KEYS[1],ARGV[3]);redis.call("PEXPIRE",KEYS[2],tonumber(ARGV[3])*2)
redis.call("ZADD",KEYS[3],ms+tonumber(ARGV[3]),KEYS[1]);return 1`).Run(ctx, s.provider.client, []string{key, key + ":index", s.provider.prefix + ":ownership-gateways"}, strconv.FormatInt(snapshot.Revision, 10), ownershipDigest(string(raw)), snapshot.Lease.Milliseconds(), string(raw)).Err())
}

// CloseGateway 使本Gateway引用失效，其他Gateway引用继续有效。
func (s *redisOwnershipStore) CloseGateway(ctx context.Context, id string) error {
	if id == "" {
		return ownership.ErrUnknown
	}
	return s.provider.client.Del(ctx, s.provider.ownershipGateway(id)).Err()
}

// References 过滤失效会话并返回有界、带正版本的引用快照。
func (s *redisOwnershipStore) References(ctx context.Context, scope, key string) (ownership.ReferenceSnapshot, error) {
	base := s.provider.ownershipBase(scope)
	values, err := redis.NewScript(redisOwnershipMetadataGuard+`
local limit=tonumber(redis.call("HGET",KEYS[1],"references"))
if not limit or redis.call("HLEN",KEYS[3])>limit then return redis.error_reply("OWNER_CAPACITY") end
local rows=redis.call("HGETALL",KEYS[3]);local live={};local stale={}
for i=1,#rows,2 do local ok,item=pcall(cjson.decode,rows[i+1]);if not ok then return redis.error_reply("OWNER_UNKNOWN") end
 if redis.call("EXISTS",item.GatewayKey)==1 then table.insert(live,cjson.encode(item.Reference)) else table.insert(stale,rows[i]) end
end
if #stale>0 then redis.call("INCR",KEYS[2]);for _,field in ipairs(stale) do redis.call("HDEL",KEYS[3],field) end end
return {redis.call("GET",KEYS[2]),live}`).Run(ctx, s.provider.client, []string{base + ":meta", base + ":sequence", s.provider.ownershipRefs(scope, key)}, "key:"+ownershipDigest(key), key).Slice()
	if err != nil {
		return ownership.ReferenceSnapshot{}, ownershipRedisError(err)
	}
	if len(values) != 2 {
		return ownership.ReferenceSnapshot{}, ownership.ErrUnknown
	}
	revision, err := strconv.ParseInt(fmt.Sprint(values[0]), 10, 64)
	if err != nil || revision <= 0 {
		return ownership.ReferenceSnapshot{}, ownership.ErrUnknown
	}
	result := ownership.ReferenceSnapshot{Revision: revision}
	rows, ok := values[1].([]any)
	if !ok {
		return result, ownership.ErrUnknown
	}
	for _, raw := range rows {
		var ref ownership.Reference
		if json.Unmarshal([]byte(fmt.Sprint(raw)), &ref) != nil {
			return result, ownership.ErrUnknown
		}
		result.References = append(result.References, ref)
	}
	return result, nil
}
