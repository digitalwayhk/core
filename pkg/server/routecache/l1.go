package routecache

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	lru "github.com/hashicorp/golang-lru"
)

type l1Entry struct {
	value     json.RawMessage
	expiresAt time.Time
	cost      int64
}

// l1Cache 使用成熟 LRU 同时限制条目数和序列化数据量。
// maxBytes 统计 JSON 负载字节，不声称等同于 Go 进程 RSS。
type l1Cache struct {
	cache         *lru.Cache
	maxValueBytes int64
	maxBytes      int64
	usedBytes     atomic.Int64
	setMu         sync.Mutex
	budget        *processL1Budget
	closed        bool
}

func newL1Cache(cfg config.RouteCacheL1Config) (*l1Cache, error) {
	cache := &l1Cache{
		maxValueBytes: cfg.MaxValueBytes,
		maxBytes:      cfg.MaxBytes,
		budget:        &sharedL1Budget,
	}
	cache.budget.acquire(cfg.MaxBytes)
	inner, err := lru.NewWithEvict(cfg.MaxEntries, func(_ interface{}, value interface{}) {
		if entry, ok := value.(l1Entry); ok {
			cache.usedBytes.Add(-entry.cost)
			cache.budget.release(entry.cost)
		}
	})
	if err != nil {
		cache.budget.closeUser()
		return nil, err
	}
	cache.cache = inner
	return cache, nil
}

func (c *l1Cache) Get(key string) (interface{}, bool) {
	value, ok := c.cache.Get(key)
	if !ok {
		return nil, false
	}
	entry, ok := value.(l1Entry)
	if !ok || !entry.expiresAt.After(time.Now()) {
		c.Delete(key)
		return nil, false
	}
	return append(json.RawMessage(nil), entry.value...), true
}

// Set 返回数据是否进入 L1；超大值被安全跳过，不影响业务响应。
func (c *l1Cache) Set(key string, value json.RawMessage, ttl time.Duration) bool {
	cost := int64(len(value))
	if cost > c.maxValueBytes || cost > c.maxBytes {
		return false
	}
	entry := l1Entry{
		value:     append(json.RawMessage(nil), value...),
		expiresAt: time.Now().Add(ttl),
		cost:      cost,
	}
	c.setMu.Lock()
	defer c.setMu.Unlock()
	if c.closed {
		return false
	}
	c.cache.Remove(key)
	for !c.budget.reserve(cost) {
		if _, _, ok := c.cache.RemoveOldest(); !ok {
			return false
		}
	}
	c.usedBytes.Add(cost)
	c.cache.Add(key, entry)
	for c.usedBytes.Load() > c.maxBytes {
		if _, _, ok := c.cache.RemoveOldest(); !ok {
			break
		}
	}
	return c.cache.Contains(key)
}

func (c *l1Cache) Delete(key string) { c.cache.Remove(key) }

func (c *l1Cache) Clear() { c.cache.Purge() }

func (c *l1Cache) Close() {
	c.setMu.Lock()
	defer c.setMu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	c.cache.Purge()
	c.budget.closeUser()
}
