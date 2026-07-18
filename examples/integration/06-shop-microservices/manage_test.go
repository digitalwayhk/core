// 本文件验证 06 all-in-one 集成场景下 Manage API 的角色限域和缓存失效边界。
// 供应商只能管理/查询自己的资料，平台管理员可做跨供应商禁用操作，
// 买家入口 facade 需要在供应商禁用事件后主动失效商品目录缓存。
package shopmicroservices_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSupplierManageScopesOwnerAndAdmin 验证供应商自管理查询被 owner 限域，平台管理员可查看全量供应商。
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

// TestSupplierDisableInvalidatesBuyerCatalog 验证管理员禁用供应商后，用户服务商品 facade 缓存会被事件主动失效。
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
