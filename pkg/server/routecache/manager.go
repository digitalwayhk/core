package routecache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/syncx"
)

var ErrManagerClosed = errors.New("route cache manager closed")

type ManagerState uint32

const (
	StateEnabled ManagerState = iota
	StateBypass
	StateDegraded
	StateClosed
)

type routePolicy struct {
	ttl        time.Duration
	generation uint64
}

type managerOptions struct {
	redisClient RedisClient
	events      InvalidationBridge
}

type Option func(*managerOptions)

func WithRedisClient(client RedisClient) Option {
	return func(options *managerOptions) { options.redisClient = client }
}

func WithInvalidationBridge(bridge InvalidationBridge) Option {
	return func(options *managerOptions) { options.events = bridge }
}

type Manager struct {
	service   string
	config    config.RouteCacheConfig
	l1        *l1Cache
	l2        *BadgerL2
	redis     *RedisL3
	events    InvalidationBridge
	flight    syncx.SingleFlight
	closed    atomic.Bool
	state     atomic.Uint32
	ttlJitter func(time.Duration) time.Duration

	invalidationReady atomic.Bool
	recoveryMu        sync.Mutex
	subscriptionMu    sync.Mutex
	localCancel       func()
	externalCancel    func()

	routesMu sync.RWMutex
	routes   map[string]routePolicy
}

