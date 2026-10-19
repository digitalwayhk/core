// 本文件通过真实Manage和买家HTTP验证既有订单副本已加载规则后的修改与恢复。
package shoporderscalemultiprocess

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	ordermodels "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

// TestOrderRuleManageRefreshesExistingReplica 验证管理更新后既有路由无需手工失效或重启。
func TestOrderRuleManageRefreshesExistingReplica(t *testing.T) {
	_, ns, processes := startStableNativeProcesses(t, 1)
	t.Setenv("SHOP_ORDER_REMOTE_MYSQL_DATABASE", "core_"+ns)
	admin := prepareDockerAdminFixture(t, processes[1])
	supplier := prepareDockerSupplierFixture(t, processes[1], admin)
	user := processes[0]
	token := user.TokenFor(t, "720001", 0)
	order := processes[2]
	adminToken := order.TokenFor(t, contract.PlatformAdminUserID, 1)
	response := order.RequestJSON(t, http.MethodPost, "/api/manage/shop-order/orderrulemanage/add", adminToken, map[string]interface{}{
		"ruleCode": "default", "ruleName": "规则更新验收", "minQuantity": 1, "maxQuantity": 100, "maxOrderAmount": "99999", "enabled": true, "ruleRevision": 1,
	})
	require.True(t, response.Success, response.ErrorMessage)
	rule := ordermodels.NewOrderRule()
	require.NoError(t, json.Unmarshal(response.Data, rule))
	// 先成功下单使同一路由加载旧规则，再管理更新；没有重启或手工失效。
	for i := range 2 {
		createDockerBuyerOrderWithRequest(t, user, token, supplier.ProductID, fmt.Sprintf("rule-warm-%d", i))
	}
	rule.MinQuantity, rule.RuleRevision = 3, 2
	response = order.RequestJSON(t, http.MethodPost, "/api/manage/shop-order/orderrulemanage/edit", adminToken, rule)
	require.True(t, response.Success, response.ErrorMessage)
	for i := range 10 {
		response = user.RequestJSON(t, http.MethodPost, "/api/shop-user/addorder", token, map[string]interface{}{
			"productID": supplier.ProductID, "quantity": 2, "requestID": fmt.Sprintf("rule-reject-%d", i),
		})
		require.False(t, response.Success, "已更新最小数量，不能继续按旧规则接单")
		require.Equal(t, servertypes.PublicCodeBusiness, response.ErrorCode)
	}
	rule.MinQuantity, rule.RuleRevision = 1, 3
	response = order.RequestJSON(t, http.MethodPost, "/api/manage/shop-order/orderrulemanage/edit", adminToken, rule)
	require.True(t, response.Success, response.ErrorMessage)
	for i := range 4 {
		createDockerBuyerOrderWithRequest(t, user, token, supplier.ProductID, fmt.Sprintf("rule-restored-%d", i))
	}
}
