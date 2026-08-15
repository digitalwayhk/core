package runtime

import (
	"context"
	"sync"

	"github.com/digitalwayhk/core/pkg/server/observability"
)

// MemorySubscriptionIndex 进程内订阅注册表，并同步导出 Prometheus 指标供跨进程查询。
type MemorySubscriptionIndex struct {
	mu    sync.RWMutex
	edges map[string]SubscriptionEdge // key 与 gauge labels 对齐：target|family|type|reliable
	refs  map[string]int
}

// GlobalSubscriptionIndex 全进程默认订阅索引（多服务同进程共享）。
var GlobalSubscriptionIndex = NewMemorySubscriptionIndex()

// NewMemorySubscriptionIndex 创建空索引。
func NewMemorySubscriptionIndex() *MemorySubscriptionIndex {
	return &MemorySubscriptionIndex{
		edges: make(map[string]SubscriptionEdge),
		refs:  make(map[string]int),
	}
}

func subscriptionRefKey(target, family, eventType string, reliable bool) string {
	rel := "false"
	if reliable {
		rel = "true"
	}
	return target + "|" + family + "|" + eventType + "|" + rel
}

// Register 登记一条已验证的业务订阅，返回幂等注销函数。
func (m *MemorySubscriptionIndex) Register(targetService, subject, eventType string, reliable bool) func() {
	noop := func() {}
	if m == nil {
		return noop
	}
	target := observability.NormalizeServiceLabel(targetService)
	family := NormalizeSubjectFamily(subject)
	if target == "unknown" || family == "" {
		return noop
	}
	et := observability.NormalizeServiceLabel(eventType)
	if et == "unknown" {
		et = "unspecified"
	}
	key := subscriptionRefKey(target, family, et, reliable)
	m.mu.Lock()
	m.refs[key]++
	m.edges[key] = SubscriptionEdge{
		SourceSubjectFamily: family,
		EventType:           et,
		TargetService:       target,
		Reliable:            reliable,
	}
	m.mu.Unlock()
	observability.SetSubscriptionActive(target, family, et, reliable, true)

	var once sync.Once
	return func() {
		once.Do(func() {
			m.Unregister(target, family, et, reliable)
		})
	}
}

// Unregister 取消订阅；引用归零后删除本地边与 Prom 样本。
func (m *MemorySubscriptionIndex) Unregister(targetService, subjectFamily, eventType string, reliable bool) {
	if m == nil {
		return
	}
	target := observability.NormalizeServiceLabel(targetService)
	family := NormalizeSubjectFamily(subjectFamily)
	et := observability.NormalizeServiceLabel(eventType)
	if et == "unknown" {
		et = "unspecified"
	}
	key := subscriptionRefKey(target, family, et, reliable)
	m.mu.Lock()
	if m.refs[key] <= 0 {
		m.mu.Unlock()
		return
	}
	m.refs[key]--
	if m.refs[key] > 0 {
		m.mu.Unlock()
		return
	}
	delete(m.refs, key)
	delete(m.edges, key)
	m.mu.Unlock()
	observability.SetSubscriptionActive(target, family, et, reliable, false)
}

// List 返回当前本进程订阅边。
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
	for _, e := range m.edges {
		observability.SetSubscriptionActive(e.TargetService, e.SourceSubjectFamily, e.EventType, e.Reliable, false)
	}
	m.edges = make(map[string]SubscriptionEdge)
	m.refs = make(map[string]int)
	m.mu.Unlock()
}

// NormalizeSubjectFamily 将 Subject 归一为有界 family。
func NormalizeSubjectFamily(subject string) string {
	s := observability.NormalizeServiceLabel(subject)
	if s == "unknown" {
		return ""
	}
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}
