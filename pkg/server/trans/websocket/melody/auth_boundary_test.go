package melody

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/safe"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/golang-jwt/jwt/v4"
	"github.com/olahol/melody"
	"github.com/stretchr/testify/require"
)

func TestLogonRejectsCasdoorTokenWhenCasdoorEnabled(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	claims := &casdoorsdk.Claims{
		User: casdoorsdk.User{Id: "casdoor-user", Email: "user@example.com"},
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Second)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(privateKey)
	require.NoError(t, err)

	subscriptions := &SessionSubscriptions{
		manage: &MelodyManager{serviceContext: &router.ServiceContext{
			Config: &config.ServerConfig{Auth: config.AuthSecret{
				AccessSecret: "internal-access-secret",
				CasDoor:      config.CasDoorConfig{Enable: true},
			}},
		}},
	}

	err = subscriptions.Logon(&SessionRequest{Token: token})
	require.Error(t, err)
	require.Nil(t, subscriptions.req)
}

type webSocketAuthHookRecorder struct {
	calls int
	args  types.AuthRequestArgs
	err   error
}

type webSocketHMACProviderFunc func(context.Context, types.HMACAuthArgs) (*types.HMACAuthResult, error)

func (f webSocketHMACProviderFunc) AuthenticateHMAC(ctx context.Context, args types.HMACAuthArgs) (*types.HMACAuthResult, error) {
	return f(ctx, args)
}

type blockingWebSocketAuthHook struct {
	calls   atomic.Int32
	release <-chan struct{}
}

func (h *blockingWebSocketAuthHook) OnAuthRequest(context.Context, types.AuthRequestArgs) error {
	h.calls.Add(1)
	<-h.release
	return nil
}

func (h *webSocketAuthHookRecorder) OnAuthRequest(_ context.Context, args types.AuthRequestArgs) error {
	h.calls++
	h.args = args
	return h.err
}

type webSocketAuthTestRequest struct {
	types.IRequest
	service      string
	secretClaims map[string]string
}

func (*webSocketAuthTestRequest) GetTraceId() string    { return "trace-ws" }
func (*webSocketAuthTestRequest) GetClientIP() string   { return "198.51.100.10" }
func (r *webSocketAuthTestRequest) ServiceName() string { return r.service }
func (r *webSocketAuthTestRequest) SetSecretClaims(claims map[string]string) {
	r.secretClaims = types.CloneSecretClaims(claims)
}
func (r *webSocketAuthTestRequest) GetSecretClaim(key string) (string, bool) {
	value, ok := r.secretClaims[key]
	return value, ok
}

func TestAuthenticatedSubscriptionRevalidatesTokenAndRunsRequestHook(t *testing.T) {
	now := time.Now().UTC()
	claims := safe.NewClaims("user-1", "用户一")
	require.NoError(t, claims.ConfigureSecretData("websocket-access-secret", types.AuthTypeUser))
	require.NoError(t, claims.AddSecretData("api_key", "private-api-key"))
	pair, err := safe.IssueTokenPair(safe.TokenIssueRequest{
		Claims: claims, Identity: types.AuthIdentity{UID: "user-1", Username: "用户一"},
		AuthType: types.AuthTypeUser, IssuedAt: now, AccessSecret: "websocket-access-secret", AccessExpireSeconds: 3600,
	})
	require.NoError(t, err)
	hook := &webSocketAuthHookRecorder{}
	serverConfig := config.NewServiceDefaultConfig("shop", 0)
	serverConfig.Auth.AccessSecret = "websocket-access-secret"
	sc := &router.ServiceContext{
		Config:                  serverConfig,
		AuthRequestHookProvider: hook,
	}
	subscriptions := &SessionSubscriptions{manage: &MelodyManager{serviceContext: sc}}
	require.NoError(t, subscriptions.Logon(&SessionRequest{Token: pair.AccessToken}))
	info := &types.RouterInfo{Path: "/private/orders", Method: "GET", PathType: types.PrivateType, Auth: true}

	request := &webSocketAuthTestRequest{service: "shop"}
	verified, err := subscriptions.authorizeAuthenticatedSubscription(info, request)

	require.NoError(t, err)
	require.Equal(t, "user-1", verified.UID)
	require.Equal(t, 1, hook.calls)
	require.Equal(t, "user-1", hook.args.Identity.UID)
	require.Equal(t, "/private/orders", hook.args.Path)
	require.Equal(t, "trace-ws", hook.args.TraceID)
	require.Equal(t, "private-api-key", hook.args.SecretClaims["api_key"])
	require.NotContains(t, hook.args.Claims, "secret_args")
	secret, ok := request.GetSecretClaim("api_key")
	require.True(t, ok)
	require.Equal(t, "private-api-key", secret)

	subscriptions.req.Token = "tampered-after-logon"
	_, err = subscriptions.authorizeAuthenticatedSubscription(info, &webSocketAuthTestRequest{service: "shop"})
	require.Equal(t, "authentication failed", types.ResolvePublicError(err).Message)
	require.Equal(t, 1, hook.calls, "Token重验失败后不得调用业务Hook")
}

