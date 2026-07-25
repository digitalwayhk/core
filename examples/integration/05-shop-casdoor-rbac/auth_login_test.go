package casdoorrbacshop_test

import (
	"net/http"
	"testing"
	"time"

	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/digitalwayhk/core/pkg/server/safe"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCasdoorAuthAndManageLoginUseSeparatedDomains(t *testing.T) {
	authToken := suite.TokenFor(t, "alice", 0)
	manageToken := suite.TokenFor(t, "manager", 1)

	authIdentity, err := safe.ValidateAccessToken(
		authToken, authAccessSecret, types.AuthTypeUser, time.Now().UTC(),
	)
	require.NoError(t, err)
	assert.Equal(t, types.AuthProviderCasdoor, authIdentity.Identity.Provider)
	assert.Equal(t, "alice", authIdentity.Identity.ProviderSubject)
	assert.Equal(t, "user", authIdentity.Claims["role"])
	assert.Equal(t, "order", authIdentity.Claims["shop_scope"])

	manageIdentity, err := safe.ValidateAccessToken(
		manageToken, manageAccessSecret, types.AuthTypeManage, time.Now().UTC(),
	)
	require.NoError(t, err)
	assert.Equal(t, types.AuthProviderCasdoor, manageIdentity.Identity.Provider)
	assert.Equal(t, "manager", manageIdentity.Identity.ProviderSubject)
	assert.Equal(t, "administrator", manageIdentity.Claims["role"])
	assert.Equal(t, "manage", manageIdentity.Claims["shop_scope"])

	_, err = safe.ValidateAccessToken(authToken, manageAccessSecret, types.AuthTypeManage, time.Now().UTC())
	require.Error(t, err)
	_, err = safe.ValidateAccessToken(manageToken, authAccessSecret, types.AuthTypeUser, time.Now().UTC())
	require.Error(t, err)
}

func TestCasdoorRefreshReappliesShopRoleClaims(t *testing.T) {
	pair := suite.TokenPairFor(t, "refresh-user", 0)
	response := suite.RequestJSON(t, http.MethodPost, "/api/refresh", "", map[string]string{
		"token": pair.RefreshToken, "type": "auth",
	})
	require.True(t, response.Success, response.ErrorMessage)
	refreshedToken, err := integration.AccessTokenFromData(response.Data)
	require.NoError(t, err)
	verified, err := safe.ValidateAccessToken(
		refreshedToken, authAccessSecret, types.AuthTypeUser, time.Now().UTC(),
	)
	require.NoError(t, err)
	assert.Equal(t, "user", verified.Claims["role"])
	assert.Equal(t, "order", verified.Claims["shop_scope"])
}
