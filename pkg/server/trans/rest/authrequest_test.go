package rest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/authstate"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/safe"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/logx"
)

func TestInternalJWTAuthorizeDoesNotLogToken(t *testing.T) {
	var output bytes.Buffer
	previous := logx.Reset()
	logx.SetWriter(logx.NewWriter(&output))
	t.Cleanup(func() {
		logx.SetWriter(previous)
		logx.Reset()
	})

	const rawToken = "integration-secret-token-value"
	request := httptest.NewRequest(http.MethodGet, "/private", nil)
	request.Header.Set("Authorization", "Bearer "+rawToken)
	response := httptest.NewRecorder()
	internalJWTAuthorize(nil, nil, "access-secret", types.AuthTypeUser, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("无效 Token 不得进入下游")
	})).ServeHTTP(response, request)

	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.NotContains(t, output.String(), rawToken)
	require.NotContains(t, output.String(), "Authorization")
}

func TestInternalJWTAuthorizePassesTrustedVerifiedIdentity(t *testing.T) {
	sc := authRequestServiceContext(nil)
	identity := types.AuthIdentity{
		UID: "user-1", Username: "用户一", AuthType: types.AuthTypeUser,
		Provider: types.AuthProviderCasdoor, ProviderSubject: "alice", Generation: 3,
	}
	request := authenticatedRequest(t, sc.Config.Auth.AccessSecret, identity)
	recorder := httptest.NewRecorder()
	called := false
	handler := internalJWTAuthorize(sc, authRequestRouterInfo(types.PrivateType), sc.Config.Auth.AccessSecret, types.AuthTypeUser,
		http.HandlerFunc(func(_ http.ResponseWriter, verifiedRequest *http.Request) {
			called = true
			verifiedRequest.Header.Del("Authorization")
			actualIdentity, claims, err := verifiedRequestIdentity(
				verifiedRequest, sc, types.AuthTypeUser,
			)
			require.NoError(t, err)
			require.Equal(t, identity.UID, actualIdentity.UID)
			require.Equal(t, identity.ProviderSubject, actualIdentity.ProviderSubject)
			require.Equal(t, identity.Generation, actualIdentity.Generation)
			require.Equal(t, identity.UID, claims["uid"])
		}),
	)

	handler.ServeHTTP(recorder, request)

	require.True(t, called)
	require.Equal(t, http.StatusOK, recorder.Code)
}

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
	handler := internalJWTAuthorize(sc, info, sc.Config.Auth.AccessSecret, types.AuthTypeUser,
		authRequestHandler(sc, info, types.AuthTypeUser, next),
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

func TestAuthRequestAuthorityUsesAuthorityRevocationAndTargetHook(t *testing.T) {
	hookCalled := false
	target := authRequestServiceContext(authRequestHookFunc(func(_ context.Context, args types.AuthRequestArgs) error {
		hookCalled = true
		require.Equal(t, "target-service", args.ServiceName)
		require.Equal(t, types.AuthTypeManage, args.Identity.AuthType)
		return nil
	}))
	target.Service.Name = "target-service"
	target.Config.Name = "target-service"

	authority := authRequestServiceContext(nil)
	authority.Service.Name = "authority-service"
	authority.Config.Name = "authority-service"
	manager, err := authstate.NewManager("authority-service", config.AuthRevocationConfig{
		Mode: config.AuthRevocationModeLocal, BadgerPath: t.TempDir(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	authority.AuthRevocationManager = manager

	info := authRequestRouterInfo(types.ServerManagerType)
	nextCalled := false
	handler := internalJWTAuthorize(
		authority,
		info,
		authority.Config.ManageAuth.AccessSecret,
		types.AuthTypeManage,
		authRequestHandlerWithAuthority(
			target,
			authority,
			info,
			types.AuthTypeManage,
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				nextCalled = true
			}),
		),
	)
	request := authenticatedRequest(t, authority.Config.ManageAuth.AccessSecret, types.AuthIdentity{
		UID: "admin-1", AuthType: types.AuthTypeManage,
		Provider: types.AuthProviderCasdoor, ProviderSubject: "admin-subject",
	})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.True(t, hookCalled)
	require.True(t, nextCalled)
}

func TestSecretClaimsOnlyUseVerifiedServerSideChannel(t *testing.T) {
	hook := authRequestHookFunc(func(_ context.Context, args types.AuthRequestArgs) error {
		require.Equal(t, "private-api-key", args.SecretClaims["api_key"])
		require.NotContains(t, args.Claims, "api_key")
		require.NotContains(t, args.Claims, "secret_args")
		return nil
	})
	sc := authRequestServiceContext(hook)
	info := authRequestRouterInfo(types.PrivateType)
	handler := internalJWTAuthorize(sc, info, sc.Config.Auth.AccessSecret, types.AuthTypeUser,
		authRequestHandler(sc, info, types.AuthTypeUser, http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			require.Nil(t, request.Context().Value("api_key"))
			require.Equal(t, "private-api-key", safe.VerifiedSecretClaimsFromContext(request.Context())["api_key"])
		})),
	)
	request := authenticatedRequestWithSecret(t, sc.Config.Auth.AccessSecret, types.AuthIdentity{
		UID: "user-1", Username: "用户一", AuthType: types.AuthTypeUser,
	}, "api_key", "private-api-key")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestCasdoorAuthorityUnavailableRejectsProtectedRequest(t *testing.T) {
	sc := authRequestServiceContext(authRequestHookFunc(func(context.Context, types.AuthRequestArgs) error {
		t.Fatal("撤销权威失败时不得执行业务Hook")
		return nil
	}))
	info := authRequestRouterInfo(types.PrivateType)
	called := false
	handler := internalJWTAuthorize(sc, info, sc.Config.Auth.AccessSecret, types.AuthTypeUser,
		authRequestHandler(sc, info, types.AuthTypeUser, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
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
	handler := internalJWTAuthorize(sc, info, sc.Config.ManageAuth.AccessSecret, types.AuthTypeManage,
		authRequestHandler(sc, info, types.AuthTypeManage, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
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

func TestVerifiedAccessIdentityMustMatchRouteAuthType(t *testing.T) {
	tests := []struct {
		name     string
		route    types.AuthType
		identity types.AuthType
	}{
		{name: "user token cannot enter manage", route: types.AuthTypeManage, identity: types.AuthTypeUser},
		{name: "manage token cannot enter user", route: types.AuthTypeUser, identity: types.AuthTypeManage},
		{name: "server-manage token cannot enter manage", route: types.AuthTypeManage, identity: types.AuthTypeServerManage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request = request.WithContext(context.WithValue(
				request.Context(),
				verifiedAccessContextKey{},
				verifiedAccessContext{identity: types.AuthIdentity{UID: "1", AuthType: tt.identity}},
			))
			_, _, err := verifiedRequestIdentity(request, authRequestServiceContext(nil), tt.route)
			require.Error(t, err)
		})
	}
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
			handler := internalJWTAuthorize(sc, info, sc.Config.Auth.AccessSecret, types.AuthTypeUser,
				authRequestHandler(sc, info, types.AuthTypeUser, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
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

func TestAuthRequestDeniedLogContainsRedactedIdentityDigest(t *testing.T) {
	var output bytes.Buffer
	previous := logx.Reset()
	logx.SetWriter(logx.NewWriter(&output))
	t.Cleanup(func() {
		logx.SetWriter(previous)
		logx.Reset()
	})

	sc := authRequestServiceContext(authRequestHookFunc(func(context.Context, types.AuthRequestArgs) error {
		return types.NewPublicError(types.ErrorKindForbidden, 40321, "账户已冻结", nil)
	}))
	manager, err := authstate.NewManager("auth-request-log-test", config.AuthRevocationConfig{
		Mode: config.AuthRevocationModeLocal, BadgerPath: t.TempDir(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	sc.AuthRevocationManager = manager
	identity := types.AuthIdentity{
		UID: "sensitive-user-id", Username: "敏感用户名", AuthType: types.AuthTypeUser,
		Provider: types.AuthProviderCasdoor, ProviderSubject: "sensitive-subject",
	}
	info := authRequestRouterInfo(types.PrivateType)
	handler := internalJWTAuthorize(sc, info, sc.Config.Auth.AccessSecret, types.AuthTypeUser,
		authRequestHandler(sc, authRequestRouterInfo(types.PrivateType), types.AuthTypeUser,
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("拒绝请求不得进入Router") })),
	)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, authenticatedRequest(t, sc.Config.Auth.AccessSecret, identity))

	sum := sha256.Sum256([]byte("auth-request-test|auth|casdoor|sensitive-subject"))
	expectedDigest := hex.EncodeToString(sum[:8])
	logOutput := output.String()
	require.Contains(t, logOutput, "auth_type")
	require.Contains(t, logOutput, string(types.AuthTypeUser))
	require.Contains(t, logOutput, "identity_hash")
	require.Contains(t, logOutput, expectedDigest)
	require.NotContains(t, logOutput, identity.UID)
	require.NotContains(t, logOutput, identity.Username)
	require.NotContains(t, logOutput, identity.ProviderSubject)
}

// TestInternalJWTAuthorizeAuthenticatesHMACBeforeAuthRequestHook 验证 Auth REST 按 HMAC、OnAuthRequest、Router 顺序执行。
func TestInternalJWTAuthorizeAuthenticatesHMACBeforeAuthRequestHook(t *testing.T) {
	calls := make([]string, 0, 3)
	sc := authRequestServiceContext(authRequestHookFunc(func(_ context.Context, args types.AuthRequestArgs) error {
		calls = append(calls, "auth-request")
		require.Equal(t, "42", args.Claims["platform_uid"])
		return nil
	}))
	sc.HMACAuthProvider = hmacAuthProviderFunc(func(_ context.Context, args types.HMACAuthArgs) (*types.HMACAuthResult, error) {
		calls = append(calls, "hmac")
		require.Equal(t, http.MethodPost, args.Method)
		require.Equal(t, "/private/orders", args.Path)
		require.Equal(t, "symbol=BTCUSDT", args.Query)
		require.Equal(t, "payload", args.AccessKey)
		require.Equal(t, sha256Hex([]byte("request-body")), args.BodyHashHex)
		return &types.HMACAuthResult{Identity: types.AuthIdentity{
			UID: "42", Username: "alice", AuthType: types.AuthTypeUser,
			Provider: "apikey", ProviderSubject: "credential-7",
		}, Claims: map[string]string{"platform_uid": "42"}}, nil
	})
	info := authRequestRouterInfo(types.PrivateType)
	info.Method = http.MethodPost
	handler := internalJWTAuthorize(sc, info, sc.Config.Auth.AccessSecret, types.AuthTypeUser,
		authRequestHandler(sc, info, types.AuthTypeUser, http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			calls = append(calls, "router")
			body := make([]byte, len("request-body"))
			_, err := request.Body.Read(body)
			require.NoError(t, err)
			require.Equal(t, "request-body", string(body))
		})),
	)
	request := hmacRequest(http.MethodPost, "/private/orders?symbol=BTCUSDT", "request-body")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, []string{"hmac", "auth-request", "router"}, calls)
}

// TestInternalJWTAuthorizeBearerTakesPriorityOverHMAC 验证同时存在两类凭证时只执行 Bearer 路径。
func TestInternalJWTAuthorizeBearerTakesPriorityOverHMAC(t *testing.T) {
	sc := authRequestServiceContext(nil)
	called := false
	sc.HMACAuthProvider = hmacAuthProviderFunc(func(context.Context, types.HMACAuthArgs) (*types.HMACAuthResult, error) {
		called = true
		return nil, nil
	})
	request := hmacRequest(http.MethodGet, "/private/orders", "")
	request.Header.Set("Authorization", "Bearer invalid-token")
	recorder := httptest.NewRecorder()

	internalJWTAuthorize(sc, authRequestRouterInfo(types.PrivateType), sc.Config.Auth.AccessSecret, types.AuthTypeUser,
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("无效 Bearer 不得进入下游") }),
	).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.False(t, called)
}

// TestInternalJWTAuthorizeRejectsHMACOutsideAuthDomain 验证 Manage 与 ServerManage 认证域不得使用 HMAC Hook。
func TestInternalJWTAuthorizeRejectsHMACOutsideAuthDomain(t *testing.T) {
	sc := authRequestServiceContext(nil)
	called := false
	sc.HMACAuthProvider = hmacAuthProviderFunc(func(context.Context, types.HMACAuthArgs) (*types.HMACAuthResult, error) {
		called = true
		return nil, nil
	})
	recorder := httptest.NewRecorder()

	internalJWTAuthorize(sc, authRequestRouterInfo(types.ManageType), sc.Config.ManageAuth.AccessSecret, types.AuthTypeManage,
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("Manage 不得使用 HMAC") }),
	).ServeHTTP(recorder, hmacRequest(http.MethodGet, "/manage", ""))

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.False(t, called)
}

// TestInternalJWTAuthorizeBoundsHMACBodyBeforeHook 验证超限 body 以统一 JSON 413 拒绝且不调用 Provider。
func TestInternalJWTAuthorizeBoundsHMACBodyBeforeHook(t *testing.T) {
	sc := authRequestServiceContext(nil)
	sc.Config.MaxBytes = 4
	called := false
	sc.HMACAuthProvider = hmacAuthProviderFunc(func(context.Context, types.HMACAuthArgs) (*types.HMACAuthResult, error) {
		called = true
		return nil, nil
	})
	recorder := httptest.NewRecorder()

	internalJWTAuthorize(sc, authRequestRouterInfo(types.PrivateType), sc.Config.Auth.AccessSecret, types.AuthTypeUser,
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("超限请求不得进入下游") }),
	).ServeHTTP(recorder, hmacRequest(http.MethodPost, "/private/orders", "12345"))

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	require.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
	require.JSONEq(t, `{"success":false,"code":41300,"message":"request entity too large"}`, recorder.Body.String())
	require.False(t, called)
}

