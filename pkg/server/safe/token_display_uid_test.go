// 本文件验证普通用户展示 UID 不改变认证身份、秘密 Claim 和旧 Token 语义。
package safe

import (
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

// TestTokenDisplayUIDPreservesVerifiedIdentity 验证短 ID 写入 JWT，认证与秘密 Claim 仍绑定原始 UUID。
func TestTokenDisplayUIDPreservesVerifiedIdentity(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	original := "6ee11693-c709-4c79-8660-c581237757ac"
	claims := NewClaims(original, "alice")
	setter := types.ITokenUIDMutator(claims)
	require.NoError(t, claims.ConfigureSecretData("access-secret", types.AuthTypeUser))
	require.NoError(t, claims.AddSecretData("private-key", "secret-value"))
	require.NoError(t, setter.SetTokenUID("1234567"))
	claims.AddData("platform_uid", "900000000000001")
	pair, err := IssueTokenPair(TokenIssueRequest{
		Claims: claims, Identity: types.AuthIdentity{UID: original, Username: "alice", AuthType: types.AuthTypeUser,
			Provider: types.AuthProviderCasdoor, ProviderSubject: "alice", Generation: 7},
		AuthType: types.AuthTypeUser, IssuedAt: now, AccessSecret: "access-secret", AccessExpireSeconds: 3600,
		RefreshSecret: "refresh-secret", RefreshExpireSeconds: 7200, IssueRefresh: true,
	})
	require.NoError(t, err)
	for _, raw := range []map[string]interface{}{
		parseTokenClaims(t, pair.AccessToken, "access-secret"), parseTokenClaims(t, pair.RefreshToken, "refresh-secret"),
	} {
		require.Equal(t, "1234567", raw["uid"])
		require.Equal(t, original, raw["auth_identity_uid"])
	}
	access, err := ValidateAccessToken(pair.AccessToken, "access-secret", types.AuthTypeUser, now)
	require.NoError(t, err)
	require.Equal(t, original, access.UID)
	require.Equal(t, original, access.Identity.UID)
	require.Equal(t, "900000000000001", access.Claims["platform_uid"])
	require.Equal(t, "secret-value", access.SecretClaims["private-key"])
	refresh, err := ValidateRefreshToken(pair.RefreshToken, "refresh-secret", types.AuthTypeUser, now)
	require.NoError(t, err)
	require.Equal(t, original, refresh.Identity.UID)
}

// TestTokenDisplayUIDRejectsInvalidAndReservedClaims 验证非法设置、跨域设置和伪造原始身份均不能签发。
func TestTokenDisplayUIDRejectsInvalidAndReservedClaims(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	issue := func(claims *Claims, authType types.AuthType) error {
		_, err := IssueTokenPair(TokenIssueRequest{Claims: claims, AuthType: authType, IssuedAt: now,
			AccessSecret: "access-secret", AccessExpireSeconds: 3600})
		return err
	}
	for _, invalid := range []string{"", " ", " 1234567", "1234567\n", string(make([]byte, 129))} {
		claims := NewClaims("original", "alice")
		require.Error(t, claims.SetTokenUID(invalid))
		require.Error(t, issue(claims, types.AuthTypeUser), "忽略设置错误不能放行签发")
	}
	for _, authType := range []types.AuthType{types.AuthTypeManage, types.AuthTypeServerManage} {
		claims := NewClaims("original", "alice")
		require.NoError(t, claims.SetTokenUID("1234567"))
		require.Error(t, issue(claims, authType))
	}
	claims := NewClaims("original", "alice")
	claims.AddData("auth_identity_uid", "forged-original")
	require.Error(t, issue(claims, types.AuthTypeUser))
	legacy := NewClaims("original", "alice")
	require.NoError(t, legacy.SetTokenUID("1234567"))
	_, err := legacy.GetToken("access-secret", 3600)
	require.Error(t, err, "旧签发入口不能静默丢弃展示 UID")
}

// TestTokenDisplayUIDRejectsMalformedIdentityClaim 验证已签名但结构不合法的新身份声明仍拒绝解析。
func TestTokenDisplayUIDRejectsMalformedIdentityClaim(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	for _, raw := range []interface{}{"", " ", 42, true, []string{"original"}} {
		claims := baseTokenClaims("1234567", "alice", types.AuthTypeUser, "access", now, 3600)
		claims["auth_identity_uid"] = raw
		token, err := signMapClaims(claims, "access-secret")
		require.NoError(t, err)
		_, err = ValidateAccessToken(token, "access-secret", types.AuthTypeUser, now)
		require.Error(t, err)
	}
}
