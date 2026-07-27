package runtime

import (
	"context"
	"sync"

	"github.com/digitalwayhk/core/pkg/server/observability"
)

// MemorySubscriptionIndex 进程内订阅注册表，供异步边拼装。
type MemorySubscriptionIndex struct {
	mu    sync.RWMutex
	edges map[string]SubscriptionEdge // key=target|family|type
}

// GlobalSubscriptionIndex 全进程默认订阅索引（多服务同进程共享）。
var GlobalSubscriptionIndex = NewMemorySubscriptionIndex()

// NewMemorySubscriptionIndex 创建空索引。
func NewMemorySubscriptionIndex() *MemorySubscriptionIndex {
	return &MemorySubscriptionIndex{edges: make(map[string]SubscriptionEdge)}
}

// Register 登记一条已验证的业务订阅。
func (m *MemorySubscriptionIndex) Register(targetService, subject, eventType string, reliable bool) {
	if m == nil {
		return
	}
	target := observability.NormalizeServiceLabel(targetService)
	family := NormalizeSubjectFamily(subject)
	if target == "unknown" || family == "" {
		return
	}
	et := observability.NormalizeServiceLabel(eventType)
	if et == "unknown" {
		et = ""
	}
	key := target + "|" + family + "|" + et
	m.mu.Lock()
	m.edges[key] = SubscriptionEdge{
		SourceSubjectFamily: family,
		EventType:           et,
		TargetService:       target,
		Reliable:            reliable,
	}
	m.mu.Unlock()
}

// List 返回当前订阅边。
func (m *MemorySubscriptionIndex) List(context.Context) ([]SubscriptionEdge, error) {
	if m == nil {
		return nil, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]SubscriptionEdge, 0, len(m.edges))
	for _, e := range m.edges {
		out = append(out, e)
	}
	return out, nil
}

// ResetForTest 清空索引。
func (m *MemorySubscriptionIndex) ResetForTest() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.edges = make(map[string]SubscriptionEdge)
	m.mu.Unlock()
}

// NormalizeSubjectFamily 将 Subject 归一为有界 family（去掉实例级后缀）。
func NormalizeSubjectFamily(subject string) string {
	s := observability.NormalizeServiceLabel(subject)
	if s == "unknown" {
		return ""
	}
	// 仅允许字母数字与 .-_，长度截断防高基数。
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}