// TestInternalJWTAuthorizeHMACFailureIsGenericAndRedacted 验证验签失败的响应与日志不泄露凭证或 body。
func TestInternalJWTAuthorizeHMACFailureIsGenericAndRedacted(t *testing.T) {
	var output bytes.Buffer
	previous := logx.Reset()
	logx.SetWriter(logx.NewWriter(&output))
	t.Cleanup(func() { logx.SetWriter(previous); logx.Reset() })
	sc := authRequestServiceContext(nil)
	sc.HMACAuthProvider = hmacAuthProviderFunc(func(context.Context, types.HMACAuthArgs) (*types.HMACAuthResult, error) {
		return nil, types.NewPublicError(types.ErrorKindForbidden, 40321, "key exists but signature failed", errors.New("secret detail"))
	})
	request := hmacRequest(http.MethodPost, "/private/orders", "sensitive-body")
	request.Header.Set(sc.Config.HMACAuth.SignatureHeader, "sensitive-signature")
	recorder := httptest.NewRecorder()

	internalJWTAuthorize(sc, authRequestRouterInfo(types.PrivateType), sc.Config.Auth.AccessSecret, types.AuthTypeUser,
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("失败认证不得进入下游") }),
	).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Contains(t, recorder.Body.String(), "authentication failed")
	for _, secret := range []string{"payload", "sensitive-signature", "sensitive-body", "key exists", "secret detail"} {
		require.NotContains(t, output.String(), secret)
		require.NotContains(t, recorder.Body.String(), secret)
	}
}

