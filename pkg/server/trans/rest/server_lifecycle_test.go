package rest

import (
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/proc"
)

type lifecycleTestService struct{ name string }

func (s *lifecycleTestService) ServiceName() string                    { return s.name }
func (s *lifecycleTestService) Routers() []types.IRouter               { return nil }
func (s *lifecycleTestService) SubscribeRouters() []*types.ObserveArgs { return nil }

func TestServerStopClosesListener(t *testing.T) {
	port := reserveTCPPort(t)
	service := &lifecycleTestService{name: "rest-lifecycle"}
	cfg := config.NewServiceDefaultConfig(service.name, port)
	cfg.Host = "127.0.0.1"
	ctx := &router.ServiceContext{
		Config:    cfg,
		StateChan: make(chan bool, 1),
		Service: &types.Service{
			Name:     service.name,
			Instance: service,
		},
	}
	ctx.Router = router.NewServiceRouter(ctx, service)
	server, err := NewServer(ctx, false, false)
	require.NoError(t, err)

	startDone := make(chan struct{})
	go func() {
		server.Start()
		close(startDone)
	}()
	waitForTCP(t, cfg.Host, port, true)

	server.Stop()
	server.Stop()
	waitForTCP(t, cfg.Host, port, false)

	// go-zero 的 StartWithOpts 在 listener 关闭后仍等待进程级 shutdown listener。
	// WebServer 是该生命周期的 owner；本测试显式触发进程协调器以回收 Start goroutine。
	proc.Shutdown()
	waitForDone(t, startDone, "REST Start")
	require.False(t, ctx.IsRun())
}

func reserveTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return port
}

func waitForTCP(t *testing.T, host string, port int, wantOpen bool) {
	t.Helper()
	address := net.JoinHostPort(host, strconv.Itoa(port))
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", address, 20*time.Millisecond)
		if err == nil {
			_ = conn.Close()
		}
		return (err == nil) == wantOpen
	}, 2*time.Second, 10*time.Millisecond)
}

func waitForDone(t *testing.T, done <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("等待 %s 返回超时", name)
	}
}