func TestAuthenticatedSubscriptionKeepsBlockingHookExecutionBounded(t *testing.T) {
	now := time.Now().UTC()
	pair, err := safe.IssueTokenPair(safe.TokenIssueRequest{
		Claims: safe.NewClaims("user-1", "用户一"), Identity: types.AuthIdentity{UID: "user-1", Username: "用户一"},
		AuthType: types.AuthTypeUser, IssuedAt: now, AccessSecret: "websocket-access-secret", AccessExpireSeconds: 3600,
	})
	require.NoError(t, err)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	hook := &blockingWebSocketAuthHook{release: release}
	serverConfig := config.NewServiceDefaultConfig("shop", 0)
	serverConfig.Auth.AccessSecret = "websocket-access-secret"
	serverConfig.Timeout = 10
	subscriptions := &SessionSubscriptions{
		manage: &MelodyManager{serviceContext: &router.ServiceContext{
			Config: serverConfig, AuthRequestHookProvider: hook,
		}},
		hookSlots: make(chan struct{}, 1),
	}
	require.NoError(t, subscriptions.Logon(&SessionRequest{Token: pair.AccessToken}))
	info := &types.RouterInfo{Path: "/private/orders", Method: "GET", PathType: types.PrivateType, Auth: true}
	request := &webSocketAuthTestRequest{service: "shop"}

	_, firstErr := subscriptions.authorizeAuthenticatedSubscription(info, request)
	_, secondErr := subscriptions.authorizeAuthenticatedSubscription(info, request)

	require.Equal(t, "internal server error", types.ResolvePublicError(firstErr).Message)
	require.Equal(t, "internal server error", types.ResolvePublicError(secondErr).Message)
	require.Equal(t, int32(1), hook.calls.Load())
	require.Len(t, subscriptions.hookSlots, 1)
}

func TestAuthenticatedSubscriptionOnlyExposesTypedHookMessage(t *testing.T) {
	now := time.Now().UTC()
	pair, err := safe.IssueTokenPair(safe.TokenIssueRequest{
		Claims: safe.NewClaims("user-1", "用户一"), Identity: types.AuthIdentity{UID: "user-1", Username: "用户一"},
		AuthType: types.AuthTypeUser, IssuedAt: now, AccessSecret: "websocket-access-secret", AccessExpireSeconds: 3600,
	})
	require.NoError(t, err)
	hook := &webSocketAuthHookRecorder{err: types.NewPublicError(
		types.ErrorKindForbidden, 40321, "账户已冻结", errors.New("internal account state"),
	)}
	serverConfig := config.NewServiceDefaultConfig("shop", 0)
	serverConfig.Auth.AccessSecret = "websocket-access-secret"
	subscriptions := &SessionSubscriptions{
		manage: &MelodyManager{serviceContext: &router.ServiceContext{
			Config: serverConfig, AuthRequestHookProvider: hook,
		}},
	}
	require.NoError(t, subscriptions.Logon(&SessionRequest{Token: pair.AccessToken}))
	info := &types.RouterInfo{Path: "/private/orders", Method: "GET", PathType: types.PrivateType, Auth: true}

	_, err = subscriptions.authorizeAuthenticatedSubscription(info, &webSocketAuthTestRequest{service: "shop"})

	require.Equal(t, "账户已冻结", webSocketPublicMessage(err))
	require.NotContains(t, webSocketPublicMessage(err), "internal")
}

