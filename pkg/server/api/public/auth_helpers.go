package public

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/safe"
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
	auth, err := authSecretForType(sc, authType)
	if err != nil {
		return safe.TokenPairResponse{}, err
	}
	issueRefresh := authType != types.AuthTypeServerManage
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
		uid,
		username,
		authType,
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
	return issueWithSchedule(
		ctx,
		sc,
		auth,
		identity.UID,
		identity.Username,
		authType,
		types.AuthSourceRefresh,
		nil,
		now,
		remaining,
		identity.ExpiresAt,
		false,
	)
}

func issueWithSchedule(
	ctx context.Context,
	sc *router.ServiceContext,
	auth *config.AuthSecret,
	uid string,
	username string,
	authType types.AuthType,
	source types.AuthSource,
	extra interface{},
	issuedAt time.Time,
	refreshExpireSeconds int64,
	refreshExpiresAt time.Time,
	issueRefresh bool,
) (safe.TokenPairResponse, error) {
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
	}
	if sc != nil && sc.AuthHookProvider != nil {
		if err := sc.AuthHookProvider.OnAuth(ctx, args); err != nil {
			return safe.TokenPairResponse{}, err
		}
	}

	return safe.IssueTokenPair(safe.TokenIssueRequest{
		Claims:               claims,
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
