package melody

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/safe"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type SessionSubscriptions struct {
	subscriptions map[string]map[uint64]types.IRouter // channel -> hash -> router
	metadata      map[string]interface{}              // 客户端元数据
	createdAt     time.Time
	lastActivity  time.Time
	mu            sync.RWMutex
	manage        *MelodyManager
	client        *MelodyClient
	sr            *router.ServiceRouter
	req           *SessionRequest
}

func NewSessionSubscriptions(manage *MelodyManager, client *MelodyClient, sr *router.ServiceRouter) *SessionSubscriptions {
	return &SessionSubscriptions{
		subscriptions: make(map[string]map[uint64]types.IRouter),
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

type IUserID interface {
	GetUserID() string
	SetUserID(userID, userName string)
}

func (s *SessionSubscriptions) getApi(info *types.RouterInfo, channel string, data interface{}) (types.IRouter, types.IRequest, error) {
	req := s.getIRequest(channel)
	api, err := s.manage.parseRequest(info, data)
	if err != nil {
		return nil, nil, err
	}
	if info.Auth {
		if s.req == nil {
			return nil, nil, errors.New("未登录，无法订阅需要认证的路由")
		}
		if err := s.req.Validate(); err != nil {
			return nil, nil, err
		}
		if s.req.userID == "" {
			return nil, nil, fmt.Errorf("invalid user ID in session request: %s", s.req.userID)
		}
		if iuid, ok := api.(IUserID); ok {
			iuid.SetUserID(s.req.userID, s.req.userName)
		}
	}
	err = api.Validation(req)
	if err != nil {
		return nil, nil, err
	}
	return api, req, nil
}
func (s *SessionSubscriptions) isLogonChannel(msg *Message) bool {
	channel := strings.TrimSpace(msg.Channel)
	if strings.EqualFold(channel, "logon") || strings.EqualFold(channel, "login") {
		req := &SessionRequest{}
		data, err := json.Marshal(msg.Data)
		if err != nil {
			s.manage.sendError(s.client.session, channel, "登录请求数据格式错误")
			return true
		}
		json.Unmarshal(data, req)
		err = s.Logon(req)
		if err != nil {
			s.manage.sendError(s.client.session, channel, "登录请求错误: "+err.Error())
			return true
		}
		s.manage.sendToSession(s.client.session, "success", channel, req.Response())
		return true
	}
	if strings.EqualFold(channel, "status") {
		if err := s.req.Validate(); err != nil {
			s.manage.sendError(s.client.session, channel, "Invalid token, API-key, IP, or permissions for action")
			return true
		}
		s.manage.sendToSession(s.client.session, "success", channel, s.req.Response())
		return true
	}
	if strings.EqualFold(channel, "logout") {
		data := s.Logout()
		s.manage.sendToSession(s.client.session, "success", channel, data)
		return true
	}
	return false
}
func (s *SessionSubscriptions) HandleSubscribe(msg *Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isLogonChannel(msg) {
		return
	}
	channel := strings.TrimSpace(msg.Channel)
	info := s.sr.GetRouter(channel)
	if info == nil {
		s.client.SendError(channel, "当前服务中未找到对应的路由")
		return
	}
	if info.Auth {
		if s.req == nil {
			s.client.SendError(channel, "未登陆不能订阅,请先使用logon通道登录!")
			return
		}
		if err := s.req.Validate(); err != nil {
			s.client.SendError(channel, "登陆超时或无效的会话，请重新登录: "+err.Error())
			return
		}
	}
	if _, exists := s.subscriptions[channel]; !exists {
		s.subscriptions[channel] = make(map[uint64]types.IRouter)
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
	hash, ok := msg.Data.(uint64)
	if !ok {
		hashStr := msg.Data.(string)
		var err error
		hash, err = strconv.ParseUint(hashStr, 10, 64)
		if err != nil {
			api, _, err := s.getApi(info, channel, msg.Data)
			if err != nil {
				s.client.SendError(channel, "退订错误: "+err.Error())
				return
			}
			hash = info.UnRegisterWebSocketClient(api, s.client)
		}
	} else {
		info.UnRegisterWebSocketHash(hash, s.client)
	}
	if _, exists := s.subscriptions[channel]; exists {
		delete(s.subscriptions[channel], hash)
		if len(s.subscriptions[channel]) == 0 {
			delete(s.subscriptions, channel)
		}
	}
	hashStr := strconv.FormatUint(hash, 10)
	s.client.Send("unsub", channel, hashStr)
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
func (s *SessionSubscriptions) UnsubscribeUser() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for channel, subs := range s.subscriptions {
		info := s.sr.GetRouter(channel)
		if info == nil {
			continue
		}
		if info.Auth {
			for hash := range subs {
				info.UnRegisterWebSocketHash(hash, s.client)
				delete(s.subscriptions[channel], hash)
			}
			if len(s.subscriptions[channel]) == 0 {
				delete(s.subscriptions, channel)
			}
		}
	}
	s.lastActivity = time.Now() // 更新最后活动时间
}
func (s *SessionSubscriptions) setServiceRouter(sr *router.ServiceRouter) {
	s.sr = sr
}
func (s *SessionSubscriptions) Logon(req *SessionRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	var sid string
	var sname string
	var err error
	if sid, sname, err = safe.ValidateJWTToken(req.Token, s.manage.serviceContext.Config.Auth); err != nil {
		return err
	}
	if sid == "" {
		return errors.New("invalid session request")
	}
	req.userID = sid
	req.userName = sname
	s.req = req

	return nil
}
func (s *SessionSubscriptions) Status() {

}
func (s *SessionSubscriptions) Logout() *SessionResponse {
	s.req = nil // 清除登录请求
	s.UnsubscribeUser()
	return &SessionResponse{
		ApiKey:           "",
		AuthorizedSince:  0,
		ConnectedSince:   0,
		ReturnRateLimits: false,
	}
}
func (s *SessionSubscriptions) GetAllSubscriptions() map[string]map[uint64]types.IRouter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.subscriptions
}
func (s *SessionSubscriptions) GetSubscriptions(channel string) map[uint64]types.IRouter {
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