// TestAuthenticatedSubscriptionSelectsAuthDomainPerRoute 固定 sub 路径的验签按路由所属
// 认证域分流。此前这里把密钥与 AuthType 都写死成用户域，普通用户 Token 因此能通过
// Manage 与 ServerManage 路由的验签，跨域隔离只剩订阅链路后面几道与认证无关的护栏
// （Manage 路由没实现 IWebSocketUserIdentity、Hub 的服务归属校验）在挡。集成用例只断言
// 「跨域订阅失败」，护栏在时看不出验签是否分流，所以这里直接盯住认证层本身。
func TestAuthenticatedSubscriptionSelectsAuthDomainPerRoute(t *testing.T) {
	newUserSession := func(t *testing.T, manageSecret string) *SessionSubscriptions {
		t.Helper()
		pair, err := safe.IssueTokenPair(safe.TokenIssueRequest{
			Claims:              safe.NewClaims("user-1", "用户一"),
			Identity:            types.AuthIdentity{UID: "user-1", Username: "用户一"},
			AuthType:            types.AuthTypeUser,
			IssuedAt:            time.Now().UTC(),
			AccessSecret:        "websocket-access-secret",
			AccessExpireSeconds: 3600,
		})
		require.NoError(t, err)
		serverConfig := config.NewServiceDefaultConfig("shop", 0)
		serverConfig.Auth.AccessSecret = "websocket-access-secret"
		serverConfig.ManageAuth.AccessSecret = manageSecret
		serverConfig.ServerManageAuth.AccessSecret = "servermanage-access-secret"
		subscriptions := &SessionSubscriptions{manage: &MelodyManager{
			serviceContext: &router.ServiceContext{Config: serverConfig},
		}}
		require.NoError(t, subscriptions.Logon(&SessionRequest{Token: pair.AccessToken}))
		return subscriptions
	}
	authRoute := func(pathType types.ApiType) *types.RouterInfo {
		return &types.RouterInfo{Path: "/ws/x", Method: "GET", PathType: pathType, Auth: true}
	}

	t.Run("UserTokenPassesPrivateRoute", func(t *testing.T) {
		verified, err := newUserSession(t, "manage-access-secret").
			authorizeAuthenticatedSubscription(authRoute(types.PrivateType), &webSocketAuthTestRequest{service: "shop"})

		require.NoError(t, err, "用户域 Token 必须仍能进 Private 路由")
		require.Equal(t, "user-1", verified.UID)
	})

	for _, tc := range []struct {
		name     string
		pathType types.ApiType
	}{
		{"ManageRoute", types.ManageType},
		{"ServerManageRoute", types.ServerManagerType},
	} {
		t.Run("UserTokenRejectedOn"+tc.name, func(t *testing.T) {
			_, err := newUserSession(t, "manage-access-secret").
				authorizeAuthenticatedSubscription(authRoute(tc.pathType), &webSocketAuthTestRequest{service: "shop"})

			require.Error(t, err)
			require.Equal(t, "authentication failed", types.ResolvePublicError(err).Message)
		})
	}

	// 即使把两个域的密钥配成同一个，AuthType 仍要把域分开：能否通过验签不能只由密钥决定。
	t.Run("SharedSecretStillRejectsForeignAuthType", func(t *testing.T) {
		_, err := newUserSession(t, "websocket-access-secret").
			authorizeAuthenticatedSubscription(authRoute(types.ManageType), &webSocketAuthTestRequest{service: "shop"})

		require.Error(t, err)
		require.Equal(t, "authentication failed", types.ResolvePublicError(err).Message)
	})
}

