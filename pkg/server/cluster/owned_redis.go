// 本文件以单次 Lua 原子操作保护 owner 和实际 Redis 注册记录。
package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/redis/go-redis/v9"
	"time"
)

var ownedRedisWrite = redis.NewScript(`
local owner=redis.call('GET',KEYS[1])
if not owner and ARGV[6]=='0' then return -2 end
if owner and owner~=ARGV[1] and (ARGV[6]=='0' or string.sub(owner,1,20)>=string.sub(ARGV[1],1,20)) then return -1 end
redis.call('SET',KEYS[1],ARGV[1])
redis.call('SET',KEYS[2],ARGV[2],'PX',ARGV[3])
redis.call('SET',KEYS[3],ARGV[4],'PX',ARGV[3])
redis.call('SADD',KEYS[4],ARGV[5])
redis.call('XADD',KEYS[5],'MAXLEN','~',ARGV[7],'*','service',ARGV[4],'action','upsert','node_id',ARGV[5])
return 1
`)
var ownedRedisDelete = redis.NewScript(`
local owner=redis.call('GET',KEYS[1])
if not owner then return -2 end
if owner~=ARGV[1] then return -1 end
redis.call('DEL',KEYS[2],KEYS[3])
redis.call('SREM',KEYS[4],ARGV[3])
redis.call('XADD',KEYS[5],'MAXLEN','~',ARGV[4],'*','service',ARGV[2],'action','delete','node_id',ARGV[3])
return 1
`)

func ownedRedisResult(n int, err error) error {
	if err != nil {
		return err
	}
	if n == -1 {
		return ErrRegistrationFenced
	}
	if n == -2 {
		return ErrRegistrationAuthorityMissing
	}
	return nil
}
func (p *RedisProvider) ownedKeys(n *NodeInfo) []string {
	return []string{p.prefix + ":owners:" + n.ID, p.nodeKey(n.ServiceName, n.ID), p.indexKey(n.ID), p.serviceKey(n.ServiceName), p.eventsKey()}
}
func (p *RedisProvider) writeOwned(ctx context.Context, node *NodeInfo, o RegistrationOwner, acquire bool) error {
	if p.closed.Load() {
		return errors.New("cluster: provider closed")
	}
	n, err := ownedNode(node, o)
	if err != nil {
		return err
	}
	data, err := json.Marshal(n)
	if err != nil {
		return err
	}
	flag := "0"
	if acquire {
		flag = "1"
	}
	value, err := ownedRedisWrite.Run(ctx, p.client, p.ownedKeys(n), o.token(), data, p.ttl.Milliseconds(), n.ServiceName, n.ID, flag, redisDiscoveryEventMaxLen).Int()
	return ownedRedisResult(value, err)
}

// AcquireRegistration 原子发布新世代及节点，不占用 MachineID 槽位。
func (p *RedisProvider) AcquireRegistration(ctx context.Context, n *NodeInfo, o RegistrationOwner) error {
	return p.writeOwned(ctx, n, o, true)
}

// RenewRegistration 只允许同一持久 owner 续期或恢复已过期节点。
func (p *RedisProvider) RenewRegistration(ctx context.Context, n *NodeInfo, o RegistrationOwner) error {
	return p.writeOwned(ctx, n, o, false)
}

// ReleaseRegistration 原子保护注销，不删除持久 owner。
func (p *RedisProvider) ReleaseRegistration(ctx context.Context, n *NodeInfo, o RegistrationOwner) error {
	if _, err := ownedNode(n, o); err != nil {
		return err
	}
	value, err := ownedRedisDelete.Run(ctx, p.client, p.ownedKeys(n), o.token(), n.ServiceName, n.ID, redisDiscoveryEventMaxLen).Int()
	return ownedRedisResult(value, err)
}

// RegistrationTTL 返回实际注册有效期，供本地准入保守计时。
func (p *RedisProvider) RegistrationTTL() time.Duration { return p.ttl }
