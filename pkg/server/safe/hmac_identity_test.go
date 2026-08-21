// Package safe 的本测试文件验证 HMAC 身份构造的保留键、凭证标识和有界过期契约。
package safe

import (
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

// TestBuildHMACAccessIdentityBuildsTrustedClaimsAndCapsExpiry 验证 Auth 用户身份的标准 Claims 与最长寿命上限。
func TestBuildHMACAccessIdentityBuildsTrustedClaimsAndCapsExpiry(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	result := &types.HMACAuthResult{
		Identity: types.AuthIdentity{
			UID: "42", Username: "alice", AuthType: types.AuthTypeUser,
			Provider: "apikey", ProviderSubject: "credential-7",
			ExpiresAt: now.Add(4 * time.Hour),
		},
		Claims: map[string]string{"platform_uid": "42"},
	}

	identity, err := BuildHMACAccessIdentity(result, "public-access-key", types.AuthTypeUser, now, 2*time.Hour)

	require.NoError(t, err)
	require.Equal(t, now, identity.IssuedAt)
	require.Equal(t, now.Add(2*time.Hour), identity.ExpiresAt)
	require.Equal(t, "42", identity.Claims["uid"])
	require.Equal(t, "access", identity.Claims["token_use"])
	require.Equal(t, "42", identity.Claims["platform_uid"])
}

// TestBuildHMACAccessIdentityRejectsRawAccessKeyAsProviderSubject 验证原始 AccessKey 不得作为稳定凭证标识。
func TestBuildHMACAccessIdentityRejectsRawAccessKeyAsProviderSubject(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	result := &types.HMACAuthResult{Identity: types.AuthIdentity{
		UID: "42", AuthType: types.AuthTypeUser, Provider: "apikey", ProviderSubject: "raw-key",
	}}

	_, err := BuildHMACAccessIdentity(result, "raw-key", types.AuthTypeUser, now, time.Hour)

	require.Error(t, err)
}

// TestBuildHMACAccessIdentityRejectsReservedClaimsAndCasdoor 验证 HMAC 身份不得伪装 Casdoor 或覆盖保留 Claims。
func TestBuildHMACAccessIdentityRejectsReservedClaimsAndCasdoor(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	for _, result := range []*types.HMACAuthResult{
		{Identity: types.AuthIdentity{UID: "42", AuthType: types.AuthTypeUser, Provider: "apikey", ProviderSubject: "credential-1"}, Claims: map[string]string{"uid": "other"}},
		{Identity: types.AuthIdentity{UID: "42", AuthType: types.AuthTypeUser, Provider: types.AuthProviderCasdoor, ProviderSubject: "credential-1"}},
	} {
		_, err := BuildHMACAccessIdentity(result, "raw-key", types.AuthTypeUser, now, time.Hour)
		require.Error(t, err)
	}
}

// TestValidateHMACAuthClaimsPublishesAllReservedKeys 验证消费方可用公开 helper 预检查完整的保留键集合。
func TestValidateHMACAuthClaimsPublishesAllReservedKeys(t *testing.T) {
	for _, key := range []string{
		"uid", "uname", "auth_type", "token_use", "iat", "exp", "auth_provider",
		"provider_subject", "auth_generation", "auth_authority_service", "args", "secret_args",
	} {
		t.Run(key, func(t *testing.T) {
			require.Error(t, ValidateHMACAuthClaims(map[string]string{key: "forbidden"}))
		})
	}
	require.NoError(t, ValidateHMACAuthClaims(map[string]string{"platform_uid": "42"}))
}
