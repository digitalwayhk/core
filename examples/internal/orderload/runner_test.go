// 本文件用受控HTTP服务验证发送器响应分类、命名空间隔离及发送量守恒。
package orderload

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestStageResponseContract 验证沿用原发送器的成功、缺ID、业务失败和非JSON分类。
func TestStageResponseContract(t *testing.T) {
	for _, tc := range []struct {
		body    string
		status  int
		success bool
		failure string
	}{
		{`{"success":true,"data":{"orderID":9}}`, 200, true, ""},
		{`{"success":true,"data":{}}`, 200, false, "响应缺少合法 orderID"},
		{`{"success":false,"errorMessage":"拒绝"}`, 200, false, "拒绝"},
		{`unavailable`, 503, false, "响应不是 JSON"},
		{strings.Repeat("x", (1<<20)+1), 200, false, "响应不是 JSON"},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); _, _ = fmt.Fprint(w, tc.body) }))
		results, summary := RunStage(context.Background(), server.Client(), server.URL, "secret", 1, 200, 20*time.Millisecond, time.Now(), "run-test")
		server.Close()
		if len(results) == 0 {
			t.Fatal("没有执行请求")
		}
		for _, result := range results {
			if result.Success != tc.success || result.Error != tc.failure {
				t.Fatalf("分类不匹配: success=%v error=%q", result.Success, result.Error)
			}
		}
		if summary["scheduled"] != len(results)+summary["generator_dropped"].(int)+summary["generator_not_started"].(int) {
			t.Fatal("计划发送量不守恒")
		}
	}
}

// TestStageNamespaceAndCancellation 验证预热/主档不撞键，以及取消后不发送。
func TestStageNamespaceAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"success":true,"data":{"orderID":9}}`)
	}))
	defer server.Close()
	seen := map[string]bool{}
	for _, stage := range []string{"run1-warm", "run1-main", "run2-main"} {
		results, _ := RunStage(context.Background(), server.Client(), server.URL, "secret", 1, 500, 20*time.Millisecond, time.Now(), stage)
		for _, r := range results {
			if seen[r.RequestID] {
				t.Fatal("阶段间requestID碰撞")
			}
			seen[r.RequestID] = true
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results, summary := RunStage(ctx, server.Client(), server.URL, "", 1, 500, time.Second, time.Now(), "cancelled")
	if len(results) != 0 || summary["interrupted"] != true || summary["generator_not_started"] != 500 {
		t.Fatal("取消预算错误")
	}
}
