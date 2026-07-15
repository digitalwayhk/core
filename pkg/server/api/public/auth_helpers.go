package public

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/server/authstate"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/safe"
	casdoorauth "github.com/digitalwayhk/core/pkg/server/safe/casdoor"
	"github.com/digitalwayhk/core/pkg/server/types"
)

func issueForService(
	ctx context.Context,
	sc *router.ServiceContext,
	uid string,
	username string,
	authType types.AuthType,
	source types.AuthSource,
	extra interface{},
) (safe.TokenPairResponse, error) {
	return issueForServiceAt(ctx, sc, uid, username, authType, source, extra, time.Now().UTC())
}

func issueForServiceAt(
	ctx context.Context,
	sc *router.ServiceContext,
	uid string,
	username string,
	authType types.AuthType,
	source types.AuthSource,
	extra interface{},
	issuedAt time.Time,
) (safe.TokenPairResponse, error) {
	return issueForServiceIdentityAt(ctx, sc, types.AuthIdentity{
		UID:      uid,
		Username: username,
		AuthType: authType,
	}, source, extra, issuedAt)
}

func issueForServiceIdentityAt(
	ctx context.Context,
	sc *router.ServiceContext,
	identity types.AuthIdentity,
	source types.AuthSource,
	extra interface{},
	issuedAt time.Time,
) (safe.TokenPairResponse, error) {
	auth, err := authSecretForType(sc, identity.AuthType)
	if err != nil {
		return safe.TokenPairResponse{}, err
	}
	issueRefresh := identity.AuthType != types.AuthTypeServerManage
	refreshExpiresAt := time.Time{}
	refreshExpireSeconds := int64(0)
	if issueRefresh {
		refreshExpireSeconds = auth.RefreshExpire
		refreshExpiresAt = issuedAt.Add(time.Duration(refreshExpireSeconds) * time.Second)
	}
	return issueWithSchedule(
		ctx,
		sc,
		auth,
		identity,
		source,
		extra,
		issuedAt,
		refreshExpireSeconds,
		refreshExpiresAt,
		issueRefresh,
	)
}

func refreshForServiceAt(
	ctx context.Context,
	sc *router.ServiceContext,
	token string,
	authType types.AuthType,
	now time.Time,
) (safe.TokenPairResponse, error) {
	var client casdoorCallbackClient
	var authority authIdentityAuthority
	if sc != nil && sc.CasdoorClients != nil {
		client, _ = casdoorClientForType(sc, authType)
	}
	if sc != nil {
		authority = sc.AuthRevocationManager
	}
	return refreshForServiceWithDependenciesAt(ctx, sc, token, authType, now, client, authority)
}

func refreshForServiceWithDependenciesAt(
	ctx context.Context,
	sc *router.ServiceContext,
	token string,
	authType types.AuthType,
	now time.Time,
	client casdoorCallbackClient,
	authority authIdentityAuthority,
) (safe.TokenPairResponse, error) {
	auth, err := authSecretForType(sc, authType)
	if err != nil {
		return safe.TokenPairResponse{}, err
	}
	identity, err := safe.ValidateRefreshToken(token, auth.RefreshSecret, authType, now)
	if err != nil {
		return safe.TokenPairResponse{}, err
	}
	remaining := int64(identity.ExpiresAt.Sub(now).Seconds())
	if remaining <= 0 {
		return safe.TokenPairResponse{}, errors.New("Refresh Token 已过期")
	}
	if identity.Identity.Provider == types.AuthProviderCasdoor {
		if err := verifyCasdoorRefreshIdentity(ctx, identity.Identity, client, authority); err != nil {
			return safe.TokenPairResponse{}, authBoundaryError(err)
		}
	}
	return issueWithSchedule(
		ctx,
		sc,
		auth,
		identity.Identity,
		types.AuthSourceRefresh,
		nil,
		now,
		remaining,
		identity.ExpiresAt,
		false,
	)
}

