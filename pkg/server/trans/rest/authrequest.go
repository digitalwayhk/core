package rest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
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
	return authRequestHandlerWithAuthority(sc, sc, info, authType, next)
}

func authRequestHandlerWithAuthority(
	sc *router.ServiceContext,
	authAuthority *router.ServiceContext,
	info *types.RouterInfo,
	authType types.AuthType,
	next http.Handler,
) http.Handler {
	if authAuthority == nil {
		authAuthority = sc
	}
	if next == nil {
		next = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writePublicErrorContract(w, types.NewPublicError(types.ErrorKindUnavailable, 0, "", nil).PublicErrorContract())
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		manager, _, authorityActive := authAuthority.GetAuthRequestRuntime()
		_, hook, serviceActive := sc.GetAuthRequestRuntime()
		if !authorityActive || !serviceActive {
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
func internalJWTAuthorize(
	sc *router.ServiceContext,
	info *types.RouterInfo,
	secret string,
	authType types.AuthType,
	next http.Handler,
) http.Handler {
	if next == nil {
		next = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writePublicErrorContract(w, types.NewPublicError(types.ErrorKindUnavailable, 0, "", nil).PublicErrorContract())
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerAccessToken(r.Header.Get("Authorization"))
		if ok {
			verified, err := safe.ValidateAccessToken(token, secret, authType, time.Now().UTC())
			if err != nil {
				writeInternalJWTUnauthorized(w, authType)
				return
			}
			serveVerifiedAccess(w, r, verified, next)
			return
		}
		if authType != types.AuthTypeUser || sc == nil || info == nil {
			writeInternalJWTUnauthorized(w, authType)
			return
		}
		credentials, present := extractHMACHeaders(r, sc.Config)
		if !present {
			writeInternalJWTUnauthorized(w, authType)
			return
		}
		provider, active := sc.GetHMACAuthRuntime()
		if !active || provider == nil {
			writeInternalJWTUnauthorized(w, authType)
			return
		}
		result, err := sc.InvokePreparedHMACAuth(r.Context(), func(hookCtx context.Context) (types.HMACAuthArgs, error) {
			body, err := readHMACBody(hookCtx, r, hmacBodyLimit(sc))
			if err != nil {
				if !errors.Is(err, errHMACBodyTooLarge) {
					err = types.NewPublicError(types.ErrorKindInternal, 0, "", err)
				}
				return types.HMACAuthArgs{}, err
			}
			bodyHash := sha256.Sum256(body)
			credentials.Method = info.GetMethod()
			credentials.Path = info.GetPath()
			credentials.PathType = info.GetPathType()
			credentials.Query = r.URL.RawQuery
			credentials.BodyHashHex = hex.EncodeToString(bodyHash[:])
			credentials.ClientIP = utils.ClientPublicIP(r, sc.Config.TrustedProxies...)
			credentials.TraceID = ensureAuthRequestTraceID(r)
			return credentials, nil
		})
		if err != nil {
			if errors.Is(err, errHMACBodyTooLarge) {
				writePublicErrorContract(w, types.NewPublicError(types.ErrorKindPayloadTooLarge, 0, "", nil).PublicErrorContract())
				return
			}
			writeHMACAccessDenied(w, sc, info, authType, credentials.AccessKey, err)
			return
		}
		maxLifetime := time.Duration(sc.Config.Auth.AccessExpire) * time.Second
		if maxLifetime <= 0 {
			maxLifetime = time.Duration(config.DefaultAccessExpireSeconds) * time.Second
		}
		verified, err := safe.BuildHMACAccessIdentity(result, credentials.AccessKey, authType, time.Now().UTC(), maxLifetime)
		if err != nil {
			writeHMACAccessDenied(w, sc, info, authType, credentials.AccessKey, err)
			return
		}
		serveVerifiedAccess(w, r, verified, next)
	})
}

func serveVerifiedAccess(w http.ResponseWriter, r *http.Request, verified *safe.AccessTokenIdentity, next http.Handler) {
	ctx := r.Context()
	for key, value := range verified.Claims {
		ctx = context.WithValue(ctx, key, value)
	}
	ctx = context.WithValue(ctx, verifiedAccessContextKey{}, verifiedAccessContext{
		identity: verified.Identity,
		claims:   types.CloneAuthClaims(verified.Claims),
	})
	if len(verified.SecretClaims) > 0 {
		ctx = safe.WithVerifiedSecretClaims(ctx, verified.SecretClaims)
	}
	next.ServeHTTP(w, r.WithContext(ctx))
}

func extractHMACHeaders(r *http.Request, cfg *config.ServerConfig) (types.HMACAuthArgs, bool) {
	if r == nil || cfg == nil {
		return types.HMACAuthArgs{}, false
	}
	headers := cfg.HMACAuth
	args := types.HMACAuthArgs{
		AccessKey:  strings.TrimSpace(r.Header.Get(headers.AccessKeyHeader)),
		Timestamp:  strings.TrimSpace(r.Header.Get(headers.TimestampHeader)),
		Nonce:      strings.TrimSpace(r.Header.Get(headers.NonceHeader)),
		Signature:  strings.TrimSpace(r.Header.Get(headers.SignatureHeader)),
		RecvWindow: strings.TrimSpace(r.Header.Get(headers.RecvWindowHeader)),
	}
	return args, args.AccessKey != "" && args.Timestamp != "" && args.Nonce != "" && args.Signature != ""
}

var errHMACBodyTooLarge = errors.New("HMAC request body too large")

func hmacBodyLimit(sc *router.ServiceContext) int64 {
	const defaultLimit int64 = 1 << 20
	if sc == nil || sc.Config == nil || sc.Config.MaxBytes <= 0 {
		return defaultLimit
	}
	return sc.Config.MaxBytes
}

func readHMACBody(ctx context.Context, r *http.Request, limit int64) ([]byte, error) {
	if r == nil || r.Body == nil {
		return []byte{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	bodyStream := r.Body
	stopClose := context.AfterFunc(ctx, func() {
		_ = bodyStream.Close()
	})
	defer stopClose()
	body, err := io.ReadAll(io.LimitReader(bodyStream, limit+1))
	if closeErr := bodyStream.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errHMACBodyTooLarge
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func writeHMACAccessDenied(
	w http.ResponseWriter,
	sc *router.ServiceContext,
	info *types.RouterInfo,
	authType types.AuthType,
	accessKey string,
	err error,
) {
	contract := hmacPublicErrorContract(err)
	serviceName := ""
	path := ""
	if sc != nil && sc.Service != nil {
		serviceName = sc.Service.Name
	}
	if info != nil {
		path = info.GetPath()
	}
	identityHash := authRequestIdentityHash(serviceName, authType, types.AuthIdentity{
		Provider: "hmac", ProviderSubject: accessKey,
	})
	logx.Infow("hmac_access_denied",
		logx.Field("service", serviceName), logx.Field("route", path),
		logx.Field("auth_type", authType), logx.Field("identity_hash", identityHash),
		logx.Field("code", contract.Code),
	)
	writePublicErrorContract(w, contract)
}

func hmacPublicErrorContract(err error) types.PublicErrorContract {
	type contractProvider interface {
		PublicErrorContract() types.PublicErrorContract
	}
	var provider contractProvider
	if errors.As(err, &provider) {
		switch provider.PublicErrorContract().Kind {
		case types.ErrorKindUnavailable:
			return types.NewPublicError(types.ErrorKindUnavailable, 0, "", nil).PublicErrorContract()
		case types.ErrorKindRateLimited:
			return types.NewPublicError(types.ErrorKindRateLimited, 0, "", nil).PublicErrorContract()
		case types.ErrorKindInternal:
			return types.NewPublicError(types.ErrorKindInternal, 0, "", nil).PublicErrorContract()
		}
	}
	return types.NewPublicError(types.ErrorKindUnauthenticated, types.PublicCodeUnauthenticated, "authentication failed", nil).PublicErrorContract()
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