func NewManager(service string, cfg config.RouteCacheConfig, options ...Option) (*Manager, error) {
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	autoMaxBytes := cfg.L1.MaxBytes == 0
	autoMaxEntries := cfg.L1.MaxEntries == 0
	cfg.L1 = resolveL1Config(cfg.L1)
	logx.Infow("route_cache_l1_resolved",
		logx.Field("service", service),
		logx.Field("max_entries", cfg.L1.MaxEntries),
		logx.Field("max_value_bytes", cfg.L1.MaxValueBytes),
		logx.Field("max_bytes", cfg.L1.MaxBytes),
		logx.Field("max_entries_source", resolutionSource(autoMaxEntries)),
		logx.Field("max_bytes_source", resolutionSource(autoMaxBytes)),
	)
	resolved := managerOptions{}
	for _, apply := range options {
		if apply != nil {
			apply(&resolved)
		}
	}
	manager := &Manager{
		service:   service,
		config:    cfg,
		events:    resolved.events,
		flight:    syncx.NewSingleFlight(),
		routes:    make(map[string]routePolicy),
		ttlJitter: jitterTTL,
	}
	manager.state.Store(uint32(StateEnabled))

	if cfg.Mode == "shared" {
		if resolved.redisClient != nil {
			manager.redis = NewRedisL3(resolved.redisClient, cfg.Redis)
		} else {
			redisL3, err := newConfiguredRedisL3(cfg.Redis)
			if err != nil {
				return manager.sharedUnavailable(err)
			}
			manager.redis = redisL3
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if !manager.redis.Ping(ctx) {
			return manager.sharedUnavailable(errors.New("route cache Redis ping failed"))
		}
		if err := manager.subscribeInvalidation(ctx); err != nil {
			return manager.sharedUnavailable(err)
		}
	}

	if err := manager.initLocalTiers(); err != nil {
		manager.cancelInvalidationSubscriptions()
		return nil, err
	}
	return manager, nil
}

func resolutionSource(auto bool) string {
	if auto {
		return "auto"
	}
	return "config"
}

func (m *Manager) sharedUnavailable(cause error) (*Manager, error) {
	if m.config.Redis.OnUnavailable != "bypass" {
		m.cancelInvalidationSubscriptions()
		return nil, cause
	}
	m.redis = nil
	m.state.Store(uint32(StateBypass))
	return m, nil
}

func (m *Manager) initLocalTiers() error {
	l1, err := newL1Cache(m.config.L1)
	if err != nil {
		return err
	}
	m.l1 = l1
	if !m.config.L2.Enable {
		return nil
	}
	l2Config := m.config.L2
	serviceDigest := sha256.Sum256([]byte(m.service))
	l2Config.Path = filepath.Join(m.config.L2.Path, hex.EncodeToString(serviceDigest[:8]))
	m.l2, err = NewBadgerL2(l2Config)
	if err != nil {
		m.l1.Clear()
		m.l1.Close()
		m.l1 = nil
	}
	return err
}

func (m *Manager) State() ManagerState {
	if m == nil {
		return StateClosed
	}
	return ManagerState(m.state.Load())
}

func (m *Manager) EnableRoute(route string, ttl time.Duration) error {
	if m == nil || m.closed.Load() {
		return ErrManagerClosed
	}
	if m.State() == StateBypass {
		return nil
	}
	if ttl <= 0 {
		ttl = m.config.TTL
	}
	generation := uint64(1)
	if m.redis != nil {
		var err error
		generation, err = m.redis.Generation(context.Background(), m.service, route)
		if err != nil {
			m.degrade()
			return err
		}
	}
	m.storeRoutePolicy(route, ttl, generation)
	return nil
}

func (m *Manager) Get(route string, source interface{}) (interface{}, bool, error) {
	if m == nil || m.closed.Load() {
		return nil, false, ErrManagerClosed
	}
	if m.State() != StateEnabled {
		return nil, false, nil
	}
	key, enabled, err := m.cacheKey(route, source)
	if err != nil || !enabled {
		return nil, false, err
	}
	if value, ok := m.l1.Get(key); ok {
		return value, true, nil
	}
	if m.l2 != nil {
		data, ok, getErr := m.l2.Get(key)
		if getErr != nil {
			return nil, false, getErr
		}
		if ok {
			m.l1.Set(key, json.RawMessage(data), m.routeTTL(route))
			return data, true, nil
		}
	}
	if m.redis == nil {
		return nil, false, nil
	}
	data, ok, err := m.redis.Get(context.Background(), key)
	if err != nil {
		m.degrade()
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	ttl := m.routeTTL(route)
	if m.l2 != nil {
		if err := m.l2.Set(key, data, ttl); err != nil {
			return nil, false, err
		}
	}
	m.l1.Set(key, json.RawMessage(data), ttl)
	return data, true, nil
}

func (m *Manager) Set(route string, source, value interface{}, ttl time.Duration) error {
	if m == nil || m.closed.Load() {
		return ErrManagerClosed
	}
	if m.State() != StateEnabled {
		return nil
	}
	key, enabled, err := m.cacheKey(route, source)
	if err != nil || !enabled {
		return err
	}
	if ttl <= 0 {
		ttl = m.routeTTL(route)
	}
	if m.ttlJitter != nil {
		ttl = m.ttlJitter(ttl)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if int64(len(data)) > m.config.L1.MaxValueBytes {
		return nil
	}
	if m.redis != nil {
		if err := m.redis.Set(context.Background(), key, data, ttl); err != nil {
			m.degrade()
			return err
		}
		if err := m.publishInvalidation(route, key, m.routeGeneration(route)); err != nil {
			m.degrade()
			return err
		}
	}
	if m.l2 != nil {
		if err := m.l2.Set(key, data, ttl); err != nil {
			return err
		}
	}
	m.l1.Set(key, json.RawMessage(data), ttl)
	return nil
}

func jitterTTL(ttl time.Duration) time.Duration {
	span := ttl / 10
	if span <= 0 {
		return ttl
	}
	return ttl - span + time.Duration(rand.Int64N(int64(2*span+1)))
}

func (m *Manager) Delete(route string, source interface{}) error {
	if m == nil || m.closed.Load() {
		return ErrManagerClosed
	}
	if m.State() != StateEnabled {
		return nil
	}
	key, enabled, err := m.cacheKey(route, source)
	if err != nil || !enabled {
		return err
	}
	if m.redis != nil {
		if err := m.redis.Delete(context.Background(), key); err != nil {
			m.degrade()
			return err
		}
		if err := m.publishInvalidation(route, key, m.routeGeneration(route)); err != nil {
			m.degrade()
			return err
		}
	}
	return m.deleteLocal(key)
}

func (m *Manager) DeleteRoute(route string) error {
	if m == nil || m.closed.Load() {
		return ErrManagerClosed
	}
	if m.State() != StateEnabled {
		return nil
	}
	m.routesMu.RLock()
	policy, ok := m.routes[route]
	m.routesMu.RUnlock()
	if !ok {
		return nil
	}
	if m.redis != nil {
		generation, err := m.redis.IncrementGeneration(context.Background(), m.service, route)
		if err != nil {
			m.degrade()
			return err
		}
		policy.generation = generation
		policy = m.storeRoutePolicy(route, 0, policy.generation)
	} else {
		m.routesMu.Lock()
		policy, ok = m.routes[route]
		if ok {
			policy.generation++
			m.routes[route] = policy
		}
		m.routesMu.Unlock()
		if !ok {
			return nil
		}
	}
	if m.redis != nil {
		if err := m.publishInvalidation(route, "", policy.generation); err != nil {
			m.degrade()
			return err
		}
	}
	return m.clearRouteLocal(route)
}

func (m *Manager) storeRoutePolicy(route string, ttl time.Duration, generation uint64) routePolicy {
	m.routesMu.Lock()
	defer m.routesMu.Unlock()
	policy := m.routes[route]
	if generation > policy.generation {
		policy.generation = generation
	}
	if ttl > 0 {
		policy.ttl = ttl
	}
	m.routes[route] = policy
	return policy
}

func (m *Manager) updateExistingRouteGeneration(route string, generation uint64) bool {
	m.routesMu.Lock()
	defer m.routesMu.Unlock()
	policy, ok := m.routes[route]
	if !ok {
		return false
	}
	if generation > policy.generation {
		policy.generation = generation
		m.routes[route] = policy
	}
	return true
}

func (m *Manager) Take(route string, source interface{}, ttl time.Duration, loader func() (interface{}, error)) (interface{}, error) {
	if value, ok, err := m.Get(route, source); err != nil || ok {
		return value, err
	}
	key, enabled, err := m.cacheKey(route, source)
	if err != nil {
		return nil, err
	}
	if !enabled || m.State() != StateEnabled {
		return loader()
	}
	return m.flight.Do(key, func() (interface{}, error) {
		if value, ok, getErr := m.Get(route, source); getErr != nil || ok {
			return value, getErr
		}
		value, loadErr := loader()
		if loadErr != nil {
			return nil, loadErr
		}
		if err := m.Set(route, source, value, ttl); err != nil {
			return nil, err
		}
		if cached, ok, err := m.Get(route, source); err != nil || ok {
			return cached, err
		}
		return value, nil
	})
}

// TakeBestEffort 合并同一缓存键的并发加载，但不让缓存故障改变业务结果。
// 只有 loader 返回的错误会传递给调用方；缓存错误会记录后旁路。
func (m *Manager) TakeBestEffort(route string, source interface{}, ttl time.Duration, loader func() (interface{}, error)) (interface{}, error) {
	if loader == nil {
		return nil, errors.New("route cache loader is nil")
	}
	if m == nil || m.closed.Load() || m.State() != StateEnabled {
		return loader()
	}
	if value, ok, err := m.Get(route, source); err == nil && ok {
		return value, nil
	} else if err != nil {
		m.logBestEffortBypass("read", route, err)
	}
	key, enabled, err := m.cacheKey(route, source)
	if err != nil {
		m.logBestEffortBypass("key", route, err)
		return loader()
	}
	if !enabled || m.State() != StateEnabled {
		return loader()
	}
	return m.flight.Do(key, func() (interface{}, error) {
		if value, ok, getErr := m.Get(route, source); getErr == nil && ok {
			return value, nil
		} else if getErr != nil {
			m.logBestEffortBypass("read", route, getErr)
		}
		value, loadErr := loader()
		if loadErr != nil || value == nil {
			return value, loadErr
		}
		if setErr := m.Set(route, source, value, ttl); setErr != nil {
			m.logBestEffortBypass("write", route, setErr)
			return value, nil
		}
		if cached, ok, getErr := m.Get(route, source); getErr == nil && ok {
			return cached, nil
		} else if getErr != nil {
			m.logBestEffortBypass("read_after_write", route, getErr)
		}
		return value, nil
	})
}

func (m *Manager) logBestEffortBypass(operation, route string, err error) {
	logx.Errorw("route_cache_bypassed",
		logx.Field("service", m.service),
		logx.Field("route", route),
		logx.Field("operation", operation),
		logx.Field("error", err),
	)
}

func (m *Manager) MarkInvalidationUnavailable() {
	if m == nil || m.config.Mode != "shared" || m.closed.Load() {
		return
	}
	m.invalidationReady.Store(false)
	m.degrade()
}

func (m *Manager) Recover(ctx context.Context) (bool, error) {
	if m == nil || m.closed.Load() {
		return false, ErrManagerClosed
	}
	if m.config.Mode != "shared" || m.State() != StateDegraded {
		return m.State() == StateEnabled, nil
	}
	m.recoveryMu.Lock()
	defer m.recoveryMu.Unlock()
	if !m.redis.Ping(ctx) {
		return false, errors.New("route cache Redis ping failed")
	}
	if !m.invalidationReady.Load() {
		if err := m.resubscribeExternal(ctx); err != nil {
			return false, err
		}
	}
	if err := m.refreshSharedGenerations(ctx); err != nil {
		return false, err
	}
	m.clearLocal()
	m.state.Store(uint32(StateEnabled))
	return true, nil
}

func (m *Manager) refreshSharedGenerations(ctx context.Context) error {
	m.routesMu.RLock()
	routes := make([]string, 0, len(m.routes))
	for route := range m.routes {
		routes = append(routes, route)
	}
	m.routesMu.RUnlock()
	for _, route := range routes {
		generation, err := m.redis.Generation(ctx, m.service, route)
		if err != nil {
			return err
		}
		m.updateExistingRouteGeneration(route, generation)
	}
	return nil
}

func (m *Manager) Close() {
	if m == nil || !m.closed.CompareAndSwap(false, true) {
		return
	}
	m.state.Store(uint32(StateClosed))
	m.cancelInvalidationSubscriptions()
	m.clearLocal()
	if m.l1 != nil {
		m.l1.Close()
	}
	if m.l2 != nil {
		_ = m.l2.Close()
	}
}

func (m *Manager) cacheKey(route string, source interface{}) (string, bool, error) {
	if m == nil || m.closed.Load() {
		return "", false, ErrManagerClosed
	}
	m.routesMu.RLock()
	policy, enabled := m.routes[route]
	m.routesMu.RUnlock()
	if !enabled {
		return "", false, nil
	}
	key, err := BuildKey(source)
	if err != nil {
		return "", false, err
	}
	return fmt.Sprintf("%sg%d:%s", m.routePrefix(route), policy.generation, key), true, nil
}

func (m *Manager) routePrefix(route string) string {
	return m.service + ":" + route + ":"
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

func (m *Manager) routeGeneration(route string) uint64 {
	m.routesMu.RLock()
	policy := m.routes[route]
	m.routesMu.RUnlock()
	return policy.generation
}

func (m *Manager) deleteLocal(key string) error {
	if m.l2 != nil {
		if err := m.l2.Delete(key); err != nil {
			return err
		}
	}
	if m.l1 != nil {
		m.l1.Delete(key)
	}
	return nil
}

func (m *Manager) clearRouteLocal(route string) error {
	prefix := m.routePrefix(route)
	if m.l2 != nil {
		if err := m.l2.DeletePrefix(prefix); err != nil {
			return err
		}
	}
	if m.l1 != nil {
		// generation 已经使旧路由 key 不可达；旧条目由 TTL/LRU 有界淘汰。
	}
	return nil
}

func (m *Manager) clearLocal() {
	if m.l1 != nil {
		m.l1.Clear()
	}
	if m.l2 != nil {
		m.routesMu.RLock()
		routes := make([]string, 0, len(m.routes))
		for route := range m.routes {
			routes = append(routes, route)
		}
		m.routesMu.RUnlock()
		for _, route := range routes {
			_ = m.l2.DeletePrefix(m.routePrefix(route))
		}
	}
}

func (m *Manager) degrade() {
	if m.config.Mode != "shared" || m.closed.Load() {
		return
	}
	m.clearLocal()
	m.state.Store(uint32(StateDegraded))
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
