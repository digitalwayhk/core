package types

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/core/proc"
)

func TestWebSocketNotificationSystemShutdownWaitsForAllWorkers(t *testing.T) {
	system := &WebSocketNotificationSystem{
		jobChan: make(chan *noticeJob, 4),
		workers: 2,
		closeCh: make(chan struct{}),
	}
	system.Start()
	system.Shutdown()

	if system.isStarted.Load() {
		t.Fatal("通知系统 Shutdown 后仍处于启动状态")
	}
	select {
	case <-system.statsDone:
	default:
		t.Fatal("通知系统 Shutdown 未等待统计重置 goroutine 退出")
	}
	done := make(chan struct{})
	go func() {
		system.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("通知系统 Shutdown 未等待全部 worker 退出")
	}

	system.Start()
	if system.isStarted.Load() {
		t.Fatal("进程级通知系统关闭后不应在同一实例重启")
	}
}

func TestWebSocketNotificationSystemSubmitAndShutdownAreSynchronized(t *testing.T) {
	system := &WebSocketNotificationSystem{
		jobChan: make(chan *noticeJob, 128),
		workers: 0,
		closeCh: make(chan struct{}),
	}
	system.Start()

	startSubmit := make(chan struct{})
	var submitters sync.WaitGroup
	for range 8 {
		submitters.Add(1)
		go func() {
			defer submitters.Done()
			<-startSubmit
			for range 16 {
				system.Submit(nil)
			}
		}()
	}

	close(startSubmit)
	system.Shutdown()
	submitters.Wait()
	if system.Submit(nil) {
		t.Fatal("通知系统关闭后仍接受任务")
	}
}

func TestWebSocketNotificationSystemShutdownBeforeStartPreventsWorkers(t *testing.T) {
	system := &WebSocketNotificationSystem{
		jobChan: make(chan *noticeJob, 4),
		workers: 2,
		closeCh: make(chan struct{}),
	}
	system.Shutdown()
	system.Start()
	if system.isStarted.Load() {
		t.Fatal("通知系统关闭后仍启动 worker")
	}
}

func TestPeriodicWebSocketCleanupCanStopAndWait(t *testing.T) {
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

func TestWebSocketProcessShutdownStopsGlobalWorkers(t *testing.T) {
	if os.Getenv("CORE_TEST_WEBSOCKET_SHUTDOWN_CHILD") == "1" {
		system := getGlobalNotificationSystem()
		system.workers = 2
		StartPeriodicCleanup()
		system.Start()
		proc.Shutdown()
		if system.isStarted.Load() {
			t.Fatal("进程关闭后全局 WebSocket 通知系统仍在运行")
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := StopPeriodicCleanup(ctx); err != nil {
			t.Fatalf("进程关闭后周期清理未退出: %v", err)
		}
		return
	}

	command := exec.Command(os.Args[0], "-test.run=^TestWebSocketProcessShutdownStopsGlobalWorkers$")
	command.Env = append(os.Environ(), "CORE_TEST_WEBSOCKET_SHUTDOWN_CHILD=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("子进程 WebSocket 关闭测试失败: %v\n%s", err, output)
	}
}
