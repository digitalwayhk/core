package melody

import (
	"strconv"
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
func (s *SessionSubscriptions) getIRequest(channel string) types.IRequest {
	req := router.NewRequest(s.sr, s.client.session.Request)
	clearRequest(req, channel)
	return req
}
func clearRequest(req interface{}, channel string) {
	if cr, ok := req.(types.IRequestClear); ok {
		cr.ClearTraceId()
		cr.SetPath(channel)
	}
}
func (s *SessionSubscriptions) getApi(info *types.RouterInfo, channel string, data interface{}) (types.IRouter, types.IRequest, error) {
	req := s.getIRequest(channel)
	api, err := s.manage.parseRequest(info, data)
	if err != nil {
		return nil, nil, err
	}
	err = api.Validation(req)
	if err != nil {
		return nil, nil, err
	}
	return api, req, nil
}
func (s *SessionSubscriptions) HandleSubscribe(msg *Message) {
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
	api, req, err := s.getApi(info, channel, msg.Data)
	if err != nil {
		s.client.SendError(channel, "订阅错误: "+err.Error())
		return
	}
	hash := info.RegisterWebSocketClient(api, s.client, req)
	s.subscriptions[channel][hash] = api
	s.client.Send("sub", channel, s.subscriptions[channel])
	s.lastActivity = time.Now() // 更新最后活动时间
}

func (s *SessionSubscriptions) HandleUnsubscribe(msg *Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	channel := strings.TrimSpace(msg.Channel)
	info := s.sr.GetRouter(channel)
	if info == nil {
		s.client.SendError(channel, "当前服务中未找到对应的路由")
		return
	}
	hash, ok := msg.Data.(int)
	if !ok {
		api, _, err := s.getApi(info, channel, msg.Data)
		if err != nil {
			s.client.SendError(channel, "退订错误: "+err.Error())
			return
		}
		hash = info.UnRegisterWebSocketClient(api, s.client)
	} else {
		info.UnRegisterWebSocketHash(hash, s.client)
	}
	if _, exists := s.subscriptions[channel]; exists {
		delete(s.subscriptions[channel], hash)
		if len(s.subscriptions[channel]) == 0 {
			delete(s.subscriptions, channel)
		}
	}
	s.client.Send("unsub", channel, strconv.Itoa(hash))
	s.lastActivity = time.Now() // 更新最后活动时间
}

func (s *SessionSubscriptions) UnsubscribeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for channel, subs := range s.subscriptions {
		info := s.sr.GetRouter(channel)
		if info == nil {
			continue
		}
		for hash := range subs {
			info.UnRegisterWebSocketHash(hash, s.client)
			delete(s.subscriptions[channel], hash)
		}
		if len(s.subscriptions[channel]) == 0 {
			delete(s.subscriptions, channel)
		}
	}
	s.lastActivity = time.Now() // 更新最后活动时间
}
func (s *SessionSubscriptions) setServiceRouter(sr *router.ServiceRouter) {
	s.sr = sr
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