// TestHMACLogonCachesFiniteIdentityAndPrivateSubscriptionRunsHook 验证 HMAC logon 只验签一次，Private 订阅仍执行 OnAuthRequest。
func TestHMACLogonCachesFiniteIdentityAndPrivateSubscriptionRunsHook(t *testing.T) {
	hook := &webSocketAuthHookRecorder{}
	providerCalls := 0
	cfg := config.NewServiceDefaultConfig("shop", 0)
	cfg.Auth.AccessExpire = 3600
	sc := &router.ServiceContext{
		Config: cfg, Service: &types.Service{Name: "shop"}, AuthRequestHookProvider: hook,
		HMACAuthProvider: webSocketHMACProviderFunc(func(_ context.Context, args types.HMACAuthArgs) (*types.HMACAuthResult, error) {
			providerCalls++
			require.Equal(t, "WS", args.Method)
			require.Equal(t, "/ws", args.Path)
			require.Equal(t, types.PrivateType, args.PathType)
			require.Equal(t, "key-public", args.AccessKey)
			require.Equal(t, "nonce-1", args.Nonce)
			return &types.HMACAuthResult{Identity: types.AuthIdentity{
				UID: "42", Username: "alice", AuthType: types.AuthTypeUser,
				Provider: "apikey", ProviderSubject: "credential-7",
			}, Claims: map[string]string{"platform_uid": "42"}}, nil
		}),
	}
	subscriptions := &SessionSubscriptions{manage: &MelodyManager{serviceContext: sc}}
	req := &SessionRequest{ApiKey: "key-public", Signature: "signature-secret", Timestamp: 1_900_000_000_000, Nonce: "nonce-1", RecvWindow: "5000"}

	require.NoError(t, subscriptions.Logon(req))
	require.True(t, subscriptions.hmacAuth)
	require.NotNil(t, subscriptions.identity)
	require.True(t, subscriptions.identity.ExpiresAt.After(time.Now()))
	require.Empty(t, subscriptions.req.Signature)
	require.Empty(t, subscriptions.req.Nonce)
	require.Zero(t, subscriptions.req.Timestamp)
	require.Empty(t, subscriptions.req.RecvWindow)
	require.NotEqual(t, "key-public", subscriptions.req.ApiKey)

	verified, err := subscriptions.authorizeAuthenticatedSubscription(
		&types.RouterInfo{Path: "/private/orders", Method: "GET", PathType: types.PrivateType, Auth: true},
		&webSocketAuthTestRequest{service: "shop"},
	)

	require.NoError(t, err)
	require.Equal(t, "42", verified.UID)
	require.Equal(t, "42", hook.args.Claims["platform_uid"])
	require.Equal(t, 1, hook.calls)
	require.Equal(t, 1, providerCalls, "订阅不得重复消费 HMAC nonce")
}

// TestHMACSubscriptionRejectsManageAndExpiredIdentity 验证 HMAC 会话不得跨入 Manage 域且过期后 fail closed。
func TestHMACSubscriptionRejectsManageAndExpiredIdentity(t *testing.T) {
	cfg := config.NewServiceDefaultConfig("shop", 0)
	sc := &router.ServiceContext{
		Config: cfg, Service: &types.Service{Name: "shop"},
		HMACAuthProvider: webSocketHMACProviderFunc(func(context.Context, types.HMACAuthArgs) (*types.HMACAuthResult, error) {
			return &types.HMACAuthResult{Identity: types.AuthIdentity{
				UID: "42", AuthType: types.AuthTypeUser, Provider: "apikey", ProviderSubject: "credential-7",
			}}, nil
		}),
	}
	newSession := func(t *testing.T) *SessionSubscriptions {
		t.Helper()
		subscriptions := &SessionSubscriptions{manage: &MelodyManager{serviceContext: sc}}
		require.NoError(t, subscriptions.Logon(&SessionRequest{
			ApiKey: "key-public", Signature: "signature-secret", Timestamp: 1_900_000_000_000, Nonce: "nonce-1",
		}))
		return subscriptions
	}

	manageSession := newSession(t)
	_, err := manageSession.authorizeAuthenticatedSubscription(
		&types.RouterInfo{Path: "/manage/orders", Method: "GET", PathType: types.ManageType, Auth: true},
		&webSocketAuthTestRequest{service: "shop"},
	)
	require.Equal(t, "authentication failed", types.ResolvePublicError(err).Message)

	expiredSession := newSession(t)
	expiredSession.identity.ExpiresAt = time.Now().Add(-time.Second)
	_, err = expiredSession.authorizeAuthenticatedSubscription(
		&types.RouterInfo{Path: "/private/orders", Method: "GET", PathType: types.PrivateType, Auth: true},
		&webSocketAuthTestRequest{service: "shop"},
	)
	require.Equal(t, "authentication failed", types.ResolvePublicError(err).Message)
	require.Nil(t, expiredSession.identity)
	require.False(t, expiredSession.hmacAuth)
}

