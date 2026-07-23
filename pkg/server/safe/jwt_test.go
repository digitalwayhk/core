package safe

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/require"
)

func TestValidateJWTTokenUsesInternalAccessTokenWhenCasdoorEnabled(t *testing.T) {
	pair, err := IssueTokenPair(TokenIssueRequest{
		Claims:              NewClaims("user-1", "用户一"),
		AuthType:            types.AuthTypeUser,
		IssuedAt:            time.Now().Add(-time.Second),
		AccessSecret:        "access-secret",
		AccessExpireSeconds: 60,
	})
	require.NoError(t, err)

	uid, username, err := ValidateJWTToken(pair.AccessToken, config.AuthSecret{
		AccessSecret: "access-secret",
		CasDoor:      config.CasDoorConfig{Enable: true},
	})
	require.NoError(t, err)
	require.Equal(t, "user-1", uid)
	require.Equal(t, "用户一", username)
}

func TestValidateJWTTokenRejectsLegacyTokenWithoutAccessPurpose(t *testing.T) {
	now := time.Now()
	token := signTokenClaims(t, "access-secret", jwt.MapClaims{
		"uid": "user-1",
		"iat": now.Add(-time.Second).Unix(),
		"exp": now.Add(time.Minute).Unix(),
	})

	_, _, err := ValidateJWTToken(token, config.AuthSecret{AccessSecret: "access-secret"})
	require.Error(t, err)
}

func TestValidateJWTTokenRejectsRefreshPurpose(t *testing.T) {
	now := time.Now()
	token := signTokenClaims(t, "access-secret", jwt.MapClaims{
		"uid":       "user-1",
		"auth_type": "auth",
		"token_use": "refresh",
		"iat":       now.Add(-time.Second).Unix(),
		"exp":       now.Add(time.Minute).Unix(),
	})

	_, _, err := ValidateJWTToken(token, config.AuthSecret{AccessSecret: "access-secret"})
	require.Error(t, err)
}

func TestClaimsJSONKeepsLegacyFieldNames(t *testing.T) {
	claims := NewClaims("user-1", "用户一")
	data, err := json.Marshal(claims)
	require.NoError(t, err)
	require.JSONEq(t, `{"userid":"user-1","username":"用户一","args":{}}`, string(data))
}
