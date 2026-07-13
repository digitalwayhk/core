package routecache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
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
	service string
	config  config.RouteCacheConfig
	l1      *l1Cache
	l2      *BadgerL2
	redis   *RedisL3
	events  InvalidationBridge
	flight  syncx.SingleFlight
	closed  atomic.Bool
	state   atomic.Uint32

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
	resolved := managerOptions{}
	for _, apply := range options {
		if apply != nil {
			apply(&resolved)
		}
	}
	manager := &Manager{
		service: service,
		config:  cfg,
		events:  resolved.events,
		flight:  syncx.NewSingleFlight(),
		routes:  make(map[string]routePolicy),
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
	l1, err := newL1Cache(m.config.TTL, m.config.L1.Limit)
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
	m.routesMu.Lock()
	policy, exists := m.routes[route]
	if !exists {
		policy.generation = 1
	}
	policy.ttl = ttl
	m.routes[route] = policy
	m.routesMu.Unlock()
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
			m.l1.Set(key, data, m.routeTTL(route))
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
	m.l1.Set(key, data, ttl)
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
	data, err := json.Marshal(value)
	if err != nil {
		return err
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
	m.l1.Set(key, value, ttl)
	return nil
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
	m.routesMu.Lock()
	policy, ok := m.routes[route]
	if ok {
		policy.generation++
		m.routes[route] = policy
	}
	m.routesMu.Unlock()
	if !ok {
		return nil
	}
	if m.redis != nil {
		if err := m.publishInvalidation(route, "", policy.generation); err != nil {
			m.degrade()
			return err
		}
	}
	return m.clearRouteLocal(route)
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
		return value, nil
	})
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
	m.clearLocal()
	m.state.Store(uint32(StateEnabled))
	return true, nil
}

func (m *Manager) Close() {
	if m == nil || !m.closed.CompareAndSwap(false, true) {
		return
	}
	m.state.Store(uint32(StateClosed))
	m.cancelInvalidationSubscriptions()
	m.clearLocal()
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
		m.l1.keys.Range(func(key, _ interface{}) bool {
			text, ok := key.(string)
			if ok && strings.HasPrefix(text, prefix) {
				m.l1.Delete(text)
			}
			return true
		})
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
