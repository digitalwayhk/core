package safe

import (
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/require"
)

func TestIssueTokenPairSeparatesAccessAndRefreshClaims(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	claims := NewClaims("user-1", "用户一")
	claims.AddData("shop_level", "gold")

	pair, err := IssueTokenPair(TokenIssueRequest{
		Claims:               claims,
		AuthType:             types.AuthTypeUser,
		IssuedAt:             now,
		AccessSecret:         "access-secret-for-test",
		AccessExpireSeconds:  7200,
		RefreshSecret:        "refresh-secret-for-test",
		RefreshExpireSeconds: 2592000,
		IssueRefresh:         true,
	})
	require.NoError(t, err)
	require.Equal(t, "Bearer", pair.TokenType)
	require.Equal(t, int64(7200), pair.AccessExpiresIn)
	require.Equal(t, int64(2592000), pair.RefreshExpiresIn)

	access := parseTokenClaims(t, pair.AccessToken, "access-secret-for-test")
	refresh := parseTokenClaims(t, pair.RefreshToken, "refresh-secret-for-test")
	require.Equal(t, "gold", access["shop_level"])
	require.NotContains(t, refresh, "shop_level")
	require.Equal(t, "access", access["token_use"])
	require.Equal(t, "refresh", refresh["token_use"])
	require.Equal(t, "auth", access["auth_type"])
	require.Equal(t, "auth", refresh["auth_type"])
	require.EqualValues(t, now.Unix(), access["iat"])
	require.EqualValues(t, now.Add(7200*time.Second).Unix(), access["exp"])
	require.EqualValues(t, now.Unix(), refresh["iat"])
	require.EqualValues(t, now.Add(2592000*time.Second).Unix(), refresh["exp"])
}

func TestIssueTokenPairCarriesCasdoorIdentityInBothTokens(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	identity := types.AuthIdentity{
		UID:             "user-1",
		Username:        "用户一",
		AuthType:        types.AuthTypeUser,
		Provider:        types.AuthProviderCasdoor,
		ProviderSubject: "alice",
		Generation:      7,
		IssuedAt:        now,
		ExpiresAt:       now.Add(2 * time.Hour),
	}
	pair, err := IssueTokenPair(TokenIssueRequest{
		Claims:               NewClaims(identity.UID, identity.Username),
		Identity:             identity,
		AuthType:             identity.AuthType,
		IssuedAt:             now,
		AccessSecret:         "access-secret",
		AccessExpireSeconds:  7200,
		RefreshSecret:        "refresh-secret",
		RefreshExpireSeconds: 86400,
		IssueRefresh:         true,
	})
	require.NoError(t, err)

	access, err := ValidateAccessToken(pair.AccessToken, "access-secret", types.AuthTypeUser, now.Add(time.Minute))
	require.NoError(t, err)
	refresh, err := ValidateRefreshToken(pair.RefreshToken, "refresh-secret", types.AuthTypeUser, now.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, identity.Provider, access.Identity.Provider)
	require.Equal(t, identity.ProviderSubject, access.Identity.ProviderSubject)
	require.Equal(t, identity.Generation, access.Identity.Generation)
	require.Equal(t, identity.Provider, refresh.Identity.Provider)
	require.Equal(t, identity.ProviderSubject, refresh.Identity.ProviderSubject)
	require.Equal(t, identity.Generation, refresh.Identity.Generation)
}

func TestCasdoorTokenPreservesUint64Generation(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	generation := uint64(1<<53 + 17)
	pair, err := IssueTokenPair(TokenIssueRequest{
		Claims:              NewClaims("user-1", "用户一"),
		Identity:            types.AuthIdentity{UID: "user-1", Username: "用户一", AuthType: types.AuthTypeUser, Provider: types.AuthProviderCasdoor, ProviderSubject: "alice", Generation: generation},
		AuthType:            types.AuthTypeUser,
		IssuedAt:            now,
		AccessSecret:        "access-secret",
		AccessExpireSeconds: 7200,
	})
	require.NoError(t, err)

	access, err := ValidateAccessToken(pair.AccessToken, "access-secret", types.AuthTypeUser, now.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, generation, access.Identity.Generation)
}

func TestCasdoorTokenWithoutSubjectOrGenerationFailsClosed(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	for _, missing := range []string{"provider_subject", "auth_generation"} {
		t.Run(missing, func(t *testing.T) {
			claims := jwt.MapClaims{
				"uid": "user-1", "auth_type": "auth", "token_use": "access",
				"auth_provider": "casdoor", "provider_subject": "alice", "auth_generation": 1,
				"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
			}
			delete(claims, missing)
			token := signTokenClaims(t, "access-secret", claims)
			_, err := ValidateAccessToken(token, "access-secret", types.AuthTypeUser, now)
			require.ErrorContains(t, err, "Claims")
		})
	}
}

