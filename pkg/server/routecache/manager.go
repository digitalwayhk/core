package routecache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/zeromicro/go-zero/core/syncx"
)

var ErrManagerClosed = errors.New("route cache manager closed")

type routePolicy struct {
	ttl time.Duration
}

type Manager struct {
	service string
	config  config.RouteCacheConfig
	l1      *l1Cache
	flight  syncx.SingleFlight
	closed  atomic.Bool

	routesMu sync.RWMutex
	routes   map[string]routePolicy
}

func NewManager(service string, cfg config.RouteCacheConfig) (*Manager, error) {
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	l1, err := newL1Cache(cfg.TTL, cfg.L1.Limit)
	if err != nil {
		return nil, err
	}
	return &Manager{
		service: service,
		config:  cfg,
		l1:      l1,
		flight:  syncx.NewSingleFlight(),
		routes:  make(map[string]routePolicy),
	}, nil
}

func (m *Manager) EnableRoute(route string, ttl time.Duration) error {
	if m == nil || m.closed.Load() {
		return ErrManagerClosed
	}
	if ttl <= 0 {
		ttl = m.config.TTL
	}
	m.routesMu.Lock()
	m.routes[route] = routePolicy{ttl: ttl}
	m.routesMu.Unlock()
	return nil
}

func (m *Manager) Get(route string, source interface{}) (interface{}, bool, error) {
	key, enabled, err := m.cacheKey(route, source)
	if err != nil || !enabled {
		return nil, false, err
	}
	value, ok := m.l1.Get(key)
	return value, ok, nil
}

func (m *Manager) Set(route string, source, value interface{}, ttl time.Duration) error {
	key, enabled, err := m.cacheKey(route, source)
	if err != nil || !enabled {
		return err
	}
	if ttl <= 0 {
		ttl = m.routeTTL(route)
	}
	m.l1.Set(key, value, ttl)
	return nil
}

func (m *Manager) Delete(route string, source interface{}) error {
	key, enabled, err := m.cacheKey(route, source)
	if err != nil || !enabled {
		return err
	}
	m.l1.Delete(key)
	return nil
}

func (m *Manager) DeleteRoute(route string) error {
	if m == nil || m.closed.Load() {
		return ErrManagerClosed
	}
	prefix := m.service + ":" + route + ":"
	m.l1.keys.Range(func(key, _ interface{}) bool {
		text := key.(string)
		if len(text) >= len(prefix) && text[:len(prefix)] == prefix {
			m.l1.Delete(text)
		}
		return true
	})
	return nil
}

func (m *Manager) Take(route string, source interface{}, ttl time.Duration, loader func() (interface{}, error)) (interface{}, error) {
	if value, ok, err := m.Get(route, source); err != nil || ok {
		return value, err
	}
	key, enabled, err := m.cacheKey(route, source)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return loader()
	}
	return m.flight.Do(key, func() (interface{}, error) {
		if value, ok := m.l1.Get(key); ok {
			return value, nil
		}
		value, loadErr := loader()
		if loadErr != nil {
			return nil, loadErr
		}
		if ttl <= 0 {
			ttl = m.routeTTL(route)
		}
		m.l1.Set(key, value, ttl)
		return value, nil
	})
}

func (m *Manager) Close() {
	if m == nil || !m.closed.CompareAndSwap(false, true) {
		return
	}
	m.l1.Clear()
}

func (m *Manager) cacheKey(route string, source interface{}) (string, bool, error) {
	if m == nil || m.closed.Load() {
		return "", false, ErrManagerClosed
	}
	m.routesMu.RLock()
	_, enabled := m.routes[route]
	m.routesMu.RUnlock()
	if !enabled {
		return "", false, nil
	}
	key, err := BuildKey(source)
	if err != nil {
		return "", false, err
	}
	return m.service + ":" + route + ":" + key, true, nil
}

func (m *Manager) routeTTL(route string) time.Duration {
	m.routesMu.RLock()
	policy, ok := m.routes[route]
	m.routesMu.RUnlock()
	if !ok || policy.ttl <= 0 {
		return m.config.TTL
	}
	return policy.ttl
}

func BuildKey(source interface{}) (string, error) {
	if keyer, ok := source.(interface{ GetCacheKey() string }); ok {
		return "key:" + keyer.GetCacheKey(), nil
	}
	if keyer, ok := source.(interface{ GetHashKey() uint64 }); ok {
		return "hash:" + strconv.FormatUint(keyer.GetHashKey(), 10), nil
	}
	typeName := "<nil>"
	if source != nil {
		typeName = reflect.TypeOf(source).String()
	}
	payload, err := json.Marshal(struct {
		Type  string      `json:"type"`
		Value interface{} `json:"value"`
	}{Type: typeName, Value: source})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "json:" + hex.EncodeToString(digest[:]), nil
}
