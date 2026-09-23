package adminui_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicAPIs(t *testing.T) {
	t.Run("GetCategories", testGetCategoriesAPI)
}

func testGetCategoriesAPI(t *testing.T) {
	token := suite.TokenFor(t, "public-admin", 1)
	_ = suite.searchCategories(t, token)

	all := suite.RequestJSON(t, http.MethodGet, "/api/catalog/getcategories", "", nil)
	require.True(t, all.Success, all.ErrorMessage)
	var categories []CategoryDTO
	require.NoError(t, json.Unmarshal(all.Data, &categories))
	require.NotEmpty(t, categories)
	assert.NotContains(t, string(all.Data), "hashCode")
	assert.NotContains(t, string(all.Data), "modelState")

	named := suite.RequestJSON(t, http.MethodGet, "/api/catalog/getcategories?name=电子", "", nil)
	require.True(t, named.Success, named.ErrorMessage)
	require.NoError(t, json.Unmarshal(named.Data, &categories))
	require.NotEmpty(t, categories)
	assert.Equal(t, "电子", categories[0].Name)
}
