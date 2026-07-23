package ratelimit

import (
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/types"
	"golang.org/x/time/rate"
)

const defaultIdleTTL = 10 * time.Minute

type clientLimiter struct {
	limiter  *rate.Limiter
	policy   types.ExternalRateLimitPolicy
	lastSeen time.Time
}

// Manager 拥有一个 ServiceContext 内所有外部 Public API 的本地令牌桶。
type Manager struct {
	service     string
	idleTTL     time.Duration
	mu          sync.Mutex
	clients     map[string]*clientLimiter
	lastCleanup time.Time
	closed      bool
}

// NewManager 创建服务级限流器；idleTTL 非正数时使用默认值。
func NewManager(service string, idleTTL time.Duration) *Manager {
	if idleTTL <= 0 {
		idleTTL = defaultIdleTTL
	}
	return &Manager{
		service:     normalizeKeyPart(service, "unknown-service"),
		idleTTL:     idleTTL,
		clients:     make(map[string]*clientLimiter),
		lastCleanup: time.Now(),
	}
}

// Allow 按服务、路由和可信客户端 IP 消费令牌。
func (m *Manager) Allow(route, clientIP string, policy types.ExternalRateLimitPolicy) bool {
	if m == nil || !policy.Valid() {
		return false
	}
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return false
	}
	m.cleanupLocked(now)

	key := m.service + "\x00" + normalizeKeyPart(route, "unknown-route") + "\x00" + normalizeKeyPart(clientIP, "unknown")
	entry := m.clients[key]
	if entry == nil || entry.policy != policy {
		entry = &clientLimiter{
			limiter: rate.NewLimiter(rate.Limit(policy.Rate), policy.Burst),
			policy:  policy,
		}
		m.clients[key] = entry
	}
	entry.lastSeen = now
	return entry.limiter.Allow()
}

func (m *Manager) cleanupLocked(now time.Time) {
	if now.Sub(m.lastCleanup) < m.idleTTL {
		return
	}
	oldest := now.Add(-m.idleTTL)
	for key, entry := range m.clients {
		if entry.lastSeen.Before(oldest) {
			delete(m.clients, key)
		}
	}
	m.lastCleanup = now
}

// Close 清空服务限流状态，之后的 Allow 全部 fail closed。
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.closed = true
	m.clients = nil
	m.mu.Unlock()
}

func normalizeKeyPart(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
