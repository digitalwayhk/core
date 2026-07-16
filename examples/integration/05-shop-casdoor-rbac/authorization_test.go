package casdoorrbacshop_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCasdoorRoleAndRouteDomainsStayIsolated(t *testing.T) {
	userToken := suite.TokenFor(t, "authorization-user", 0)
	adminToken := suite.TokenFor(t, "authorization-admin", 1)

	publicResponse := suite.RequestJSON(t, http.MethodGet, "/api/casdoorrbacshop/getproducts", "", nil)
	require.Equal(t, http.StatusOK, publicResponse.HTTPStatus)
	require.True(t, publicResponse.Success, publicResponse.ErrorMessage)

	privateResponse := suite.RequestJSON(t, http.MethodGet, "/api/casdoorrbacshop/getorders", userToken, nil)
	require.Equal(t, http.StatusOK, privateResponse.HTTPStatus)
	require.True(t, privateResponse.Success, privateResponse.ErrorMessage)

	manageResponse := suite.RequestJSON(t, http.MethodPost, "/api/manage/casdoorrbacshop/productmanage/view", adminToken, nil)
	require.Equal(t, http.StatusOK, manageResponse.HTTPStatus)
	require.True(t, manageResponse.Success, manageResponse.ErrorMessage)

	forgedManage := suite.RequestJSON(t, http.MethodPost, "/api/manage/casdoorrbacshop/productmanage/view", userToken, map[string]string{
		"role": "administrator", "shop_scope": "manage",
	})
	assert.Equal(t, http.StatusUnauthorized, forgedManage.HTTPStatus)

	forgedPrivate := suite.RequestJSON(t, http.MethodGet, "/api/casdoorrbacshop/getorders?role=user&shop_scope=order", adminToken, nil)
	assert.Equal(t, http.StatusUnauthorized, forgedPrivate.HTTPStatus)

	withoutToken := suite.RequestJSON(t, http.MethodGet, "/api/casdoorrbacshop/getorders", "", nil)
	assert.Equal(t, http.StatusUnauthorized, withoutToken.HTTPStatus)
}