// TestInternalJWTAuthorizeWithoutHMACProviderKeepsJWTOnlyResponse 验证未实现 Provider 的旧服务仍保持 JWT-only 401 契约。
func TestInternalJWTAuthorizeWithoutHMACProviderKeepsJWTOnlyResponse(t *testing.T) {
	sc := authRequestServiceContext(nil)
	sc.Config.MaxBytes = 4
	request := hmacRequest(http.MethodPost, "/private/orders", "12345")
	recorder := httptest.NewRecorder()

	internalJWTAuthorize(sc, authRequestRouterInfo(types.PrivateType), sc.Config.Auth.AccessSecret, types.AuthTypeUser,
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("未实现 HMAC Provider 不得进入下游") }),
	).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Contains(t, recorder.Body.String(), "authentication failed")
}

type observedReader struct {
	reader io.Reader
	reads  atomic.Int32
}

type blockingRequestBody struct {
	started   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

func (b *blockingRequestBody) Read([]byte) (int, error) {
	b.startOnce.Do(func() { close(b.started) })
	<-b.closed
	return 0, errors.New("request body closed")
}

func (b *blockingRequestBody) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

func (r *observedReader) Read(buffer []byte) (int, error) {
	r.reads.Add(1)
	return r.reader.Read(buffer)
}

// TestInternalJWTAuthorizeRejectsBusyHMACBeforeReadingBody 验证并发名额饱和时在读取未认证 body 前拒绝。
func TestInternalJWTAuthorizeRejectsBusyHMACBeforeReadingBody(t *testing.T) {
	sc := authRequestServiceContext(nil)
	sc.Config.HMACAuth.MaxInFlight = 1
	release := make(chan struct{})
	entered := make(chan struct{})
	sc.HMACAuthProvider = hmacAuthProviderFunc(func(context.Context, types.HMACAuthArgs) (*types.HMACAuthResult, error) {
		close(entered)
		<-release
		return &types.HMACAuthResult{Identity: types.AuthIdentity{
			UID: "42", AuthType: types.AuthTypeUser, Provider: "apikey", ProviderSubject: "credential-1",
		}}, nil
	})
	handler := internalJWTAuthorize(sc, authRequestRouterInfo(types.PrivateType), sc.Config.Auth.AccessSecret, types.AuthTypeUser,
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	)
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		handler.ServeHTTP(httptest.NewRecorder(), hmacRequest(http.MethodPost, "/private/orders", "first"))
	}()
	<-entered

	body := &observedReader{reader: strings.NewReader("second")}
	request := httptest.NewRequest(http.MethodPost, "/private/orders", body)
	request.RemoteAddr = "198.51.100.10:4321"
	request.Header.Set("X-Access-Key", "payload-2")
	request.Header.Set("X-Timestamp", "1900000000000")
	request.Header.Set("X-Nonce", "nonce-2")
	request.Header.Set("X-Signature", "signature-2")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Zero(t, body.reads.Load())
	close(release)
	<-firstDone
}

