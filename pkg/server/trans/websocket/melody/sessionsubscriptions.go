package melody

import (
	"context"
	"encoding/json"
	"errors"
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
	identity      *safe.AccessTokenIdentity
	hookSlots     chan struct{}
}

func NewSessionSubscriptions(manage *MelodyManager, client *MelodyClient, sr *router.ServiceRouter) *SessionSubscriptions {
	return &SessionSubscriptions{
		subscriptions: make(map[string]map[uint64]types.IRouter),
		client:        client,
		manage:        manage,
		metadata:      make(map[string]interface{}),
		createdAt:     time.Now(),
		lastActivity:  time.Now(),
		hookSlots:     make(chan struct{}, 1),
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
	request := types.IRequest(req)
	var verified *safe.AccessTokenIdentity
	if routeRequiresWebSocketAuth(info) {
		var err error
		verified, err = s.authorizeAuthenticatedSubscription(info, req)
		if err != nil {
			return nil, nil, err
		}
		request = &authenticatedWebSocketRequest{IRequest: req, identity: toWebSocketAuthIdentity(req.ServiceName(), verified)}
	}
	api, err := s.manage.parseSubscriptionRequest(info, data)
	if err != nil {
		return nil, nil, err
	}
	if routeRequiresWebSocketAuth(info) {
		identity, ok := api.(types.IWebSocketUserIdentity)
		if !ok {
			info.ReleaseSubscription(api)
			return nil, nil, errors.New("认证 WebSocket 路由必须实现 IWebSocketUserIdentity")
		}
		identity.SetUserID(verified.UID, verified.Username)
	}
	if err := api.Validation(request); err != nil {
		info.ReleaseSubscription(api)
		return nil, nil, err
	}
	return api, request, nil
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
		err = s.logonLocked(req)
		if err != nil {
			s.manage.sendError(s.client.session, channel, "登录请求错误: "+webSocketPublicMessage(err))
			return true
		}
		s.manage.sendToSession(s.client.session, "success", channel, req.Response())
		return true
	}
	if strings.EqualFold(channel, "status") {
		if s.req == nil || s.identity == nil || s.req.Validate() != nil {
			s.manage.sendError(s.client.session, channel, "Invalid token, API-key, IP, or permissions for action")
			return true
		}
		s.manage.sendToSession(s.client.session, "success", channel, s.req.Response())
		return true
	}
	if strings.EqualFold(channel, "logout") {
		data := s.logoutLocked()
		s.manage.sendToSession(s.client.session, "success", channel, data)
		return true
	}
	return false
}
func (s *SessionSubscriptions) HandleSubscribe(msg *Message) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isLogonChannel(msg) {
		return false
	}
	channel := strings.TrimSpace(msg.Channel)
	info := s.sr.GetRouter(channel)
	if info == nil {
		s.client.SendError(channel, "当前服务中未找到对应的路由")
		return false
	}
	if _, exists := s.subscriptions[channel]; !exists {
		s.subscriptions[channel] = make(map[uint64]types.IRouter)
	}
	api, req, err := s.getApi(info, channel, msg.Data)
	if err != nil {
		s.client.SendError(channel, "订阅错误: "+webSocketPublicMessage(err))
		return false
	}

	hash := info.RegisterWebSocketClient(api, s.client, req)
	if hash == 0 {
		s.client.SendError(channel, "订阅注册失败")
		return false
	}
	s.subscriptions[channel][hash] = api
	s.client.Send("sub", channel, s.subscriptions[channel])
	s.lastActivity = time.Now() // 更新最后活动时间
	return true
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
		api, _, err := s.getApi(info, channel, msg.Data)
		if err != nil {
			s.client.SendError(channel, "退订错误: "+err.Error())
			return
		}
		hash = info.UnRegisterWebSocketClient(api, s.client)
		info.ReleaseSubscription(api)
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
	s.unsubscribeUserLocked()
}