// TestHMACLogonFailureIsGenericAndBearerTakesPriority 验证 logon 失败脱敏且 Token 始终优先。
func TestHMACLogonFailureIsGenericAndBearerTakesPriority(t *testing.T) {
	providerCalls := 0
	cfg := config.NewServiceDefaultConfig("shop", 0)
	sc := &router.ServiceContext{
		Config: cfg,
		HMACAuthProvider: webSocketHMACProviderFunc(func(context.Context, types.HMACAuthArgs) (*types.HMACAuthResult, error) {
			providerCalls++
			return nil, types.NewPublicError(types.ErrorKindForbidden, 40321, "key exists", errors.New("signature detail"))
		}),
	}
	subscriptions := &SessionSubscriptions{manage: &MelodyManager{serviceContext: sc}}

	err := subscriptions.Logon(&SessionRequest{
		ApiKey: "key-public", Signature: "signature-secret", Timestamp: 1_900_000_000_000, Nonce: "nonce-1",
	})
	require.Equal(t, "authentication failed", webSocketPublicMessage(err))
	require.Nil(t, subscriptions.req)
	require.Equal(t, 1, providerCalls)

	jwt := websocketAccessToken(t, cfg.Auth.AccessSecret, "user-1")
	require.NoError(t, subscriptions.Logon(&SessionRequest{
		Token: jwt, ApiKey: "key-public", Signature: "signature-secret", Timestamp: 1_900_000_000_000, Nonce: "nonce-2",
	}))
	require.False(t, subscriptions.hmacAuth)
	require.Equal(t, 1, providerCalls)
}

// TestSessionRequestRequiresNonceForHMAC 验证无 Token 的 logon 必须提供 nonce 防重放材料。
func TestSessionRequestRequiresNonceForHMAC(t *testing.T) {
	err := (&SessionRequest{ApiKey: "key-public", Signature: "signature", Timestamp: 1_900_000_000_000}).Validate()
	require.Error(t, err)
}

// TestHMACSessionExpiresWithoutAnotherSubscription 验证无后续消息时 HMAC 会话也会主动过期。
func TestHMACSessionExpiresWithoutAnotherSubscription(t *testing.T) {
	cfg := config.NewServiceDefaultConfig("shop", 0)
	sc := &router.ServiceContext{
		Config: cfg,
		HMACAuthProvider: webSocketHMACProviderFunc(func(context.Context, types.HMACAuthArgs) (*types.HMACAuthResult, error) {
			return &types.HMACAuthResult{Identity: types.AuthIdentity{
				UID: "42", AuthType: types.AuthTypeUser, Provider: "apikey", ProviderSubject: "credential-7",
				ExpiresAt: time.Now().Add(30 * time.Millisecond),
			}}, nil
		}),
	}
	subscriptions := &SessionSubscriptions{manage: &MelodyManager{serviceContext: sc}}
	require.NoError(t, subscriptions.Logon(&SessionRequest{
		ApiKey: "key-public", Signature: "signature-secret", Timestamp: 1_900_000_000_000, Nonce: "nonce-1",
	}))

	require.Eventually(t, func() bool {
		subscriptions.mu.RLock()
		defer subscriptions.mu.RUnlock()
		return subscriptions.identity == nil && !subscriptions.hmacAuth
	}, time.Second, 10*time.Millisecond)
}

