package melody

import (
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/types"
)

type SessionSubscriptions struct {
	subscriptions map[string]map[int]types.IRouter // channel -> hash -> router
	metadata      map[string]interface{}           // 客户端元数据
	createdAt     time.Time
	lastActivity  time.Time
	mu            sync.RWMutex
	manage        *MelodyManager
}

func NewSessionSubscriptions(manage *MelodyManager) *SessionSubscriptions {
	return &SessionSubscriptions{
		subscriptions: make(map[string]map[int]types.IRouter),
		manage:        manage,
		metadata:      make(map[string]interface{}),
		createdAt:     time.Now(),
		lastActivity:  time.Now(),
	}
}
func (s *SessionSubscriptions) Subscribe(channel string, hash int, router types.IRouter) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.subscriptions[channel]; !exists {
		s.subscriptions[channel] = make(map[int]types.IRouter)
	}
	s.subscriptions[channel][hash] = router
	s.lastActivity = time.Now() // 更新最后活动时间
}
func (s *SessionSubscriptions) Unsubscribe(channel string, hash int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.subscriptions[channel]; exists {
		delete(s.subscriptions[channel], hash)
		if len(s.subscriptions[channel]) == 0 {
			delete(s.subscriptions, channel)
		}
	}
	s.lastActivity = time.Now() // 更新最后活动时间
}
func (s *SessionSubscriptions) GetAllSubscriptions() map[string]map[int]types.IRouter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.subscriptions
}
func (s *SessionSubscriptions) GetSubscriptions(channel string) map[int]types.IRouter {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if subs, exists := s.subscriptions[channel]; exists {
		return subs
	}
	return nil
}
func (s *SessionSubscriptions) GetMetadata(key string) interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.metadata[key]
}
func (s *SessionSubscriptions) SetMetadata(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.metadata[key] = value
	s.lastActivity = time.Now() // 更新最后活动时间
}
