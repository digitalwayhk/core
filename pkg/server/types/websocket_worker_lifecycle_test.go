package types

import (
	"context"
	"testing"
	"time"
)

func TestWebSocketNotificationSystemCompatibilityShellIsStateless(t *testing.T) {
	system := &WebSocketNotificationSystem{}
	system.Start()
	if system.Submit(nil) {
		t.Fatal("废弃兼容壳不应接受通知任务")
	}
	system.ResetStats()
	if system.IsHealthy() {
		t.Fatal("废弃兼容壳不应报告健康")
	}
	if stats := system.GetStats(); len(stats) != 0 {
		t.Fatalf("废弃兼容壳不应保存进程状态：%v", stats)
	}
	system.Shutdown()
}

func TestPeriodicWebSocketCleanupCompatibilityMethodsAreSafe(t *testing.T) {
	StartPeriodicCleanup()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := StopPeriodicCleanup(ctx); err != nil {
		t.Fatalf("停止 WebSocket 周期清理失败: %v", err)
	}
	if err := StopPeriodicCleanup(ctx); err != nil {
		t.Fatalf("重复停止 WebSocket 周期清理失败: %v", err)
	}
}