// TestHMACSessionDisconnectCancelsAuthentication 验证连接清理会立即取消进行中的 HMAC Hook。
func TestHMACSessionDisconnectCancelsAuthentication(t *testing.T) {
	cfg := config.NewServiceDefaultConfig("shop", 0)
	cfg.Timeout = 5000
	entered := make(chan struct{})
	canceled := make(chan struct{})
	sc := &router.ServiceContext{
		Config: cfg,
		HMACAuthProvider: webSocketHMACProviderFunc(func(ctx context.Context, _ types.HMACAuthArgs) (*types.HMACAuthResult, error) {
			close(entered)
			<-ctx.Done()
			close(canceled)
			return nil, ctx.Err()
		}),
	}
	subscriptions := NewSessionSubscriptions(&MelodyManager{serviceContext: sc}, nil, nil)
	logonDone := make(chan struct{})
	go func() {
		defer close(logonDone)
		_ = subscriptions.Logon(&SessionRequest{
			ApiKey: "key-public", Signature: "signature-secret", Timestamp: 1_900_000_000_000, Nonce: "nonce-1",
		})
	}()
	<-entered
	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)
		subscriptions.UnsubscribeAll()
	}()

	select {
	case <-canceled:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("断连应立即取消进行中的 HMAC 认证")
	}
	select {
	case <-cleanupDone:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("断连清理不应等待 HMAC 超时")
	}
	<-logonDone
}

// TestCleanupSessionCancelsHMACAuthenticationAndRemovesSession 验证真实 manager 断连入口取消认证并删除会话。
func TestCleanupSessionCancelsHMACAuthenticationAndRemovesSession(t *testing.T) {
	cfg := config.NewServiceDefaultConfig("shop", 0)
	cfg.Timeout = 5000
	entered := make(chan struct{})
	canceled := make(chan struct{})
	sc := &router.ServiceContext{
		Config: cfg,
		HMACAuthProvider: webSocketHMACProviderFunc(func(ctx context.Context, _ types.HMACAuthArgs) (*types.HMACAuthResult, error) {
			close(entered)
			<-ctx.Done()
			close(canceled)
			return nil, ctx.Err()
		}),
	}
	session := &melody.Session{Request: &http.Request{RemoteAddr: "127.0.0.1:10000", Header: make(http.Header)}}
	manager := &MelodyManager{serviceContext: sc, subscriptions: make(map[*melody.Session]*SessionSubscriptions)}
	subscriptions := NewSessionSubscriptions(manager, &MelodyClient{session: session, manager: manager}, nil)
	manager.subscriptions[session] = subscriptions
	go func() {
		_ = subscriptions.Logon(&SessionRequest{
			ApiKey: "key-public", Signature: "signature-secret", Timestamp: 1_900_000_000_000, Nonce: "nonce-1",
		})
	}()
	<-entered

	manager.cleanupSession(session)

	select {
	case <-canceled:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("真实断连清理入口应立即取消 HMAC 认证")
	}
	manager.subscriptionsMu.RLock()
	_, exists := manager.subscriptions[session]
	manager.subscriptionsMu.RUnlock()
	require.False(t, exists)
}

