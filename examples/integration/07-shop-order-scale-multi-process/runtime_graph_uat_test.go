// Package shoporderscalemultiprocess 含 Runtime 图的可选真实环境 UAT。
package shoporderscalemultiprocess

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestRuntimeGraphUAT 在显式环境变量下验证 Runtime API 拓扑闭环。
//
// 启用：
//
//	SHOP_RUN_RUNTIME_UAT=1
//	SHOP_RUNTIME_API_BASE=http://127.0.0.1:18181
//	SHOP_RUNTIME_TOKEN=<servermanage access token>
//
// 前置：07 compose 已 up（含 prometheus），并产生过真实下单流量。
func TestRuntimeGraphUAT(t *testing.T) {
	if os.Getenv("SHOP_RUN_RUNTIME_UAT") != "1" {
		t.Skip("set SHOP_RUN_RUNTIME_UAT=1 with live 07 stack to run")
	}
	base := os.Getenv("SHOP_RUNTIME_API_BASE")
	if base == "" {
		base = "http://127.0.0.1:18181"
	}
	token := os.Getenv("SHOP_RUNTIME_TOKEN")
	require.NotEmpty(t, token, "SHOP_RUNTIME_TOKEN is required")

	client := &http.Client{Timeout: 10 * time.Second}
	topo := postRuntimeJSON(t, client, base+"/api/servermanage/runtimetopology", token, map[string]string{
		"window": "15s",
	})
	require.NotEmpty(t, topo["services"], "topology services must not be empty when cluster is up")

	// 指标源故障时也必须有 status / warnings，不得用 0 伪装。
	status, _ := topo["status"].(string)
	require.NotEmpty(t, status)

	services, _ := topo["services"].([]interface{})
	names := map[string]bool{}
	for _, raw := range services {
		m, _ := raw.(map[string]interface{})
		if m == nil {
			continue
		}
		name, _ := m["service"].(string)
		names[name] = true
	}
	// 至少应识别逻辑服务名之一；未发现时给出明确失败信息。
	require.True(t, names["shop-user"] || names["shop-order"] || names["shop-supplier"],
		"expected shop-* services in topology, got %v", names)

	if names["shop-order"] {
		detail := postRuntimeJSON(t, client, base+"/api/servermanage/runtimeservice", token, map[string]string{
			"window":  "15s",
			"service": "shop-order",
		})
		svc, _ := detail["service"].(map[string]interface{})
		require.NotNil(t, svc)
		// components 必须存在且使用诚实 state。
		comps, _ := detail["components"].([]interface{})
		require.NotEmpty(t, comps)
	}
}

func postRuntimeJSON(t *testing.T, client *http.Client, url, token string, body map[string]string) map[string]interface{} {
	t.Helper()
	payload, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "url=%s body=%s", url, string(raw))

	var wrapped map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &wrapped))
	// 兼容框架 Response 包装。
	if data, ok := wrapped["data"].(map[string]interface{}); ok && data != nil {
		return data
	}
	if len(wrapped) > 0 {
		return wrapped
	}
	require.Fail(t, fmt.Sprintf("unexpected response: %s", string(raw)))
	return nil
}
