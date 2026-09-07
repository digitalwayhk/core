package melody

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digitalwayhk/core/internal/controlnotify"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/safe"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/gofrs/uuid"
)

type SessionSubscriptions struct {
	subscriptions        map[string]map[uint64]types.IRouter // channel -> hash -> router
	metadata             map[string]interface{}              // 客户端元数据
	createdAt            time.Time
	lastActivity         time.Time
	mu                   sync.RWMutex
	manage               *MelodyManager
	client               *MelodyClient
	sr                   *router.ServiceRouter
	req                  *SessionRequest
	identity             *safe.AccessTokenIdentity
	hmacAuth             bool
	hmacExpiryTimer      *time.Timer
	hmacExpired          atomic.Bool
	hmacExpiryGen        atomic.Uint64
	hmacExpiryMu         sync.Mutex
	sessionContext       context.Context
	cancelSession        context.CancelFunc
	hookSlots            chan struct{}
	notificationMu       sync.Mutex
	notificationGen      atomic.Uint64
	notificationExpired  atomic.Bool
	notificationCancel   context.CancelFunc
	notificationState    *controlnotify.AuthSessionState
	notificationWatchdog *time.Timer
}

func NewSessionSubscriptions(manage *MelodyManager, client *MelodyClient, sr *router.ServiceRouter) *SessionSubscriptions {
	sessionContext, cancelSession := context.WithCancel(context.Background())
	return &SessionSubscriptions{
		subscriptions:  make(map[string]map[uint64]types.IRouter),
		client:         client,
		manage:         manage,
		sr:             sr,
		metadata:       make(map[string]interface{}),
		createdAt:      time.Now(),
		lastActivity:   time.Now(),
		sessionContext: sessionContext,
		cancelSession:  cancelSession,
		hookSlots:      make(chan struct{}, 1),
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
		if !s.sessionAuthenticated() {
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
	s.Cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopHMACExpiryTimerLocked()
	s.stopNotificationSessionLocked()
	s.req = nil
	s.identity = nil
	s.hmacAuth = false
	s.hmacExpired.Store(false)
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

// Cancel 立即取消与连接同寿命的认证任务，不等待会话锁。
func (s *SessionSubscriptions) Cancel() {
	if s != nil && s.cancelSession != nil {
		s.cancelSession()
	}
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
func (s *SessionSubscriptions) Logon(req *SessionRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logonLocked(req)
}

func (s *SessionSubscriptions) logonLocked(req *SessionRequest) error {
	if err := req.Validate(); err != nil {
		return webSocketAuthenticationError(err)
	}
	if s == nil || s.manage == nil || s.manage.serviceContext == nil || s.manage.serviceContext.Config == nil {
		return webSocketAuthenticationError(errors.New("authentication context unavailable"))
	}
	if req.Token == "" {
		return s.hmacLogonLocked(req)
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
	notificationState := &controlnotify.AuthSessionState{}
	if identity.Identity.Provider == types.AuthProviderCasdoor {
		if manager == nil {
			return webSocketAuthenticationError(errors.New("revocation authority unavailable"))
		}
		lifetime := s.sessionContext
		if lifetime == nil {
			lifetime = context.Background()
		}
		ctx, cancel := context.WithTimeout(lifetime, 3*time.Second)
		defer cancel()
		if err := manager.Authorize(controlnotify.WithAuthSession(ctx, notificationState), identity.Identity); err != nil {
			return webSocketAuthenticationError(err)
		}
	}
	if s.identity != nil {
		s.unsubscribeUserLocked()
	}
	s.stopHMACExpiryTimerLocked()
	req.userID = identity.UID
	req.userName = identity.Username
	s.req = req
	s.identity = identity
	s.hmacAuth = false
	s.hmacExpired.Store(false)
	s.startNotificationSessionLocked(manager, identity, notificationState)

	return nil
}

func (s *SessionSubscriptions) hmacLogonLocked(req *SessionRequest) error {
	sc := s.manage.serviceContext
	hash := sha256.Sum256(nil)
	args := types.HMACAuthArgs{
		AccessKey: req.ApiKey, Timestamp: strconv.FormatInt(req.Timestamp, 10), Nonce: req.Nonce,
		Signature: req.Signature, RecvWindow: req.RecvWindow,
		Method: "WS", Path: "/ws", PathType: types.PrivateType,
		BodyHashHex: hex.EncodeToString(hash[:]),
	}
	if request := s.webSocketHTTPRequest(); request != nil {
		args.ClientIP = utils.ClientPublicIP(request, sc.Config.TrustedProxies...)
		args.TraceID = strings.TrimSpace(request.Header.Get("X-Trace-Id"))
		if args.TraceID == "" {
			if generated, err := uuid.NewV4(); err == nil {
				args.TraceID = generated.String()
				request.Header.Set("X-Trace-Id", args.TraceID)
			}
		}
	}
	ctx := s.sessionContext
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := sc.InvokeHMACAuth(ctx, args)
	if err != nil {
		return webSocketHMACAuthenticationError(err)
	}
	maxLifetime := time.Duration(sc.Config.Auth.AccessExpire) * time.Second
	if maxLifetime <= 0 {
		maxLifetime = time.Duration(config.DefaultAccessExpireSeconds) * time.Second
	}
	identity, err := safe.BuildHMACAccessIdentity(result, req.ApiKey, types.AuthTypeUser, time.Now().UTC(), maxLifetime)
	if err != nil {
		return webSocketAuthenticationError(err)
	}
	if s.identity != nil {
		s.unsubscribeUserLocked()
	}
	s.stopHMACExpiryTimerLocked()
	req.userID = identity.UID
	req.userName = identity.Username
	sanitizeHMACSessionRequest(req)
	s.req = req
	s.identity = identity
	s.hmacAuth = true
	s.stopNotificationSessionLocked()
	s.hmacExpired.Store(false)
	s.scheduleHMACExpiryLocked(identity)
	return nil
}

func (s *SessionSubscriptions) scheduleHMACExpiryLocked(identity *safe.AccessTokenIdentity) {
	s.stopHMACExpiryTimerLocked()
	if identity == nil || identity.ExpiresAt.IsZero() {
		return
	}
	delay := time.Until(identity.ExpiresAt)
	if delay < 0 {
		delay = 0
	}
	s.hmacExpired.Store(false)
	s.hmacExpiryMu.Lock()
	generation := s.hmacExpiryGen.Add(1)
	s.hmacExpiryTimer = time.AfterFunc(delay, func() { s.expireHMACSession(generation, identity) })
	s.hmacExpiryMu.Unlock()
}

func (s *SessionSubscriptions) expireHMACSession(generation uint64, identity *safe.AccessTokenIdentity) {
	s.hmacExpiryMu.Lock()
	ownedGeneration := generation + 1
	if !s.hmacExpiryGen.CompareAndSwap(generation, ownedGeneration) {
		s.hmacExpiryMu.Unlock()
		return
	}
	s.hmacExpired.Store(true)
	if client := s.client; client != nil {
		_ = client.Close()
	}
	s.hmacExpiryTimer = nil
	s.hmacExpiryMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hmacExpiryGen.Load() != ownedGeneration || !s.hmacAuth || s.identity != identity || time.Now().UTC().Before(identity.ExpiresAt) {
		return
	}
	s.logoutLocked()
}

func (s *SessionSubscriptions) stopHMACExpiryTimerLocked() {
	s.hmacExpiryMu.Lock()
	defer s.hmacExpiryMu.Unlock()
	s.hmacExpiryGen.Add(1)
	if s.hmacExpiryTimer != nil {
		s.hmacExpiryTimer.Stop()
		s.hmacExpiryTimer = nil
	}
}

func (s *SessionSubscriptions) webSocketHTTPRequest() *http.Request {
	if s == nil || s.client == nil || s.client.session == nil {
		return nil
	}
	return s.client.session.Request
}

func sanitizeHMACSessionRequest(req *SessionRequest) {
	if req == nil {
		return
	}
	sum := sha256.Sum256([]byte(req.ApiKey))
	req.ApiKey = hex.EncodeToString(sum[:8])
	req.Signature = ""
	req.Timestamp = 0
	req.Nonce = ""
	req.RecvWindow = ""
}

func (s *SessionSubscriptions) sessionAuthenticated() bool {
	if s != nil && s.notificationExpired.Load() {
		return false
	}
	if s == nil || s.req == nil || s.identity == nil {
		return false
	}
	if s.hmacAuth {
		return !s.hmacExpired.Load() && s.identity.ExpiresAt.After(time.Now().UTC())
	}
	return s.req.Validate() == nil
}

func routeRequiresWebSocketAuth(info *types.RouterInfo) bool {
	return info != nil && (info.GetAuth() || info.GetPathType() == types.PrivateType)
}

// webSocketRouteAuthPolicy 按路由所属的认证域选出验签密钥与 AuthType，与 REST 侧的
// resolveRouteAuthPolicy 同构。三个域的密钥各不相同，因此本域之外的 Token 会在验签
// 阶段就被拒绝，而不是依赖订阅链路后面的护栏。
func webSocketRouteAuthPolicy(cfg *config.ServerConfig, info *types.RouterInfo) (config.AuthSecret, types.AuthType) {
	if info != nil {
		switch info.GetPathType() {
		case types.ServerManagerType:
			return cfg.ServerManageAuth, types.AuthTypeServerManage
		case types.ManageType:
			return cfg.ManageAuth, types.AuthTypeManage
		}
	}
	return cfg.Auth, types.AuthTypeUser
}
func (s *SessionSubscriptions) Status() {

}
func (s *SessionSubscriptions) Logout() *SessionResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logoutLocked()
}

func (s *SessionSubscriptions) logoutLocked() *SessionResponse {
	s.stopHMACExpiryTimerLocked()
	s.stopNotificationSessionLocked()
	s.req = nil
	s.identity = nil
	s.hmacAuth = false
	s.hmacExpired.Store(false)
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
	secret, authType := webSocketRouteAuthPolicy(s.manage.serviceContext.Config, info)
	manager, hook, active := s.manage.serviceContext.GetAuthRequestRuntime()
	if !active {
		return nil, webSocketAuthenticationError(errors.New("service authentication is closing"))
	}
	var verified *safe.AccessTokenIdentity
	if s.hmacAuth {
		provider, hmacActive := s.manage.serviceContext.GetHMACAuthRuntime()
		if !hmacActive || provider == nil || s.identity == nil || s.hmacExpired.Load() || authType != types.AuthTypeUser || s.identity.AuthType != authType {
			return nil, webSocketAuthenticationError(errors.New("HMAC session auth domain is invalid"))
		}
		if !s.identity.ExpiresAt.After(time.Now().UTC()) {
			s.logoutLocked()
			return nil, webSocketAuthenticationError(errors.New("HMAC session expired"))
		}
		verified = s.identity
	} else {
		var err error
		verified, err = safe.ValidateAccessToken(s.req.Token, secret.AccessSecret, authType, time.Now().UTC())
		if err != nil {
			return nil, webSocketAuthenticationError(err)
		}
	}
	if !s.hmacAuth && verified.Identity.Provider == types.AuthProviderCasdoor {
		if manager == nil {
			return nil, webSocketAuthenticationError(errors.New("revocation authority unavailable"))
		}
		ctx := requestContext(req)
		if s.notificationState != nil {
			ctx = controlnotify.WithAuthSession(ctx, s.notificationState)
		}
		if err := manager.Authorize(ctx, verified.Identity); err != nil {
			return nil, webSocketAuthenticationError(err)
		}
	}
	if setter, ok := req.(types.IRequestSecretClaimsSetter); ok && !s.hmacAuth {
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

func webSocketHMACAuthenticationError(err error) error {
	type contractProvider interface {
		PublicErrorContract() types.PublicErrorContract
	}
	var provider contractProvider
	if errors.As(err, &provider) {
		switch provider.PublicErrorContract().Kind {
		case types.ErrorKindUnavailable:
			return types.NewPublicError(types.ErrorKindUnavailable, 0, "", err)
		case types.ErrorKindRateLimited:
			return types.NewPublicError(types.ErrorKindRateLimited, 0, "", err)
		case types.ErrorKindInternal:
			return types.NewPublicError(types.ErrorKindInternal, 0, "", err)
		}
	}
	return webSocketAuthenticationError(err)
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
