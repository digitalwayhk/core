package rest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/authstate"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/safe"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
	zeroresthandler "github.com/zeromicro/go-zero/rest/handler"
)

func TestAuthRequestHookRunsAfterJWTBeforeRouter(t *testing.T) {
	var callsMu sync.Mutex
	calls := []string{}
	hook := authRequestHookFunc(func(_ context.Context, args types.AuthRequestArgs) error {
		callsMu.Lock()
		calls = append(calls, "hook")
		callsMu.Unlock()
		require.Equal(t, "alice", args.Identity.ProviderSubject)
		require.Equal(t, "user-1", args.Identity.UID)
		require.Equal(t, "/private/orders", args.Path)
		return nil
	})
	sc := authRequestServiceContext(hook)
	manager, err := authstate.NewManager("auth-request-test", config.AuthRevocationConfig{
		Mode: config.AuthRevocationModeLocal, BadgerPath: t.TempDir(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	sc.AuthRevocationManager = manager
	info := authRequestRouterInfo(types.PrivateType)
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		callsMu.Lock()
		calls = append(calls, "router")
		callsMu.Unlock()
	})
	handler := zeroresthandler.Authorize(sc.Config.Auth.AccessSecret)(
		authRequestHandler(sc, info, types.AuthTypeUser, authModeInternalJWT, next),
	)
	request := authenticatedRequest(t, sc.Config.Auth.AccessSecret, types.AuthIdentity{
		UID: "user-1", Username: "用户一", AuthType: types.AuthTypeUser,
		Provider: types.AuthProviderCasdoor, ProviderSubject: "alice", Generation: 0,
	})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, []string{"hook", "router"}, calls)
}

func TestCasdoorAuthorityUnavailableRejectsProtectedRequest(t *testing.T) {
	sc := authRequestServiceContext(authRequestHookFunc(func(context.Context, types.AuthRequestArgs) error {
		t.Fatal("撤销权威失败时不得执行业务Hook")
		return nil
	}))
	info := authRequestRouterInfo(types.PrivateType)
	called := false
	handler := zeroresthandler.Authorize(sc.Config.Auth.AccessSecret)(
		authRequestHandler(sc, info, types.AuthTypeUser, authModeInternalJWT, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			called = true
		})),
	)
	request := authenticatedRequest(t, sc.Config.Auth.AccessSecret, types.AuthIdentity{
		UID: "user-1", AuthType: types.AuthTypeUser, Provider: types.AuthProviderCasdoor,
		ProviderSubject: "alice", Generation: 2,
	})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.False(t, called)
	require.NotContains(t, recorder.Body.String(), "authority")
}

func TestAuthRequestRejectsTokenFromWrongAuthDomain(t *testing.T) {
	sc := authRequestServiceContext(nil)
	sc.Config.ManageAuth.AccessSecret = sc.Config.Auth.AccessSecret
	info := authRequestRouterInfo(types.ManageType)
	called := false
	handler := zeroresthandler.Authorize(sc.Config.ManageAuth.AccessSecret)(
		authRequestHandler(sc, info, types.AuthTypeManage, authModeInternalJWT, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			called = true
		})),
	)
	request := authenticatedRequest(t, sc.Config.Auth.AccessSecret, types.AuthIdentity{
		UID: "user-1", AuthType: types.AuthTypeUser,
	})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.False(t, called)
}

func TestLogtoIdentityRunsBusinessHookWithoutCasdoorAuthority(t *testing.T) {
	called := false
	sc := authRequestServiceContext(authRequestHookFunc(func(_ context.Context, args types.AuthRequestArgs) error {
		called = true
		require.Equal(t, types.AuthProviderLogto, args.Identity.Provider)
		require.Equal(t, "logto-user", args.Identity.ProviderSubject)
		return nil
	}))
	info := authRequestRouterInfo(types.PrivateType)
	handler := authRequestHandler(sc, info, types.AuthTypeUser, authModeLogto, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	request := httptest.NewRequest(http.MethodGet, info.GetPath(), nil)
	ctx := context.WithValue(request.Context(), "uid", "logto-user")
	request = request.WithContext(context.WithValue(ctx, "uname", "Logto User"))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, called)
}

func TestAuthRequestHookFailureContract(t *testing.T) {
	tests := []struct {
		name   string
		hook   types.IAuthRequestHookProvider
		status int
		body   string
	}{
		{name: "panic", hook: authRequestHookFunc(func(context.Context, types.AuthRequestArgs) error { panic("secret panic") }), status: 500, body: "internal server error"},
		{name: "timeout", hook: authRequestHookFunc(func(ctx context.Context, _ types.AuthRequestArgs) error { <-ctx.Done(); return ctx.Err() }), status: 500, body: "internal server error"},
		{name: "public", hook: authRequestHookFunc(func(context.Context, types.AuthRequestArgs) error {
			return types.NewPublicError(types.ErrorKindForbidden, 40321, "账户已冻结", errors.New("secret state"))
		}), status: 403, body: "账户已冻结"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := authRequestServiceContext(tt.hook)
			sc.Config.Timeout = 10
			info := authRequestRouterInfo(types.PrivateType)
			handler := zeroresthandler.Authorize(sc.Config.Auth.AccessSecret)(
				authRequestHandler(sc, info, types.AuthTypeUser, authModeInternalJWT, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					t.Fatal("Hook失败时不得执行Router")
				})),
			)
			request := authenticatedRequest(t, sc.Config.Auth.AccessSecret, types.AuthIdentity{UID: "user-1", AuthType: types.AuthTypeUser})
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			require.Equal(t, tt.status, recorder.Code)
			require.Contains(t, recorder.Body.String(), tt.body)
			require.NotContains(t, recorder.Body.String(), "secret")
		})
	}
}

type authRequestHookFunc func(context.Context, types.AuthRequestArgs) error

func (f authRequestHookFunc) OnAuthRequest(ctx context.Context, args types.AuthRequestArgs) error {
	return f(ctx, args)
}

func authRequestServiceContext(hook types.IAuthRequestHookProvider) *router.ServiceContext {
	cfg := config.NewServiceDefaultConfig("auth-request-test", 18091)
	cfg.Auth.AccessSecret = "auth-access-secret"
	cfg.Auth.AccessExpire = 3600
	cfg.ManageAuth.AccessSecret = "manage-access-secret"
	cfg.ManageAuth.AccessExpire = 3600
	return &router.ServiceContext{
		Config:                  cfg,
		Service:                 &types.Service{Name: "auth-request-test"},
		AuthRequestHookProvider: hook,
	}
}

func authRequestRouterInfo(pathType types.ApiType) *types.RouterInfo {
	return &types.RouterInfo{
		Path: "/private/orders", Method: http.MethodGet, Auth: true,
		PathType: pathType, ServiceName: "auth-request-test",
	}
}

func authenticatedRequest(t *testing.T, secret string, identity types.AuthIdentity) *http.Request {
	t.Helper()
	now := time.Now().UTC().Add(-time.Second)
	claims := safe.NewClaims(identity.UID, identity.Username)
	pair, err := safe.IssueTokenPair(safe.TokenIssueRequest{
		Claims: claims, Identity: identity, AuthType: identity.AuthType, IssuedAt: now,
		AccessSecret: secret, AccessExpireSeconds: 3600,
	})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodGet, "/private/orders", nil)
	request.RemoteAddr = "198.51.100.10:4321"
	request.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	return request
}
