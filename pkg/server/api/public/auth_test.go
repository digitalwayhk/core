package public

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/require"
)

type authHookRecorder struct {
	calls    int
	captured *types.AuthHookArgs
	reject   error
}

func (h *authHookRecorder) OnAuth(_ context.Context, args *types.AuthHookArgs) error {
	h.calls++
	h.captured = args
	if h.reject != nil {
		return h.reject
	}
	args.Claims.AddData("shop_level", "gold")
	return nil
}

func TestIssueForServiceCallsHookBeforeSigning(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	hook := &authHookRecorder{}
	sc := authTestServiceContext(hook)
	extra := struct{ Provider string }{Provider: "casdoor"}

	pair, err := issueForServiceAt(context.Background(), sc, "user-1", "用户一", types.AuthTypeUser, types.AuthSourceCallback, extra, now)
	require.NoError(t, err)
	require.Equal(t, 1, hook.calls)
	require.Equal(t, "user-1", hook.captured.UID)
	require.Equal(t, "用户一", hook.captured.Username)
	require.Equal(t, types.AuthTypeUser, hook.captured.AuthType)
	require.Equal(t, types.AuthSourceCallback, hook.captured.Source)
	require.Equal(t, now, hook.captured.IssuedAt)
	require.Equal(t, int64(7200), hook.captured.AccessExpireSeconds)
	require.Equal(t, int64(30*24*60*60), hook.captured.RefreshExpireSeconds)
	require.Equal(t, now.Add(2*time.Hour), hook.captured.AccessExpiresAt)
	require.Equal(t, now.Add(30*24*time.Hour), hook.captured.RefreshExpiresAt)
	require.Equal(t, extra, hook.captured.Extra)
	require.NotNil(t, hook.captured.Claims)

	access := decodeAuthToken(t, pair.AccessToken, "auth-access")
	refresh := decodeAuthToken(t, pair.RefreshToken, "auth-refresh")
	require.Equal(t, "gold", access["shop_level"])
	require.NotContains(t, refresh, "shop_level")
}

func TestIssueForServiceRejectsEmptyUIDBeforeHook(t *testing.T) {
	hook := &authHookRecorder{}
	sc := authTestServiceContext(hook)

	pair, err := issueForServiceAt(context.Background(), sc, " ", "", types.AuthTypeUser, types.AuthSourceTestToken, nil, time.Now())
	require.Error(t, err)
	require.Zero(t, hook.calls)
	require.Empty(t, pair.AccessToken)
	require.Empty(t, pair.RefreshToken)
}

func TestIssueForServiceReturnsNoTokenWhenHookRejects(t *testing.T) {
	hook := &authHookRecorder{reject: errors.New("禁止登录")}
	sc := authTestServiceContext(hook)

	pair, err := issueForServiceAt(context.Background(), sc, "user-1", "", types.AuthTypeUser, types.AuthSourceCallback, nil, time.Now())
	require.ErrorIs(t, err, hook.reject)
	require.Empty(t, pair.AccessToken)
	require.Empty(t, pair.RefreshToken)
}

func TestIssueForServiceRejectsInvalidRefreshConfigBeforeHook(t *testing.T) {
	hook := &authHookRecorder{}
	sc := authTestServiceContext(hook)
	sc.Config.Auth.RefreshSecret = ""

	pair, err := issueForServiceAt(context.Background(), sc, "user-1", "", types.AuthTypeUser, types.AuthSourceCallback, nil, time.Now())
	require.Error(t, err)
	require.Zero(t, hook.calls)
	require.Empty(t, pair.AccessToken)
	require.Empty(t, pair.RefreshToken)
}

func TestRefreshAcceptsAuthAndManageSecretsIndependently(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	for _, authType := range []types.AuthType{types.AuthTypeUser, types.AuthTypeManage} {
		t.Run(string(authType), func(t *testing.T) {
			hook := &authHookRecorder{}
			sc := authTestServiceContext(hook)
			original, err := issueForServiceAt(context.Background(), sc, "user-1", "用户一", authType, types.AuthSourceTestToken, nil, now)
			require.NoError(t, err)

			refreshed, err := refreshForServiceAt(context.Background(), sc, original.RefreshToken, authType, now.Add(time.Hour))
			require.NoError(t, err)
			require.Equal(t, 2, hook.calls, "刷新必须重新执行 Hook")
			require.Equal(t, types.AuthSourceRefresh, hook.captured.Source)
			require.Empty(t, refreshed.RefreshToken, "本版刷新不旋转 Refresh Token")
			require.Zero(t, refreshed.RefreshExpiresIn)

			wrongType := types.AuthTypeManage
			if authType == types.AuthTypeManage {
				wrongType = types.AuthTypeUser
			}
			_, err = refreshForServiceAt(context.Background(), sc, original.RefreshToken, wrongType, now.Add(time.Hour))
			require.Error(t, err)
		})
	}
}

func authTestServiceContext(hook types.IAuthHookProvider) *router.ServiceContext {
	return &router.ServiceContext{
		Config: &config.ServerConfig{
			Auth: config.AuthSecret{
				AccessSecret:  "auth-access",
				AccessExpire:  7200,
				RefreshSecret: "auth-refresh",
				RefreshExpire: 30 * 24 * 60 * 60,
			},
			ManageAuth: config.AuthSecret{
				AccessSecret:  "manage-access",
				AccessExpire:  7200,
				RefreshSecret: "manage-refresh",
				RefreshExpire: 30 * 24 * 60 * 60,
			},
			ServerManageAuth: config.AuthSecret{
				AccessSecret: "server-access",
				AccessExpire: 86400,
			},
		},
		AuthHookProvider: hook,
	}
}

func decodeAuthToken(t *testing.T, tokenString, secret string) jwt.MapClaims {
	t.Helper()
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithoutClaimsValidation(),
	)
	token, err := parser.Parse(tokenString, func(*jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	require.NoError(t, err)
	require.True(t, token.Valid)
	claims, ok := token.Claims.(jwt.MapClaims)
	require.True(t, ok)
	return claims
}