func (s *SessionSubscriptions) unsubscribeUserLocked() {
	for channel, subs := range s.subscriptions {
		info := s.sr.GetRouter(channel)
		if info == nil {
			continue
		}
		if routeRequiresWebSocketAuth(info) {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logonLocked(req)
}

func (s *SessionSubscriptions) logonLocked(req *SessionRequest) error {
	if err := req.Validate(); err != nil {
		return webSocketAuthenticationError(err)
	}
	identity, err := safe.ValidateAccessToken(
		req.Token,
		s.manage.serviceContext.Config.Auth.AccessSecret,
		types.AuthTypeUser,
		time.Now(),
	)
	if err != nil {
		return webSocketAuthenticationError(err)
	}
	if identity.UID == "" {
		return webSocketAuthenticationError(errors.New("invalid session request"))
	}
	manager, _, active := s.manage.serviceContext.GetAuthRequestRuntime()
	if !active {
		return webSocketAuthenticationError(errors.New("service authentication is closing"))
	}
	if identity.Identity.Provider == types.AuthProviderCasdoor {
		if manager == nil {
			return webSocketAuthenticationError(errors.New("revocation authority unavailable"))
		}
		if err := manager.Authorize(context.Background(), identity.Identity); err != nil {
			return webSocketAuthenticationError(err)
		}
	}
	if s.identity != nil && !sameWebSocketIdentity(s.identity, identity) {
		s.unsubscribeUserLocked()
	}
	req.userID = identity.UID
	req.userName = identity.Username
	s.req = req
	s.identity = identity

	return nil
}

func sameWebSocketIdentity(left, right *safe.AccessTokenIdentity) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Identity.UID == right.Identity.UID &&
		left.Identity.AuthType == right.Identity.AuthType &&
		left.Identity.Provider == right.Identity.Provider &&
		left.Identity.ProviderSubject == right.Identity.ProviderSubject &&
		left.Identity.Generation == right.Identity.Generation
}

func routeRequiresWebSocketAuth(info *types.RouterInfo) bool {
	return info != nil && (info.GetAuth() || info.GetPathType() == types.PrivateType)
}
func (s *SessionSubscriptions) Status() {

}
func (s *SessionSubscriptions) Logout() *SessionResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logoutLocked()
}

func (s *SessionSubscriptions) logoutLocked() *SessionResponse {
	s.req = nil
	s.identity = nil
	s.unsubscribeUserLocked()
	return &SessionResponse{
		ApiKey:           "",
		AuthorizedSince:  0,
		ConnectedSince:   0,
		ReturnRateLimits: false,
	}
}

type authenticatedWebSocketRequest struct {
	types.IRequest
	identity types.WebSocketAuthIdentity
}

func (r *authenticatedWebSocketRequest) GetUser() (string, string) {
	return r.identity.UID, r.identity.Username
}

func (r *authenticatedWebSocketRequest) GetWebSocketAuthIdentity() (types.WebSocketAuthIdentity, bool) {
	return r.identity, strings.TrimSpace(r.identity.UID) != ""
}

func (r *authenticatedWebSocketRequest) GetSecretClaim(key string) (string, bool) {
	reader, ok := r.IRequest.(types.IRequestSecretClaims)
	if !ok {
		return "", false
	}
	return reader.GetSecretClaim(key)
}

