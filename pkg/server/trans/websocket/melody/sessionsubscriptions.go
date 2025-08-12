package melody

import (
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type SessionSubscriptions struct {
	subscriptions map[string]map[int]types.IRouter // channel -> hash -> router
	metadata      map[string]interface{}           // 客户端元数据
	createdAt     time.Time
	lastActivity  time.Time
	mu            sync.RWMutex
	manage        *MelodyManager
	client        *MelodyClient
	sr            *router.ServiceRouter
}

func NewSessionSubscriptions(manage *MelodyManager, client *MelodyClient, sr *router.ServiceRouter) *SessionSubscriptions {
	return &SessionSubscriptions{
		subscriptions: make(map[string]map[int]types.IRouter),
		client:        client,
		manage:        manage,
		metadata:      make(map[string]interface{}),
		createdAt:     time.Now(),
		lastActivity:  time.Now(),
	}
}
func (s *SessionSubscriptions) GetClient() types.IWebSocket {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.client
}
func (s *SessionSubscriptions) Subscribe(channel string, hash int, api types.IRouter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.subscriptions[channel]; !exists {
		s.subscriptions[channel] = make(map[int]types.IRouter)
	}
	if _, exists := s.subscriptions[channel][hash]; !exists {
		s.subscriptions[channel][hash] = api
		info := api.RouterInfo()
		res := router.NewRequest(s.sr, s.client.session.Request)
		info.RegisterWebSocketClient(api, s.client, res)
	}
	s.lastActivity = time.Now() // 更新最后活动时间
}
func (s *SessionSubscriptions) handleSubscribe(msg *Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	channel := strings.TrimSpace(msg.Channel)
	info := s.sr.GetRouter(channel)
	if info == nil {
		s.client.SendError(channel, "当前服务中未找到对应的路由")
		return
	}
	if _, exists := s.subscriptions[channel]; !exists {
		s.subscriptions[channel] = make(map[int]types.IRouter)
	}
	res := router.NewRequest(s.sr, s.client.session.Request)
	api, err := info.ParseNew(msg.Data)
	if err != nil {
		s.client.SendError(channel, "订阅错误: "+err.Error())
		return
	}
	hash := info.RegisterWebSocketClient(api, s.client, res)
	s.subscriptions[channel][hash] = api
}
func (s *SessionSubscriptions) handleUnsubscribe(msg *Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	channel := strings.TrimSpace(msg.Channel)
	hash := msg.Data.(int)
	if _, exists := s.subscriptions[channel]; exists {
		delete(s.subscriptions[channel], hash)
		if len(s.subscriptions[channel]) == 0 {
			delete(s.subscriptions, channel)
		}
	}
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
