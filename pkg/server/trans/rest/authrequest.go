package rest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/safe"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/gofrs/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel/trace"
)

type verifiedAccessContextKey struct{}

type verifiedAccessContext struct {
	identity types.AuthIdentity
	claims   map[string]interface{}
}

// authRequestHandler 在认证中间件已经验证签名后，执行框架用途隔离、撤销校验和业务授权 Hook。
func authRequestHandler(
	sc *router.ServiceContext,
	info *types.RouterInfo,
	authType types.AuthType,
	next http.Handler,
) http.Handler {
	if next == nil {
		next = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writePublicErrorContract(w, types.NewPublicError(types.ErrorKindUnavailable, 0, "", nil).PublicErrorContract())
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		manager, hook, active := sc.GetAuthRequestRuntime()
		if !active {
			contract := types.ResolvePublicError(requestAuthenticationError(errors.New("service authentication is closing")))
			logAuthRequestDenied(sc, info, authType, types.AuthIdentity{}, contract)
			writePublicErrorContract(w, contract)
			return
		}
		identity, claims, err := verifiedRequestIdentity(r, sc, authType)
		if err == nil && identity.Provider == types.AuthProviderCasdoor {
			if manager == nil {
				err = requestAuthenticationError(errors.New("revocation authority unavailable"))
			} else if authorizeErr := manager.Authorize(r.Context(), identity); authorizeErr != nil {
				err = requestAuthenticationError(authorizeErr)
			}
		}
		if err == nil && hook != nil {
			args := buildAuthRequestArgs(r, sc, info, identity, claims)
			err = invokeAuthRequestHook(r.Context(), sc, hook, args)
		}
		if err != nil {
			contract := types.ResolvePublicError(err)
			logAuthRequestDenied(sc, info, authType, identity, contract)
			writePublicErrorContract(w, contract)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func verifiedRequestIdentity(
	r *http.Request,
	sc *router.ServiceContext,
	authType types.AuthType,
) (types.AuthIdentity, map[string]interface{}, error) {
	if r == nil || sc == nil || sc.Config == nil {
		return types.AuthIdentity{}, nil, requestAuthenticationError(errors.New("authentication context unavailable"))
	}
	verified, ok := r.Context().Value(verifiedAccessContextKey{}).(verifiedAccessContext)
	if !ok {
		return types.AuthIdentity{}, nil, requestAuthenticationError(errors.New("verified access identity missing"))
	}
	if verified.identity.AuthType != authType || strings.TrimSpace(verified.identity.UID) == "" {
		return types.AuthIdentity{}, nil, requestAuthenticationError(errors.New("verified access identity invalid"))
	}
	return verified.identity, types.CloneAuthClaims(verified.claims), nil
}

func bearerAccessToken(header string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	return token, token != ""
}

// internalJWTAuthorize 验证框架签发的 Access Token，并把已验证 Claims 注入请求上下文。
// 不使用 go-zero 默认 Authorize 的失败日志，因为它会转储包含 Authorization 的完整请求。
func internalJWTAuthorize(secret string, authType types.AuthType, next http.Handler) http.Handler {
	if next == nil {
		next = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writePublicErrorContract(w, types.NewPublicError(types.ErrorKindUnavailable, 0, "", nil).PublicErrorContract())
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerAccessToken(r.Header.Get("Authorization"))
		if !ok {
			writeInternalJWTUnauthorized(w, authType)
			return
		}
		verified, err := safe.ValidateAccessToken(token, secret, authType, time.Now().UTC())
		if err != nil {
			writeInternalJWTUnauthorized(w, authType)
			return
		}
		ctx := r.Context()
		for key, value := range verified.Claims {
			ctx = context.WithValue(ctx, key, value)
		}
		ctx = context.WithValue(ctx, verifiedAccessContextKey{}, verifiedAccessContext{
			identity: verified.Identity,
			claims:   types.CloneAuthClaims(verified.Claims),
		})
		ctx = safe.WithVerifiedSecretClaims(ctx, verified.SecretClaims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeInternalJWTUnauthorized(w http.ResponseWriter, authType types.AuthType) {
	logx.Infow("jwt_access_denied", logx.Field("auth_type", authType))
	writePublicErrorContract(w, types.NewPublicError(
		types.ErrorKindUnauthenticated, types.PublicCodeUnauthenticated, "authentication failed", nil,
	).PublicErrorContract())
}

func buildAuthRequestArgs(
	r *http.Request,
	sc *router.ServiceContext,
	info *types.RouterInfo,
	identity types.AuthIdentity,
	claims map[string]interface{},
) types.AuthRequestArgs {
	serviceName := ""
	if sc != nil {
		if sc.Service != nil {
			serviceName = sc.Service.Name
		}
		if serviceName == "" && sc.Config != nil {
			serviceName = sc.Config.Name
		}
	}
	args := types.AuthRequestArgs{
		Identity: identity, ServiceName: serviceName, Claims: types.CloneAuthClaims(claims),
	}
	if r != nil {
		args.SecretClaims = safe.VerifiedSecretClaimsFromContext(r.Context())
	}
	if info != nil {
		args.Path = info.GetPath()
		args.Method = info.GetMethod()
		args.PathType = info.GetPathType()
	}
	if r != nil {
		args.TraceID = ensureAuthRequestTraceID(r)
		if sc != nil && sc.Config != nil {
			args.ClientIP = utils.ClientPublicIP(r, sc.Config.TrustedProxies...)
		}
	}
	return args
}

func ensureAuthRequestTraceID(r *http.Request) string {
	if r == nil {
		return ""
	}
	if traceID := strings.TrimSpace(r.Header.Get("X-Trace-Id")); traceID != "" {
		return traceID
	}
	if spanContext := trace.SpanContextFromContext(r.Context()); spanContext.HasTraceID() {
		traceID := spanContext.TraceID().String()
		r.Header.Set("X-Trace-Id", traceID)
		return traceID
	}
	generated, err := uuid.NewV4()
	if err != nil {
		return ""
	}
	traceID := generated.String()
	r.Header.Set("X-Trace-Id", traceID)
	return traceID
}

func invokeAuthRequestHook(
	ctx context.Context,
	sc *router.ServiceContext,
	hook types.IAuthRequestHookProvider,
	args types.AuthRequestArgs,
) error {
	timeout := 3 * time.Second
	if sc != nil && sc.Config != nil && sc.Config.Timeout > 0 {
		timeout = time.Duration(sc.Config.Timeout) * time.Millisecond
	}
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		defer func() {
			if recover() != nil {
				result <- types.NewPublicError(types.ErrorKindInternal, 0, "", errors.New("auth request hook panic"))
			}
		}()
		result <- hook.OnAuthRequest(hookCtx, args)
	}()
	select {
	case err := <-result:
		return err
	case <-hookCtx.Done():
		return types.NewPublicError(types.ErrorKindInternal, 0, "", errors.New("auth request hook timeout"))
	}
}

func requestAuthenticationError(cause error) error {
	return types.NewPublicError(types.ErrorKindUnauthenticated, types.PublicCodeUnauthenticated, "authentication failed", cause)
}

func logAuthRequestDenied(
	sc *router.ServiceContext,
	info *types.RouterInfo,
	authType types.AuthType,
	identity types.AuthIdentity,
	contract types.PublicErrorContract,
) {
	serviceName := ""
	path := ""
	if sc != nil && sc.Service != nil {
		serviceName = sc.Service.Name
	}
	if info != nil {
		path = info.GetPath()
	}
	identityHash := authRequestIdentityHash(serviceName, authType, identity)
	if identityHash != "" {
		logx.Infow("auth_request_denied",
			logx.Field("service", serviceName),
			logx.Field("route", path),
			logx.Field("auth_type", authType),
			logx.Field("identity_hash", identityHash),
			logx.Field("code", contract.Code),
		)
		return
	}
	logx.Infow("auth_request_denied",
		logx.Field("service", serviceName),
		logx.Field("route", path),
		logx.Field("auth_type", authType),
		logx.Field("code", contract.Code),
	)
}

func authRequestIdentityHash(serviceName string, authType types.AuthType, identity types.AuthIdentity) string {
	subject := strings.TrimSpace(identity.ProviderSubject)
	if subject == "" {
		subject = strings.TrimSpace(identity.UID)
	}
	if subject == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		serviceName, string(authType), identity.Provider, subject,
	}, "|")))
	return hex.EncodeToString(sum[:8])
}
