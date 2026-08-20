package melody

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/safe"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/golang-jwt/jwt/v4"
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
