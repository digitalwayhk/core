package simpleshop_test

import (
	"encoding/json"
	"net/http"
	"testing"

	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/require"
)

func TestTestTokenReturnsHookedAccessToken(t *testing.T) {
	response := suite.RequestJSON(t, http.MethodGet, "/api/servermanage/testtoken?userid=hook-user", "", nil)
	require.True(t, response.Success, response.ErrorMessage)
	var tokens integration.TokenResponse
	require.NoError(t, json.Unmarshal(response.Data, &tokens))
	require.NotEmpty(t, tokens.AccessToken)
	require.NotEmpty(t, tokens.RefreshToken)

	access := parseUnverifiedClaims(t, tokens.AccessToken)
	refresh := parseUnverifiedClaims(t, tokens.RefreshToken)
	require.Equal(t, "hook-user", access["uid"])
	require.Equal(t, "access", access["token_use"])
	require.Equal(t, "shop", access["example_service"])
	require.Equal(t, "refresh", refresh["token_use"])
	require.NotContains(t, refresh, "example_service")
}

func parseUnverifiedClaims(t *testing.T, tokenString string) jwt.MapClaims {
	t.Helper()
	claims := jwt.MapClaims{}
	_, _, err := jwt.NewParser().ParseUnverified(tokenString, claims)
	require.NoError(t, err)
	return claims
}
