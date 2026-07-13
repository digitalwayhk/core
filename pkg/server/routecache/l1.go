package routecache

import (
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/collection"
)

type l1Entry struct {
	value     interface{}
	expiresAt time.Time
}

type l1Cache struct {
	cache *collection.Cache
	keys  sync.Map
}

func newL1Cache(ttl time.Duration, limit int) (*l1Cache, error) {
	cache, err := collection.NewCache(ttl, collection.WithLimit(limit), collection.WithName("route-cache-l1"))
	if err != nil {
		return nil, err
	}
	return &l1Cache{cache: cache}, nil
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
	return entry.value, true
}

func (c *l1Cache) Set(key string, value interface{}, ttl time.Duration) {
	c.keys.Store(key, struct{}{})
	c.cache.SetWithExpire(key, l1Entry{value: value, expiresAt: time.Now().Add(ttl)}, ttl)
}

func (c *l1Cache) Delete(key string) {
	c.keys.Delete(key)
	c.cache.Del(key)
}

func (c *l1Cache) Clear() {
	c.keys.Range(func(key, _ interface{}) bool {
		c.Delete(key.(string))
		return true
	})
}