// TestInternalJWTAuthorizeKeepsSlotUntilTimedOutProviderReturns 验证超时不会提前释放仍在执行的 Provider 名额。
func TestInternalJWTAuthorizeKeepsSlotUntilTimedOutProviderReturns(t *testing.T) {
	sc := authRequestServiceContext(nil)
	sc.Config.HMACAuth.MaxInFlight = 1
	sc.Config.Timeout = 20
	release := make(chan struct{})
	entered := make(chan struct{})
	sc.HMACAuthProvider = hmacAuthProviderFunc(func(context.Context, types.HMACAuthArgs) (*types.HMACAuthResult, error) {
		close(entered)
		<-release
		return nil, errors.New("provider stopped")
	})
	handler := internalJWTAuthorize(sc, authRequestRouterInfo(types.PrivateType), sc.Config.Auth.AccessSecret, types.AuthTypeUser,
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	)
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		handler.ServeHTTP(httptest.NewRecorder(), hmacRequest(http.MethodPost, "/private/orders", "first"))
	}()
	<-entered
	<-firstDone

	body := &observedReader{reader: strings.NewReader("second")}
	request := httptest.NewRequest(http.MethodPost, "/private/orders", body)
	request.RemoteAddr = "198.51.100.10:4321"
	request.Header.Set("X-Access-Key", "payload-2")
	request.Header.Set("X-Timestamp", "1900000000000")
	request.Header.Set("X-Nonce", "nonce-2")
	request.Header.Set("X-Signature", "signature-2")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Zero(t, body.reads.Load())
	close(release)
}