// TestHMACSessionDisconnectStopsExpiryTimerAndClearsIdentity 验证断连释放 timer、凭证快照和可信身份。
func TestHMACSessionDisconnectStopsExpiryTimerAndClearsIdentity(t *testing.T) {
	subscriptions := NewSessionSubscriptions(nil, nil, nil)
	identity := &safe.AccessTokenIdentity{
		UID: "42", ExpiresAt: time.Now().Add(time.Hour),
		Identity: types.AuthIdentity{UID: "42", AuthType: types.AuthTypeUser, Provider: "apikey", ProviderSubject: "credential-7"},
	}
	subscriptions.identity = identity
	subscriptions.hmacAuth = true
	subscriptions.scheduleHMACExpiryLocked(identity)
	require.NotNil(t, subscriptions.hmacExpiryTimer)

	subscriptions.UnsubscribeAll()

	require.Nil(t, subscriptions.hmacExpiryTimer)
	require.Nil(t, subscriptions.identity)
	require.Nil(t, subscriptions.req)
	require.False(t, subscriptions.hmacAuth)
}

// TestHMACSessionMarksExpiredBeforeWaitingForSessionLock 验证过期标记不被慢 OnAuthRequest 占用的会话锁阻塞。
func TestHMACSessionMarksExpiredBeforeWaitingForSessionLock(t *testing.T) {
	subscriptions := NewSessionSubscriptions(nil, nil, nil)
	identity := &safe.AccessTokenIdentity{
		UID: "42", ExpiresAt: time.Now().Add(30 * time.Millisecond),
		Identity: types.AuthIdentity{UID: "42", AuthType: types.AuthTypeUser, Provider: "apikey", ProviderSubject: "credential-7"},
	}
	subscriptions.identity = identity
	subscriptions.hmacAuth = true
	subscriptions.scheduleHMACExpiryLocked(identity)
	subscriptions.mu.Lock()

	require.Eventually(t, subscriptions.hmacExpired.Load, time.Second, 10*time.Millisecond)
	require.False(t, subscriptions.sessionAuthenticated())
	subscriptions.mu.Unlock()
}

// TestStaleHMACExpiryCannotInvalidateReplacementIdentity 验证旧 timer 不得失效重新登录后的新身份。
func TestStaleHMACExpiryCannotInvalidateReplacementIdentity(t *testing.T) {
	subscriptions := NewSessionSubscriptions(nil, nil, nil)
	oldIdentity := &safe.AccessTokenIdentity{
		UID: "old", ExpiresAt: time.Now().Add(-time.Second),
		Identity: types.AuthIdentity{UID: "old", AuthType: types.AuthTypeUser, Provider: "apikey", ProviderSubject: "credential-old"},
	}
	oldGeneration := subscriptions.hmacExpiryGen.Add(1)
	subscriptions.stopHMACExpiryTimerLocked()
	newIdentity := &safe.AccessTokenIdentity{
		UID: "new", ExpiresAt: time.Now().Add(time.Hour),
		Identity: types.AuthIdentity{UID: "new", AuthType: types.AuthTypeUser, Provider: "apikey", ProviderSubject: "credential-new"},
	}
	subscriptions.identity = newIdentity
	subscriptions.hmacAuth = true
	subscriptions.hmacExpired.Store(false)

	subscriptions.expireHMACSession(oldGeneration, oldIdentity)

	require.False(t, subscriptions.hmacExpired.Load())
	require.Same(t, newIdentity, subscriptions.identity)
	require.True(t, subscriptions.hmacAuth)
}

// TestMelodyClientCloseMarksDeliveryClosedSynchronously 验证过期关闭会在底层关闭帧排队前立即禁止投递。
func TestMelodyClientCloseMarksDeliveryClosedSynchronously(t *testing.T) {
	client := &MelodyClient{}

	require.NoError(t, client.Close())

	client.stateMu.RLock()
	closed := client.closed
	client.stateMu.RUnlock()
	require.True(t, closed)
	require.True(t, client.IsClosed())
}

func websocketAccessToken(t *testing.T, secret, uid string) string {
	t.Helper()
	pair, err := safe.IssueTokenPair(safe.TokenIssueRequest{
		Claims: safe.NewClaims(uid, "user"), Identity: types.AuthIdentity{UID: uid}, AuthType: types.AuthTypeUser,
		IssuedAt: time.Now().UTC(), AccessSecret: secret, AccessExpireSeconds: 3600,
	})
	require.NoError(t, err)
	return pair.AccessToken
}
