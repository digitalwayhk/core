package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisDiscoveryConnectTimeout = 2 * time.Second

var redisRegisterNodeScript = redis.NewScript(`
local owner = redis.call('GET', KEYS[1])
if owner and owner ~= ARGV[1] then
  return 0
end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[3])
redis.call('SET', KEYS[2], ARGV[2], 'PX', ARGV[3])
redis.call('SET', KEYS[3], ARGV[4], 'PX', ARGV[3])
redis.call('SADD', KEYS[4], ARGV[1])
redis.call('XADD', KEYS[5], '*', 'service', ARGV[4], 'action', 'upsert', 'node_id', ARGV[1])
return 1
`)

var redisDeregisterNodeScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  redis.call('DEL', KEYS[1])
end
redis.call('DEL', KEYS[2])
redis.call('DEL', KEYS[3])
redis.call('SREM', KEYS[4], ARGV[1])
redis.call('XADD', KEYS[5], '*', 'service', ARGV[2], 'action', 'delete', 'node_id', ARGV[1])
return 1
`)

// RedisProvider 使用带 TTL 的节点键保存服务成员，并通过 Stream 唤醒 Watch。
// Watch 同时周期对账，因此不依赖 Redis keyspace notification 才能观察异常退出。
type RedisProvider struct {
	client *redis.Client
	prefix string
	ttl    time.Duration

	ctx       context.Context
	cancel    context.CancelFunc
	closed    atomic.Bool
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// NewRedisProvider 创建并验证 Redis 服务发现连接。
func NewRedisProvider(addr string, db int, prefix string, ttl time.Duration) (*RedisProvider, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, errors.New("redis discovery address is empty")
	}
	if prefix = strings.TrimSpace(prefix); prefix == "" {
		prefix = "core:discovery"
	}
	if ttl <= 0 {
		ttl = 10 * time.Second
	}
	client := redis.NewClient(&redis.Options{Addr: addr, DB: db})
	ctx, cancelConnect := context.WithTimeout(context.Background(), redisDiscoveryConnectTimeout)
	err := client.Ping(ctx).Err()
	cancelConnect()
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connect redis discovery %s: %w", addr, err)
	}
	providerCtx, cancel := context.WithCancel(context.Background())
	return &RedisProvider{client: client, prefix: prefix, ttl: ttl, ctx: providerCtx, cancel: cancel}, nil
}

func (p *RedisProvider) Name() string { return "redis" }

func (p *RedisProvider) Register(ctx context.Context, node *NodeInfo) error {
	if node == nil || strings.TrimSpace(node.ID) == "" || strings.TrimSpace(node.ServiceName) == "" {
		return errors.New("redis discovery node id and service are required")
	}
	if p.closed.Load() {
		return errors.New("redis discovery provider is closed")
	}
	copyNode := cloneNode(node)
	now := time.Now().UTC()
	if copyNode.RegisteredAt.IsZero() {
		copyNode.RegisteredAt = now
	}
	copyNode.LastHeartbeat = now
	copyNode.Status = NodeStatusRunning
	if copyNode.Weight <= 0 {
		copyNode.Weight = 1
	}
	data, err := json.Marshal(copyNode)
	if err != nil {
		return err
	}
	result, err := redisRegisterNodeScript.Run(ctx, p.client,
		[]string{p.slotKey(copyNode), p.nodeKey(copyNode.ServiceName, copyNode.ID), p.indexKey(copyNode.ID), p.serviceKey(copyNode.ServiceName), p.eventsKey()},
		copyNode.ID, data, p.ttl.Milliseconds(), copyNode.ServiceName,
	).Int()
	if err != nil {
		return fmt.Errorf("redis discovery register %s: %w", copyNode.ID, err)
	}
	if result == 0 {
		return fmt.Errorf("%w: service=%s datacenter=%d machine=%d", ErrSlotConflict, copyNode.ServiceName, copyNode.DataCenterID, copyNode.MachineID)
	}
	*node = *copyNode
	return nil
}

func (p *RedisProvider) Deregister(ctx context.Context, nodeID string) error {
	node, err := p.Get(ctx, nodeID)
	if err != nil {
		return err
	}
	_, err = redisDeregisterNodeScript.Run(ctx, p.client,
		[]string{p.slotKey(node), p.nodeKey(node.ServiceName, node.ID), p.indexKey(node.ID), p.serviceKey(node.ServiceName), p.eventsKey()},
		node.ID, node.ServiceName,
	).Result()
	if err != nil {
		return fmt.Errorf("redis discovery deregister %s: %w", nodeID, err)
	}
	return nil
}

func (p *RedisProvider) Heartbeat(ctx context.Context, nodeID string) error {
	node, err := p.Get(ctx, nodeID)
	if err != nil {
		return err
	}
	return p.Register(ctx, node)
}

func (p *RedisProvider) Get(ctx context.Context, nodeID string) (*NodeInfo, error) {
	serviceName, err := p.client.Get(ctx, p.indexKey(nodeID)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNodeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("redis discovery get node index %s: %w", nodeID, err)
	}
	data, err := p.client.Get(ctx, p.nodeKey(serviceName, nodeID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNodeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("redis discovery get node %s: %w", nodeID, err)
	}
	node := &NodeInfo{}
	if err := json.Unmarshal(data, node); err != nil {
		return nil, fmt.Errorf("redis discovery decode node %s: %w", nodeID, err)
	}
	return node, nil
}

func (p *RedisProvider) List(ctx context.Context, serviceName string, statuses ...NodeStatus) ([]*NodeInfo, error) {
	if strings.TrimSpace(serviceName) == "" {
		return p.listAll(ctx, statuses...)
	}
	ids, err := p.client.SMembers(ctx, p.serviceKey(serviceName)).Result()
	if err != nil {
		return nil, fmt.Errorf("redis discovery list %s: %w", serviceName, err)
	}
	return p.loadNodes(ctx, serviceName, ids, statuses...)
}

func (p *RedisProvider) listAll(ctx context.Context, statuses ...NodeStatus) ([]*NodeInfo, error) {
	var cursor uint64
	var result []*NodeInfo
	pattern := p.prefix + ":services:*"
	for {
		keys, next, err := p.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, fmt.Errorf("redis discovery list services: %w", err)
		}
		for _, key := range keys {
			serviceName := strings.TrimPrefix(key, p.prefix+":services:")
			nodes, err := p.List(ctx, serviceName, statuses...)
			if err != nil {
				return nil, err
			}
			result = append(result, nodes...)
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return result, nil
}

func (p *RedisProvider) loadNodes(ctx context.Context, serviceName string, ids []string, statuses ...NodeStatus) ([]*NodeInfo, error) {
	statusSet := make(map[NodeStatus]struct{}, len(statuses))
	for _, status := range statuses {
		statusSet[status] = struct{}{}
	}
	result := make([]*NodeInfo, 0, len(ids))
	for _, id := range ids {
		data, err := p.client.Get(ctx, p.nodeKey(serviceName, id)).Bytes()
		if errors.Is(err, redis.Nil) {
			_ = p.client.SRem(ctx, p.serviceKey(serviceName), id).Err()
			_ = p.client.Del(ctx, p.indexKey(id)).Err()
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("redis discovery load node %s: %w", id, err)
		}
		node := &NodeInfo{}
		if err := json.Unmarshal(data, node); err != nil {
			return nil, fmt.Errorf("redis discovery decode node %s: %w", id, err)
		}
		if len(statusSet) > 0 {
			if _, ok := statusSet[node.Status]; !ok {
				continue
			}
		}
		result = append(result, node)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (p *RedisProvider) Watch(ctx context.Context, serviceName string, onChange func([]*NodeInfo)) (func(), error) {
	if strings.TrimSpace(serviceName) == "" || onChange == nil {
		return nil, errors.New("redis discovery watch service and callback are required")
	}
	if p.closed.Load() {
		return nil, errors.New("redis discovery provider is closed")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := p.client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis discovery watch %s: %w", serviceName, err)
	}
	watchCtx, cancel := context.WithCancel(p.ctx)
	stopCallerCancel := context.AfterFunc(ctx, cancel)
	previous := ""
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer stopCallerCancel()
		p.runWatch(watchCtx, serviceName, onChange, &previous)
	}()
	return cancel, nil
}

func (p *RedisProvider) runWatch(ctx context.Context, serviceName string, onChange func([]*NodeInfo), previous *string) {
	p.publishSnapshot(ctx, serviceName, onChange, previous)
	lastID := "$"
	reconcileEvery := p.ttl / 2
	if reconcileEvery < 100*time.Millisecond {
		reconcileEvery = 100 * time.Millisecond
	}
	ticker := time.NewTicker(reconcileEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.publishSnapshot(ctx, serviceName, onChange, previous)
		default:
		}
		streams, err := p.client.XRead(ctx, &redis.XReadArgs{Streams: []string{p.eventsKey(), lastID}, Count: 20, Block: 200 * time.Millisecond}).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		for _, stream := range streams {
			for _, message := range stream.Messages {
				lastID = message.ID
				if fmt.Sprint(message.Values["service"]) == serviceName {
					p.publishSnapshot(ctx, serviceName, onChange, previous)
				}
			}
		}
	}
}

func (p *RedisProvider) publishSnapshot(ctx context.Context, serviceName string, onChange func([]*NodeInfo), previous *string) {
	nodes, err := p.List(ctx, serviceName, NodeStatusRunning)
	if err != nil {
		return
	}
	parts := make([]string, 0, len(nodes))
	for _, node := range nodes {
		parts = append(parts, node.ID+"@"+node.Address)
	}
	fingerprint := strings.Join(parts, ",")
	if fingerprint == *previous {
		return
	}
	*previous = fingerprint
	onChange(nodes)
}

func (p *RedisProvider) Close() error {
	var err error
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		p.cancel()
		p.wg.Wait()
		err = p.client.Close()
	})
	return err
}

func (p *RedisProvider) nodeKey(serviceName, nodeID string) string {
	return p.prefix + ":nodes:" + serviceName + ":" + nodeID
}

func (p *RedisProvider) indexKey(nodeID string) string {
	return p.prefix + ":node-index:" + nodeID
}

func (p *RedisProvider) serviceKey(serviceName string) string {
	return p.prefix + ":services:" + serviceName
}

func (p *RedisProvider) slotKey(node *NodeInfo) string {
	return fmt.Sprintf("%s:slots:%s:%d:%d", p.prefix, node.ServiceName, node.DataCenterID, node.MachineID)
}

func (p *RedisProvider) eventsKey() string {
	return p.prefix + ":events"
}

func cloneNode(node *NodeInfo) *NodeInfo {
	copyNode := *node
	if node.Metadata != nil {
		copyNode.Metadata = make(map[string]string, len(node.Metadata))
		for key, value := range node.Metadata {
			copyNode.Metadata[key] = value
		}
	}
	return &copyNode
}
