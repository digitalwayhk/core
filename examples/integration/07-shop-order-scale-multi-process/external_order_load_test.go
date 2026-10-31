// 本文件管理独立发送进程及其受限凭证文件，取消时等待请求结果落盘后再核对。
package shoporderscalemultiprocess

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/internal/orderload"
	"github.com/stretchr/testify/require"
)

// TestExternalSenderProcess 验证真实独立发送器使用受限文件、返回请求结果并移除凭证文件。
func TestExternalSenderProcess(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "order-load")
	output, err := exec.Command("go", "build", "-o", binary, "github.com/digitalwayhk/core/examples/07-shop-order-scale/tools/order-load").CombinedOutput()
	require.NoError(t, err, "%s", output)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer process-token" {
			w.WriteHeader(403)
			return
		}
		_, _ = fmt.Fprint(w, `{"success":true,"data":{"orderID":9}}`)
	}))
	defer server.Close()
	stage := orderload.Stage{URL: server.URL, Token: "process-token", ProductID: 1, Rate: 100, DurationMS: 100, Origin: time.Now(), Namespace: "process-test", Output: filepath.Join(root, "stage")}
	results, summary := runExternalOrderLoadStage(t, context.Background(), binary, stage)
	require.Len(t, results, 10)
	require.EqualValues(t, 10, summary["successful"])
	for _, result := range results {
		require.EqualValues(t, 9, result.OrderID)
		require.True(t, result.Success)
	}
	require.NoFileExists(t, stage.Output+"-config.json")
}

func runExternalOrderLoadStage(t *testing.T, ctx context.Context, binary string, stage orderload.Stage) ([]orderLoadResult, map[string]interface{}) {
	t.Helper()
	configPath := stage.Output + "-config.json"
	data, err := json.Marshal(stage)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, data, 0600))
	defer func() { require.NoError(t, os.Remove(configPath)) }()
	cmd := exec.Command(binary, "-config", configPath)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	require.NoError(t, cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case err = <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			err = <-done
		}
	}
	require.NoError(t, err, "独立发送器失败: %s", output.String())
	var results []orderLoadResult
	var summary map[string]interface{}
	data, err = os.ReadFile(filepath.Join(stage.Output, "requests.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &results))
	data, err = os.ReadFile(filepath.Join(stage.Output, "summary.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &summary))
	// JSON跨进程后数字为float64；恢复既有验收使用的整数字段，保持原断言口径。
	for _, key := range []string{"target_rps", "scheduled", "started", "generator_dropped", "generator_not_started", "successful", "failed"} {
		if value, ok := summary[key].(float64); ok {
			summary[key] = int(value)
		}
	}
	return results, summary
}
