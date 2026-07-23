package melody

import (
	"testing"
	"time"
)

func TestConnectionRateLimiterCloseWaitsForCleanupWorker(t *testing.T) {
	limiter := newConnectionRateLimiter(time.Millisecond)
	limiter.Close()
	limiter.Close()

	select {
	case <-limiter.done:
	case <-time.After(time.Second):
		t.Fatal("等待连接限流清理 worker 退出超时")
	}
}

func TestMelodyManagerCloseWaitsForOwnedWorkers(t *testing.T) {
	manager := NewMelodyManager(nil)
	if err := manager.Close(); err != nil {
		t.Fatalf("关闭 MelodyManager 失败: %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("重复关闭 MelodyManager 失败: %v", err)
	}

	select {
	case <-manager.monitorDone:
	default:
		t.Fatal("MelodyManager.Close 未等待统计监控退出")
	}
	select {
	case <-manager.connectionLimit.done:
	default:
		t.Fatal("MelodyManager.Close 未等待限流清理 worker 退出")
	}
}
