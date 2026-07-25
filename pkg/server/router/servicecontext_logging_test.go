package router

import (
	"bytes"
	"strings"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/zeromicro/go-zero/core/logx"
)

func TestCallServiceFailureLogKeepsTraceWithoutPayload(t *testing.T) {
	var output bytes.Buffer
	previous := logx.Reset()
	logx.SetWriter(logx.NewWriter(&output))
	t.Cleanup(func() {
		logx.Reset()
		if previous != nil {
			logx.SetWriter(previous)
		}
	})

	context := &ServiceContext{Service: &types.Service{Name: "source"}}
	payload := &types.PayLoad{
		TraceID:       "trace-log-contract",
		TargetService: "target",
		TargetPath:    "/api/target/action",
		Instance:      map[string]string{"secret": "must-not-appear"},
	}
	if _, err := context.CallService(payload); err == nil {
		t.Fatal("缺少目标地址时应返回错误")
	}

	logOutput := output.String()
	for _, expected := range []string{"service_call_failed", "trace-log-contract", "/api/target/action"} {
		if !strings.Contains(logOutput, expected) {
			t.Fatalf("日志缺少 %q: %s", expected, logOutput)
		}
	}
	if strings.Contains(logOutput, "must-not-appear") {
		t.Fatalf("日志泄露了 payload 内容: %s", logOutput)
	}
}
