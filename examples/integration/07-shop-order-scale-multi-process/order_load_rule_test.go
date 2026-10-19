// 本文件通过真实Manage配置与内建回退完全相同的规则，区分缺配置负载和正向缓存负载。
package shoporderscalemultiprocess

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/stretchr/testify/require"
)

// prepareOrderLoadDefaultRule 在任何压测请求前通过现有Manage写入规则，Core与原生共用同一事实。
func prepareOrderLoadDefaultRule(t *testing.T, order *integration.Suite, evidence string) {
	t.Helper()
	rule := models.NewOrderRule()
	payload := map[string]interface{}{
		"ruleCode": rule.RuleCode, "ruleName": rule.RuleName,
		"minQuantity": rule.MinQuantity, "maxQuantity": rule.MaxQuantity,
		"maxOrderAmount": rule.MaxOrderAmount, "enabled": rule.Enabled,
		"ruleRevision": rule.RuleRevision,
	}
	token := order.TokenFor(t, "900001", 1)
	response := order.RequestJSON(t, http.MethodPost, "/api/manage/shop-order/orderrulemanage/add", token, payload)
	require.True(t, response.Success, response.ErrorMessage)
	require.NotZero(t, parseDockerManageID(t, response.Data))
	writeLoadJSON(t, filepath.Join(evidence, "default-rule-fixture.json"), payload)
}
