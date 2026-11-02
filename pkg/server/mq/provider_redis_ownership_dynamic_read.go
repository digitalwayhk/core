// 本文件在一次Lua内核对原PEL/游标快照并按登记状态计费，保持W与U分别有界。
package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/redis/go-redis/v9"
	"time"
)

type redisOwnedClassification struct{ ID, Field, Key string }

func (r *redisOwnedReader) classify(rows []redis.XMessage, subject string) ([]redisOwnedClassification, error) {
	result := make([]redisOwnedClassification, 0, len(rows))
	for _, row := range rows {
		if len(redisMessageData(row.Values["data"])) > int(r.options.Manifest.MaxMessageBytes) {
			return nil, ownership.ErrCapacity
		}
		if len(redisMessageData(row.Values["ordering_key"])) == 0 {
			return nil, ownership.ErrUnknown
		}
		key, err := r.options.Key(&Message{ID: row.ID, Subject: subject, Data: redisMessageData(row.Values["data"])})
		if err != nil || key == "" || len(key) > 512 {
			return nil, ownership.ErrUnknown
		}
		result = append(result, redisOwnedClassification{row.ID, "key:" + ownershipDigest(key), key})
	}
	return result, nil
}

func (r *redisOwnedReader) dynamicSubjectMessages(ctx context.Context, subject string, read bool) ([]redis.XMessage, error) {
	m := r.options.Manifest
	p := r.provider
	options := r.subscribeOptions()
	stream := p.streamKey(subject)
	started := time.Now()
	rows, _, err := p.claimReliableKeyed(ctx, stream, subject, options, "0-0", m.PendingWindow())
	if err != nil {
		return nil, ownershipRedisError(err)
	}
	if !read {
		return rows, nil
	}
	r.metrics.reclaimRead.Add(int64(len(rows)))
	observeRedisKeyedRead(ctx, started, time.Now(), "pending_scan", stream, m.Group, rows)
	pending, err := p.client.XPending(ctx, stream, m.Group).Result()
	if err != nil {
		return nil, err
	}
	// 旧卷超出新预算时只恢复，不扩大；无法看全前序时不能准入后页。
	if pending.Count != int64(len(rows)) || pending.Count >= m.PendingWindow() {
		r.metrics.dynamicBackpressure.Add(1)
		return rows, nil
	}
	groups, err := p.client.XInfoGroups(ctx, stream).Result()
	if err != nil {
		return nil, err
	}
	last := ""
	for _, group := range groups {
		if group.Name == m.Group {
			last = group.LastDeliveredID
		}
	}
	if last == "" {
		return nil, ownership.ErrUnknown
	}
	candidates, err := r.unread(ctx, subject, last)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return rows, nil
	}
	if len(candidates) > int(m.Window) {
		candidates = candidates[:m.Window]
	}
	old, err := r.classify(rows, subject)
	if err != nil {
		return nil, err
	}
	next, err := r.classify(candidates, subject)
	if err != nil {
		return nil, err
	}
	oldJSON, err := json.Marshal(old)
	if err != nil {
		return nil, err
	}
	nextJSON, err := json.Marshal(next)
	if err != nil {
		return nil, err
	}
	base := p.ownershipBase(m.Scope)
	const script = `
if redis.call("GET",KEYS[2])~=ARGV[3] then return redis.error_reply("OWNER_MODE_CONFLICT") end
if redis.call("HGET",KEYS[3],"fingerprint")~=ARGV[10] then return redis.error_reply("OWNER_UNKNOWN") end
local seq=redis.call("GET",KEYS[4]);if not seq or not string.match(seq,"^[1-9][0-9]*$") then return redis.error_reply("OWNER_UNKNOWN") end
local count=redis.call("HGET",KEYS[3],"directory_count");local capacity=redis.call("HGET",KEYS[3],"directory_capacity")
local base=string.sub(KEYS[3],1,-6)
if redis.call("HGET",KEYS[3],"directory_mode")~="dynamic-v1" or not count or not capacity or not tonumber(count) or not tonumber(capacity) or tonumber(count)>tonumber(capacity) or redis.call("LLEN",base..":directory")~=tonumber(count) then return redis.error_reply("OWNER_UNKNOWN") end
local last=nil
for _,group in ipairs(redis.call("XINFO","GROUPS",KEYS[1])) do
 local name=nil;local delivered=nil
 for i=1,#group,2 do if group[i]=="name" then name=group[i+1] elseif group[i]=="last-delivered-id" then delivered=group[i+1] end end
 if name==ARGV[1] then last=delivered end
end
if not last then return redis.error_reply("OWNER_UNKNOWN") end
if last~=ARGV[4] then return nil end
local old=cjson.decode(ARGV[5]);local incoming=cjson.decode(ARGV[6]);local w=tonumber(ARGV[7]);local u=tonumber(ARGV[8])
local pending=redis.call("XPENDING",KEYS[1],ARGV[1],"-","+",w+u+1)
if #pending~=#old then return nil end
local registered=0;local unknown=0
local function known(item) return redis.call("HGET",KEYS[3],item.Field)==item.Key end
for i,item in ipairs(old) do
 if pending[i][1]~=item.ID then return nil end
 if known(item) then registered=registered+1 else unknown=unknown+1 end
end
local source=redis.call("XRANGE",KEYS[1],"("..last,"+","COUNT",#incoming)
if #source~=#incoming then return nil end
local allowed=0
for i,item in ipairs(incoming) do
 if source[i][1]~=item.ID then return nil end
 for j=1,#source[i][2],2 do if source[i][2][j]=="data" and string.len(source[i][2][j+1])>tonumber(ARGV[9]) then return redis.error_reply("OWNER_CAPACITY") end end
 if known(item) then if registered>=w then break end;registered=registered+1 else if unknown>=u then break end;unknown=unknown+1 end
 if registered+unknown>w+u then break end
 allowed=allowed+1
end
if allowed==0 then return nil end
return redis.call("XREADGROUP","GROUP",ARGV[1],ARGV[2],"COUNT",allowed,"STREAMS",KEYS[1],">")`
	args := []any{"eval", script, 4, stream, p.ownerLockKey(subject, m.Group), base + ":meta", base + ":sequence", m.Group, options.Consumer, ownedRedisMarker(m), last, string(oldJSON), string(nextJSON), m.Window, m.DynamicKeys.UnregisteredWindow, m.MaxMessageBytes, m.Fingerprint()}
	cmd := redis.NewXStreamSliceCmd(ctx, args...)
	_ = p.client.Process(ctx, cmd)
	entries, err := cmd.Result()
	if err != nil && err != redis.Nil {
		return nil, ownershipRedisError(err)
	}
	if len(entries) == 0 {
		r.metrics.dynamicBackpressure.Add(1)
	}
	for _, entry := range entries {
		r.metrics.newRead.Add(int64(len(entry.Messages)))
		for _, row := range entry.Messages {
			r.metrics.publishToReadNanos.Add(redisMessageAge(row.ID, time.Now()).Nanoseconds())
		}
		observeRedisKeyedRead(ctx, started, time.Now(), "new", stream, m.Group, entry.Messages)
	}
	rows, _, err = p.claimReliableKeyed(ctx, stream, subject, options, "0-0", m.PendingWindow())
	if err != nil {
		return nil, ownershipRedisError(err)
	}
	if int64(len(rows)) > m.PendingWindow() {
		return nil, fmt.Errorf("%w: dynamic PEL budget", ownership.ErrCapacity)
	}
	return rows, nil
}
