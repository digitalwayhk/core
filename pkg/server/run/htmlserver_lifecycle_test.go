package run

import (
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHTMLServerStopClosesListenerAndUnblocksStart(t *testing.T) {
	port := reserveHTMLServerPort(t)
	server := NewHTMLServer(port)
	require.NoError(t, server.Prepare())
	server.Isstart <- true

	startDone := make(chan struct{})
	go func() {
		server.Start()
		close(startDone)
	}()
	waitForHTMLServerTCP(t, port, true)

	server.Stop()
	server.Stop()
	waitForHTMLServerDone(t, startDone)
	waitForHTMLServerTCP(t, port, false)
}

func TestHTMLServersUseIndependentMuxes(t *testing.T) {
	first := NewHTMLServer(reserveHTMLServerPort(t))
	second := NewHTMLServer(reserveHTMLServerPort(t))
	require.NoError(t, first.Prepare())
	require.NoError(t, second.Prepare())
	first.Isstart <- true
	second.Isstart <- true

	firstDone := make(chan struct{})
	secondDone := make(chan struct{})
	go func() {
		first.Start()
		close(firstDone)
	}()
	go func() {
		second.Start()
		close(secondDone)
	}()
	waitForHTMLServerTCP(t, first.Port, true)
	waitForHTMLServerTCP(t, second.Port, true)

	first.Stop()
	second.Stop()
	waitForHTMLServerDone(t, firstDone)
	waitForHTMLServerDone(t, secondDone)
}

func reserveHTMLServerPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return port
}

func waitForHTMLServerTCP(t *testing.T, port int, wantOpen bool) {
	t.Helper()
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", address, 20*time.Millisecond)
		if err == nil {
			_ = conn.Close()
		}
		return (err == nil) == wantOpen
	}, 2*time.Second, 10*time.Millisecond)
}

func waitForHTMLServerDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("等待 HTMLServer.Start 返回超时")
	}
}
