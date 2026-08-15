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

// TestRuntimeGraphUAT 在显式环境变量下验证 Runtime API 拓扑与调用边闭环。
//
// 启用：
//
//	SHOP_RUN_RUNTIME_UAT=1
//	SHOP_RUNTIME_API_BASE=http://127.0.0.1:18181
//	SHOP_RUNTIME_TOKEN=<servermanage access token>
//
// 可选：
//
//	SHOP_RUNTIME_TRAFFIC_URL  产生流量的入口（默认 POST {base}/api/shop-user/... 不自动伪造业务）
//	SHOP_RUNTIME_UAT_TIMEOUT  轮询超时，默认 90s
//
// 前置：07 compose 已 up（含 prometheus）；UAT 会轮询 topology 直到出现：
//   - sync: shop-user -> shop-order
//   - async: shop-order -> shop-user
//
// 若环境尚未产生流量，可先手工下单或外部脚本造流量后再跑。
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

	timeout := 90 * time.Second
	if raw := os.Getenv("SHOP_RUNTIME_UAT_TIMEOUT"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			timeout = d
		}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	deadline := time.Now().Add(timeout)

	var lastTopo map[string]interface{}
	var lastEdges []map[string]interface{}
	for time.Now().Before(deadline) {
		topo := postRuntimeJSON(t, client, base+"/api/servermanage/runtimetopology", token, map[string]string{
			"window": "15s",
		})
		lastTopo = topo
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
		if !names["shop-user"] || !names["shop-order"] {
			time.Sleep(3 * time.Second)
			continue
		}

		edges := extractEdges(topo)
		lastEdges = edges
		hasSync := hasEdge(edges, "shop-user", "shop-order", "sync")
		hasAsync := hasEdge(edges, "shop-order", "shop-user", "async")
		if hasSync && hasAsync {
			// 成功：再校验 shop-order 详情组件存在
			detail := postRuntimeJSON(t, client, base+"/api/servermanage/runtimeservice", token, map[string]string{
				"window":  "15s",
				"service": "shop-order",
			})
			comps, _ := detail["components"].([]interface{})
			require.NotEmpty(t, comps, "shop-order components must be present")
			return
		}
		time.Sleep(3 * time.Second)
	}

	t.Fatalf(
		"timeout waiting for runtime edges: need sync shop-user->shop-order and async shop-order->shop-user; last status=%v services=%v edges=%v",
		lastTopo["status"], lastTopo["services"], lastEdges,
	)
}

func extractEdges(topo map[string]interface{}) []map[string]interface{} {
	raw, _ := topo["edges"].([]interface{})
	out := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

func hasEdge(edges []map[string]interface{}, source, target, kind string) bool {
	for _, e := range edges {
		if fmt.Sprint(e["kind"]) != kind {
			continue
		}
		if fmt.Sprint(e["source"]) == source && fmt.Sprint(e["target"]) == target {
			return true
		}
	}
	return false
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
	if data, ok := wrapped["data"].(map[string]interface{}); ok && data != nil {
		return data
	}
	if len(wrapped) > 0 {
		return wrapped
	}
	require.Fail(t, fmt.Sprintf("unexpected response: %s", string(raw)))
	return nil
}
