package melody

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// TestMelodyManagerCloseWaitsForAdmittedUpgrade 验证关闭不会漏掉已通过入口检查、但尚未注册到 Hub 的并发握手。
func TestMelodyManagerCloseWaitsForAdmittedUpgrade(t *testing.T) {
	manager := NewMelodyManager(nil)
	reachedUpgrade := make(chan struct{})
	releaseUpgrade := make(chan struct{})
	manager.GetMelody().Upgrader.CheckOrigin = func(*http.Request) bool {
		close(reachedUpgrade)
		<-releaseUpgrade
		return true
	}

	server := httptest.NewServer(http.HandlerFunc(manager.ServeWS))
	defer server.Close()

	dialDone := make(chan *websocket.Conn, 1)
	go func() {
		conn, _, _ := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
		dialDone <- conn
	}()

	select {
	case <-reachedUpgrade:
	case <-time.After(time.Second):
		t.Fatal("等待 WebSocket 握手进入升级边界超时")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- manager.Close() }()
	select {
	case err := <-closeDone:
		require.NoError(t, err)
		close(releaseUpgrade)
		t.Fatal("Close 在已接纳的握手完成注册前返回")
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseUpgrade)
	select {
	case err := <-closeDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("等待 MelodyManager 关闭超时")
	}

	select {
	case conn := <-dialDone:
		if conn != nil {
			defer conn.Close()
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
			_, _, err := conn.ReadMessage()
			require.Error(t, err, "服务停止后不得留下可用 WebSocket 连接")
		}
	case <-time.After(time.Second):
		t.Fatal("等待 WebSocket 握手返回超时")
	}
	require.Eventually(t, func() bool {
		return manager.GetConnectionCounter().Get() == 0
	}, time.Second, 10*time.Millisecond)
}

// TestMelodyManagerCloseTimeoutFinishesAfterLateAdmission 验证调用方超时后仍由后台收尾，不会把迟到 Session 留在已关闭 Hub。
func TestMelodyManagerCloseTimeoutFinishesAfterLateAdmission(t *testing.T) {
	manager := NewMelodyManager(nil)
	reachedUpgrade := make(chan struct{})
	releaseUpgrade := make(chan struct{})
	manager.GetMelody().Upgrader.CheckOrigin = func(*http.Request) bool {
		close(reachedUpgrade)
		<-releaseUpgrade
		return true
	}

	server := httptest.NewServer(http.HandlerFunc(manager.ServeWS))
	defer server.Close()
	dialDone := make(chan *websocket.Conn, 1)
	go func() {
		conn, _, _ := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
		dialDone <- conn
	}()

	select {
	case <-reachedUpgrade:
	case <-time.After(time.Second):
		t.Fatal("等待 WebSocket 握手进入升级边界超时")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, manager.CloseContext(ctx), context.DeadlineExceeded)

	close(releaseUpgrade)
	select {
	case conn := <-dialDone:
		if conn != nil {
			defer conn.Close()
		}
	case <-time.After(time.Second):
		t.Fatal("等待迟到 WebSocket 握手返回超时")
	}

	finishCtx, finishCancel := context.WithTimeout(context.Background(), time.Second)
	defer finishCancel()
	require.NoError(t, manager.CloseContext(finishCtx))
	require.Eventually(t, func() bool {
		return manager.GetMelody().Len() == 0 && manager.GetConnectionCounter().Get() == 0
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, manager.Close())
}

// TestMelodyManagerRejectedConnectionDoesNotDoubleDecrement 验证超过连接上限的 Session 只回滚一次计数。
func TestMelodyManagerRejectedConnectionDoesNotDoubleDecrement(t *testing.T) {
	manager := NewMelodyManager(nil)
	manager.maxConnections = 0
	server := httptest.NewServer(http.HandlerFunc(manager.ServeWS))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
	_, _, err = conn.ReadMessage()
	require.Error(t, err)

	require.Eventually(t, func() bool {
		manager.stats.mu.RLock()
		active := manager.stats.activeConnections
		manager.stats.mu.RUnlock()
		return manager.GetConnectionCounter().Get() == 0 && active == 0
	}, time.Second, 10*time.Millisecond)
	require.Never(t, func() bool {
		manager.stats.mu.RLock()
		active := manager.stats.activeConnections
		manager.stats.mu.RUnlock()
		return manager.GetConnectionCounter().Get() < 0 || active < 0
	}, 200*time.Millisecond, 10*time.Millisecond)
	require.NoError(t, manager.Close())
}
