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
	subscriptions map[string]map[int]types.IRouter // channel -> hash -> router
	metadata      map[string]interface{}           // 客户端元数据
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

type IUserID interface {
	GetUserID() uint
	SetUserID(userID uint)
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
		id, err := strconv.Atoi(s.req.userID)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid user ID in session request: %w", err)
		}
		if id <= 0 {
			return nil, nil, fmt.Errorf("invalid user ID in session request: %d", id)
		}
		if iuid, ok := api.(IUserID); ok {
			iuid.SetUserID(uint(id))
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
	secret := s.manage.serviceContext.Config.Auth.AccessSecret
	var sid string
	var err error
	if sid, err = safe.ValidateJWTToken(req.Token, secret); err != nil {
		return err
	}
	if sid == "" {
		return errors.New("invalid session request")
	}
	req.userID = sid
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

type SessionRequest struct {
	ApiKey    string `json:"apiKey"`
	Signature string `json:"signature"`
	Timestamp int64  `json:"timestamp"`
	Token     string `json:"token"`
	userID    string `json:"userId"`
}

func (own *SessionRequest) Response() *SessionResponse {
	key := own.ApiKey
	if key == "" {
		key = own.Token
	}
	return &SessionResponse{
		ApiKey:           key,
		AuthorizedSince:  time.Now().Unix(),
		ConnectedSince:   time.Now().Unix(),
		ReturnRateLimits: false,
	}
}
func (own *SessionRequest) Validate() error {
	if own == nil {
		return errors.New("invalid session request")
	}
	if own.Token == "" && (own.ApiKey == "" || own.Signature == "" || own.Timestamp == 0) {
		return errors.New("invalid session request")
	}
	return nil
}

type SessionResponse struct {
	ApiKey           string `json:"apiKey"`
	AuthorizedSince  int64  `json:"authorizedSince"`
	ConnectedSince   int64  `json:"connectedSince"`
	ReturnRateLimits bool   `json:"returnRateLimits"`
	ServerTime       int64  `json:"serverTime"`
}
