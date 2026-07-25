package casdoorrbacshop_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCasdoorWebhookRevokesTokenAndWritesOneAuditRecord(t *testing.T) {
	const subject = "webhook-user"
	userToken := suite.TokenFor(t, subject, 0)
	adminToken := suite.TokenFor(t, "webhook-auditor", 1)
	before := suite.RequestJSON(t, http.MethodGet, "/api/casdoorrbacshop/getorders", userToken, nil)
	require.Equal(t, http.StatusOK, before.HTTPStatus)

	webhook := newWebhookFixture(types.AuthTypeUser, "logout", subject, true)
	require.True(t, suite.SendWebhook(t, webhook).Success)
	require.True(t, suite.SendWebhook(t, webhook).Success, "相同 Webhook 重试必须幂等接受")

	after := suite.RequestJSON(t, http.MethodGet, "/api/casdoorrbacshop/getorders", userToken, nil)
	assert.Equal(t, http.StatusUnauthorized, after.HTTPStatus)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		search := suite.RequestJSON(t, http.MethodPost, "/api/manage/casdoorrbacshop/identityeventmanage/search", adminToken,
			map[string]interface{}{"page": 1, "size": 20})
		if search.Success {
			var table struct {
				Rows []map[string]interface{} `json:"rows"`
			}
			require.NoError(t, json.Unmarshal(search.Data, &table))
			matches := 0
			for _, row := range table.Rows {
				if row["userID"] == "shop-auth-org-"+subject && row["eventType"] == "logout" {
					matches++
				}
			}
			if matches == 1 {
				return
			}
			assert.LessOrEqual(t, matches, 1, "重复 Webhook 不得生成重复审计记录")
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("超时前未查询到唯一身份事件审计记录")
}
