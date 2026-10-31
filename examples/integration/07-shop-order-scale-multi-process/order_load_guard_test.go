// 本文件验证容量测试的时间边界和取消守护，避免停发后仍制造负载或用提前结束冒充通过。
package shoporderscalemultiprocess

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestOrderLoadWindowBounds 验证稳态窗口只能显式选择 1 至 600 秒，默认保持原 30 秒。
func TestOrderLoadWindowBounds(t *testing.T) {
	for _, value := range []string{"0", "-1", "601", "invalid"} {
		_, err := parseOrderLoadSeconds(value)
		require.Error(t, err)
	}
	for value, expected := range map[string]int{"": 30, "1": 1, "600": 600} {
		actual, err := parseOrderLoadSeconds(value)
		require.NoError(t, err)
		require.Equal(t, expected, actual)
	}
}

// TestCanceledOrderLoadDoesNotSend 验证已取消窗口不发送请求，记录 interrupted 而不是伪装完成。
func TestCanceledOrderLoadDoesNotSend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	results, summary := runOrderLoadStage(ctx, &http.Client{Timeout: time.Second}, "http://127.0.0.1:1", "", 1, 2000, time.Second, start)
	require.Empty(t, results)
	require.Equal(t, true, summary["interrupted"])
	require.Less(t, time.Since(start), time.Second)
}