// TestInternalJWTAuthorizeBoundsHMACBodyReadTime 验证停发的未认证 body 会被 ctx 取消并 fail closed。
func TestInternalJWTAuthorizeBoundsHMACBodyReadTime(t *testing.T) {
	sc := authRequestServiceContext(nil)
	sc.Config.Timeout = 20
	var providerCalled atomic.Bool
	sc.HMACAuthProvider = hmacAuthProviderFunc(func(context.Context, types.HMACAuthArgs) (*types.HMACAuthResult, error) {
		providerCalled.Store(true)
		return nil, nil
	})
	body := &blockingRequestBody{started: make(chan struct{}), closed: make(chan struct{})}
	request := httptest.NewRequest(http.MethodPost, "/private/orders", nil)
	request.Body = body
	request.RemoteAddr = "198.51.100.10:4321"
	request.Header.Set("X-Access-Key", "payload")
	request.Header.Set("X-Timestamp", "1900000000000")
	request.Header.Set("X-Nonce", "nonce-1")
	request.Header.Set("X-Signature", "signature-1")
	recorder := httptest.NewRecorder()

	internalJWTAuthorize(sc, authRequestRouterInfo(types.PrivateType), sc.Config.Auth.AccessSecret, types.AuthTypeUser,
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("未读完请求体不得进入下游") }),
	).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.False(t, providerCalled.Load())
	select {
	case <-body.closed:
	case <-time.After(time.Second):
		t.Fatal("HMAC 请求体超时后必须关闭 body")
	}
}

type authRequestHookFunc func(context.Context, types.AuthRequestArgs) error

func (f authRequestHookFunc) OnAuthRequest(ctx context.Context, args types.AuthRequestArgs) error {
	return f(ctx, args)
}

type hmacAuthProviderFunc func(context.Context, types.HMACAuthArgs) (*types.HMACAuthResult, error)

func (f hmacAuthProviderFunc) AuthenticateHMAC(ctx context.Context, args types.HMACAuthArgs) (*types.HMACAuthResult, error) {
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

func hmacRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	request.RemoteAddr = "198.51.100.10:4321"
	request.Header.Set("X-Access-Key", "payload")
	request.Header.Set("X-Timestamp", "1900000000000")
	request.Header.Set("X-Nonce", "nonce-1")
	request.Header.Set("X-Signature", "signature-1")
	return request
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
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

func authenticatedRequestWithSecret(
	t *testing.T,
	secret string,
	identity types.AuthIdentity,
	key, value string,
) *http.Request {
	t.Helper()
	now := time.Now().UTC().Add(-time.Second)
	claims := safe.NewClaims(identity.UID, identity.Username)
	require.NoError(t, claims.ConfigureSecretData(secret, identity.AuthType))
	require.NoError(t, claims.AddSecretData(key, value))
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