func TestIssueTokenPairRejectsMismatchedIdentity(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	_, err := IssueTokenPair(TokenIssueRequest{
		Claims:              NewClaims("user-1", "用户一"),
		Identity:            types.AuthIdentity{UID: "other", AuthType: types.AuthTypeUser, Provider: types.AuthProviderCasdoor, ProviderSubject: "alice"},
		AuthType:            types.AuthTypeUser,
		IssuedAt:            now,
		AccessSecret:        "access-secret",
		AccessExpireSeconds: 7200,
	})
	require.ErrorContains(t, err, "Identity")
}

func TestIssueTokenPairRejectsHookOverrideOfReservedClaims(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	claims := NewClaims("user-1", "用户一")
	claims.AddData("auth_type", "manage")

	_, err := IssueTokenPair(TokenIssueRequest{
		Claims:              claims,
		AuthType:            types.AuthTypeUser,
		IssuedAt:            now,
		AccessSecret:        "access-secret",
		AccessExpireSeconds: 7200,
	})

	require.ErrorContains(t, err, "保留Claim")
}

func TestValidateRefreshTokenRejectsAccessToken(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	token := signTokenClaims(t, "refresh-secret", jwt.MapClaims{
		"uid":       "user-1",
		"auth_type": "auth",
		"token_use": "access",
		"iat":       now.Unix(),
		"exp":       now.Add(time.Hour).Unix(),
	})

	_, err := ValidateRefreshToken(token, "refresh-secret", types.AuthTypeUser, now)
	require.Error(t, err)
}

func TestValidateAccessTokenUsesExplicitAuthType(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	token := signTokenClaims(t, "access-secret", jwt.MapClaims{
		"uid":       "manager-1",
		"uname":     "管理员",
		"auth_type": "manage",
		"token_use": "access",
		"iat":       now.Add(-time.Minute).Unix(),
		"exp":       now.Add(time.Hour).Unix(),
	})

	verified, err := ValidateAccessToken(token, "access-secret", types.AuthTypeManage, now)
	require.NoError(t, err)
	require.Equal(t, "manager-1", verified.UID)
	require.Equal(t, "管理员", verified.Username)
	require.Equal(t, types.AuthTypeManage, verified.AuthType)
}

func TestValidateAccessTokenRejectsWrongAuthType(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	token := signTokenClaims(t, "access-secret", jwt.MapClaims{
		"uid":       "manager-1",
		"auth_type": "manage",
		"token_use": "access",
		"iat":       now.Add(-time.Minute).Unix(),
		"exp":       now.Add(time.Hour).Unix(),
	})

	_, err := ValidateAccessToken(token, "access-secret", types.AuthTypeUser, now)
	require.Error(t, err)
}

func TestValidateRefreshTokenRejectsWrongAuthType(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	token := signTokenClaims(t, "refresh-secret", jwt.MapClaims{
		"uid":       "user-1",
		"auth_type": "auth",
		"token_use": "refresh",
		"iat":       now.Unix(),
		"exp":       now.Add(time.Hour).Unix(),
	})

	_, err := ValidateRefreshToken(token, "refresh-secret", types.AuthTypeManage, now)
	require.Error(t, err)
}

func TestValidateRefreshTokenRejectsWrongSecret(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	token := signTokenClaims(t, "refresh-secret", jwt.MapClaims{
		"uid":       "user-1",
		"auth_type": "auth",
		"token_use": "refresh",
		"iat":       now.Unix(),
		"exp":       now.Add(time.Hour).Unix(),
	})

	_, err := ValidateRefreshToken(token, "wrong-secret", types.AuthTypeUser, now)
	require.Error(t, err)
}

func TestValidateRefreshTokenReturnsVerifiedIdentity(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	token := signTokenClaims(t, "refresh-secret", jwt.MapClaims{
		"uid":       "user-1",
		"uname":     "用户一",
		"auth_type": "manage",
		"token_use": "refresh",
		"iat":       now.Add(-time.Minute).Unix(),
		"exp":       now.Add(time.Hour).Unix(),
	})

	verified, err := ValidateRefreshToken(token, "refresh-secret", types.AuthTypeManage, now)
	require.NoError(t, err)
	require.Equal(t, "user-1", verified.UID)
	require.Equal(t, "用户一", verified.Username)
	require.Equal(t, types.AuthTypeManage, verified.AuthType)
	require.Equal(t, now.Add(time.Hour), verified.ExpiresAt)
}

func TestValidateRefreshTokenRejectsFutureIssuedAt(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	token := signTokenClaims(t, "refresh-secret", jwt.MapClaims{
		"uid":       "user-1",
		"auth_type": "auth",
		"token_use": "refresh",
		"iat":       now.Add(time.Minute).Unix(),
		"exp":       now.Add(time.Hour).Unix(),
	})

	_, err := ValidateRefreshToken(token, "refresh-secret", types.AuthTypeUser, now)
	require.Error(t, err)
}

func parseTokenClaims(t *testing.T, tokenString, secret string) jwt.MapClaims {
	t.Helper()
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithoutClaimsValidation(),
	)
	token, err := parser.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	require.NoError(t, err)
	require.True(t, token.Valid)
	claims, ok := token.Claims.(jwt.MapClaims)
	require.True(t, ok)
	return claims
}

func signTokenClaims(t *testing.T, secret string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	require.NoError(t, err)
	return signed
}