func verifyCasdoorRefreshIdentity(
	ctx context.Context,
	identity types.AuthIdentity,
	client casdoorCallbackClient,
	authority authIdentityAuthority,
) error {
	if client == nil || authority == nil || strings.TrimSpace(identity.ProviderSubject) == "" {
		return authstate.ErrAuthorityUnavailable
	}
	current, err := authority.Current(ctx, identity)
	if err != nil || current.Blocked || current.Generation != identity.Generation {
		return authstate.ErrIdentityRevoked
	}
	user, err := client.GetUser(identity.ProviderSubject)
	if err != nil {
		return err
	}
	if err := casdoorauth.VerifyActiveUser(user, client.Organization(), identity.ProviderSubject); err != nil {
		return err
	}
	if strings.TrimSpace(user.Id) == "" || strings.TrimSpace(user.Id) != identity.UID {
		return casdoorauth.ErrIdentityInactive
	}
	confirmed, err := authority.ConfirmActive(ctx, identity, identity.Generation)
	if err != nil || confirmed.Blocked || confirmed.Generation != identity.Generation {
		return authstate.ErrIdentityRevoked
	}
	return nil
}

func issueWithSchedule(
	ctx context.Context,
	sc *router.ServiceContext,
	auth *config.AuthSecret,
	identity types.AuthIdentity,
	source types.AuthSource,
	extra interface{},
	issuedAt time.Time,
	refreshExpireSeconds int64,
	refreshExpiresAt time.Time,
	issueRefresh bool,
) (safe.TokenPairResponse, error) {
	uid := identity.UID
	username := identity.Username
	authType := identity.AuthType
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return safe.TokenPairResponse{}, errors.New("颁发 Token 时 UID 不能为空")
	}
	if auth == nil || auth.AccessSecret == "" || auth.AccessExpire <= 0 {
		return safe.TokenPairResponse{}, errors.New("Access Token 配置无效")
	}
	if issueRefresh && (auth.RefreshSecret == "" || auth.RefreshExpire <= 0 || auth.RefreshSecret == auth.AccessSecret) {
		return safe.TokenPairResponse{}, errors.New("Refresh Token 配置无效")
	}
	if issuedAt.IsZero() {
		return safe.TokenPairResponse{}, errors.New("颁发 Token 时 IssuedAt 不能为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	claims := safe.NewClaims(uid, username)
	identity.UID = uid
	identity.Username = username
	identity.AuthType = authType
	identity.IssuedAt = issuedAt
	identity.ExpiresAt = issuedAt.Add(time.Duration(auth.AccessExpire) * time.Second)
	args := &types.AuthHookArgs{
		UID:                  uid,
		Username:             username,
		AuthType:             authType,
		Source:               source,
		IssuedAt:             issuedAt,
		AccessExpireSeconds:  auth.AccessExpire,
		RefreshExpireSeconds: refreshExpireSeconds,
		AccessExpiresAt:      issuedAt.Add(time.Duration(auth.AccessExpire) * time.Second),
		RefreshExpiresAt:     refreshExpiresAt,
		Extra:                extra,
		Claims:               claims,
		Identity:             identity,
	}
	if sc != nil && sc.AuthHookProvider != nil {
		if err := sc.AuthHookProvider.OnAuth(ctx, args); err != nil {
			return safe.TokenPairResponse{}, err
		}
	}

	return safe.IssueTokenPair(safe.TokenIssueRequest{
		Claims:               claims,
		Identity:             identity,
		AuthType:             authType,
		IssuedAt:             issuedAt,
		AccessSecret:         auth.AccessSecret,
		AccessExpireSeconds:  auth.AccessExpire,
		RefreshSecret:        auth.RefreshSecret,
		RefreshExpireSeconds: auth.RefreshExpire,
		IssueRefresh:         issueRefresh,
	})
}

func authSecretForType(sc *router.ServiceContext, authType types.AuthType) (*config.AuthSecret, error) {
	if sc == nil || sc.Config == nil {
		return nil, errors.New("服务认证配置不存在")
	}
	switch authType {
	case types.AuthTypeUser:
		return &sc.Config.Auth, nil
	case types.AuthTypeManage:
		return &sc.Config.ManageAuth, nil
	case types.AuthTypeServerManage:
		return &sc.Config.ServerManageAuth, nil
	default:
		return nil, errors.New("认证类型无效")
	}
}

func requestContext(req types.IRequest) context.Context {
	httpRequest, ok := req.(types.IRequestHttp)
	if !ok || httpRequest.GetHttpRequest() == nil {
		return context.Background()
	}
	return httpRequest.GetHttpRequest().Context()
}
