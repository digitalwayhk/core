package shopmicroservices_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSupplierManageScopesOwnerAndAdmin(t *testing.T) {
	ownerToken := suites.supplier.TokenFor(t, "supplier-manage-owner", 1)
	otherToken := suites.supplier.TokenFor(t, "supplier-manage-other", 1)
	owner := suites.supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/suppliermanage/search", ownerToken, map[string]interface{}{"page": 1, "size": 10})
	require.True(t, owner.Success, owner.ErrorMessage)
	other := suites.supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/suppliermanage/search", otherToken, map[string]interface{}{"page": 1, "size": 10})
	require.True(t, other.Success, other.ErrorMessage)
	require.NotEqual(t, string(owner.Data), string(other.Data))
	admin := suites.supplier.TokenFor(t, "platform-admin", 1)
	all := suites.supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/suppliermanage/search", admin, map[string]interface{}{"page": 1, "size": 100})
	require.True(t, all.Success, all.ErrorMessage)
}

func TestSupplierDisableInvalidatesBuyerCatalog(t *testing.T) {
	product, ownerToken := addProduct(t, "supplier-cache")
	path := "/api/shop-user/getproducts?code=" + product.Code
	require.True(t, suites.user.RequestJSON(t, http.MethodGet, path, "", nil).Success)
	searched := suites.supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/suppliermanage/search", ownerToken, map[string]interface{}{"page": 1, "size": 10})
	require.True(t, searched.Success, searched.ErrorMessage)
	var table struct {
		Rows []struct {
			ID string `json:"id"`
		} `json:"rows"`
	}
	require.NoError(t, json.Unmarshal(searched.Data, &table))
	require.Len(t, table.Rows, 1)
	admin := suites.supplier.TokenFor(t, "platform-admin", 1)
	disabled := suites.supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/suppliermanage/setsupplierenabled", admin, map[string]interface{}{"id": table.Rows[0].ID, "enabled": false})
	require.True(t, disabled.Success, disabled.ErrorMessage)
	require.Eventually(t, func() bool {
		response := suites.user.RequestJSON(t, http.MethodGet, path, "", nil)
		return response.Success && !jsonContains(response.Data, product.Code)
	}, 5*time.Second, 25*time.Millisecond)
}

func jsonContains(data json.RawMessage, value string) bool {
	return strings.Contains(string(data), value)
}
