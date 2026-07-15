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
	service string
}

func (*webSocketAuthTestRequest) GetTraceId() string    { return "trace-ws" }
func (*webSocketAuthTestRequest) GetClientIP() string   { return "198.51.100.10" }
func (r *webSocketAuthTestRequest) ServiceName() string { return r.service }

func TestAuthenticatedSubscriptionRevalidatesTokenAndRunsRequestHook(t *testing.T) {
	now := time.Now().UTC()
	pair, err := safe.IssueTokenPair(safe.TokenIssueRequest{
		Claims: safe.NewClaims("user-1", "用户一"), Identity: types.AuthIdentity{UID: "user-1", Username: "用户一"},
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

	verified, err := subscriptions.authorizeAuthenticatedSubscription(info, &webSocketAuthTestRequest{service: "shop"})

	require.NoError(t, err)
	require.Equal(t, "user-1", verified.UID)
	require.Equal(t, 1, hook.calls)
	require.Equal(t, "user-1", hook.args.Identity.UID)
	require.Equal(t, "/private/orders", hook.args.Path)
	require.Equal(t, "trace-ws", hook.args.TraceID)

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