func (s *SessionSubscriptions) authorizeAuthenticatedSubscription(info *types.RouterInfo, req types.IRequest) (*safe.AccessTokenIdentity, error) {
	if s == nil || s.req == nil || s.manage == nil || s.manage.serviceContext == nil || s.manage.serviceContext.Config == nil {
		return nil, webSocketAuthenticationError(errors.New("authentication context unavailable"))
	}
	verified, err := safe.ValidateAccessToken(
		s.req.Token,
		s.manage.serviceContext.Config.Auth.AccessSecret,
		types.AuthTypeUser,
		time.Now().UTC(),
	)
	if err != nil {
		return nil, webSocketAuthenticationError(err)
	}
	manager, hook, active := s.manage.serviceContext.GetAuthRequestRuntime()
	if !active {
		return nil, webSocketAuthenticationError(errors.New("service authentication is closing"))
	}
	if verified.Identity.Provider == types.AuthProviderCasdoor {
		if manager == nil {
			return nil, webSocketAuthenticationError(errors.New("revocation authority unavailable"))
		}
		if err := manager.Authorize(requestContext(req), verified.Identity); err != nil {
			return nil, webSocketAuthenticationError(err)
		}
	}
	if setter, ok := req.(types.IRequestSecretClaimsSetter); ok {
		setter.SetSecretClaims(verified.SecretClaims)
	}
	if hook != nil {
		if s.hookSlots == nil {
			s.hookSlots = make(chan struct{}, 1)
		}
		args := types.AuthRequestArgs{
			Identity: verified.Identity, ServiceName: req.ServiceName(), Path: info.GetPath(),
			Method: info.GetMethod(), PathType: info.GetPathType(), ClientIP: req.GetClientIP(),
			TraceID: req.GetTraceId(), Claims: types.CloneAuthClaims(verified.Claims),
			SecretClaims: types.CloneSecretClaims(verified.SecretClaims),
		}
		if err := invokeWebSocketAuthRequestHook(requestContext(req), s.manage.serviceContext.Config.Timeout, hook, args, s.hookSlots); err != nil {
			return nil, err
		}
	}
	s.identity = verified
	s.req.userID = verified.UID
	s.req.userName = verified.Username
	return verified, nil
}

func requestContext(req types.IRequest) context.Context {
	if value, ok := req.(types.IRequestHttp); ok && value.GetHttpRequest() != nil {
		return value.GetHttpRequest().Context()
	}
	return context.Background()
}

func invokeWebSocketAuthRequestHook(
	ctx context.Context,
	timeoutMilliseconds int64,
	hook types.IAuthRequestHookProvider,
	args types.AuthRequestArgs,
	slots chan struct{},
) error {
	if slots == nil {
		return types.NewPublicError(types.ErrorKindInternal, 0, "", errors.New("websocket auth hook slot unavailable"))
	}
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return types.NewPublicError(types.ErrorKindInternal, 0, "", errors.New("websocket auth hook canceled"))
	default:
		return types.NewPublicError(types.ErrorKindInternal, 0, "", errors.New("websocket auth hook still running"))
	}
	timeout := 3 * time.Second
	if timeoutMilliseconds > 0 {
		timeout = time.Duration(timeoutMilliseconds) * time.Millisecond
	}
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		defer func() { <-slots }()
		defer func() {
			if recover() != nil {
				result <- types.NewPublicError(types.ErrorKindInternal, 0, "", errors.New("websocket auth request hook panic"))
			}
		}()
		result <- hook.OnAuthRequest(hookCtx, args)
	}()
	select {
	case err := <-result:
		return err
	case <-hookCtx.Done():
		return types.NewPublicError(types.ErrorKindInternal, 0, "", errors.New("websocket auth request hook timeout"))
	}
}

func toWebSocketAuthIdentity(serviceName string, verified *safe.AccessTokenIdentity) types.WebSocketAuthIdentity {
	if verified == nil {
		return types.WebSocketAuthIdentity{}
	}
	return types.WebSocketAuthIdentity{
		ServiceName: serviceName, AuthType: verified.AuthType, Provider: verified.Identity.Provider,
		ProviderSubject: verified.Identity.ProviderSubject, UID: verified.UID,
		Username: verified.Username, Generation: verified.Identity.Generation,
	}
}

func webSocketAuthenticationError(cause error) error {
	return types.NewPublicError(types.ErrorKindUnauthenticated, types.PublicCodeUnauthenticated, "authentication failed", cause)
}

func webSocketPublicMessage(err error) string {
	return types.ResolvePublicError(err).Message
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
