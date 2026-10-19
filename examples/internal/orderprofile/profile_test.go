package orderprofile

import (
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFlightSnapshotRequiresExplicitOptIn(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	t.Setenv("SHOP_PROFILE_PORT", strconv.Itoa(port))
	t.Setenv("SHOP_FLIGHT_RECORDER", "1")
	stop := Start()
	t.Cleanup(stop)
	client := &http.Client{Timeout: 5 * time.Second}
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/debug/pprof/flight"
	response, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 64<<20))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(string(data), "go 1.") {
		t.Fatalf("需要可解析的Go运行时trace，status=%d, bytes=%d", response.StatusCode, len(data))
	}
	if response.Trailer.Get("X-Trace-Error") != "" {
		t.Fatal(response.Trailer.Get("X-Trace-Error"))
	}
	path := filepath.Join(t.TempDir(), "snapshot.trace")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("go", "tool", "trace", "-pprof=sched", path).CombinedOutput(); err != nil || len(output) == 0 {
		t.Fatalf("快照必须能由Go工具解析，err=%v", err)
	}
	response, err = client.Post(url, "application/octet-stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST不应触发快照，status=%d", response.StatusCode)
	}
}

func TestFlightSnapshotIsAbsentWithoutOptIn(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	t.Setenv("SHOP_PROFILE_PORT", strconv.Itoa(port))
	t.Setenv("SHOP_FLIGHT_RECORDER", "")
	stop := Start()
	defer stop()
	response, err := (&http.Client{Timeout: time.Second}).Get("http://127.0.0.1:" + strconv.Itoa(port) + "/debug/pprof/flight")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("未显式开启时不应注册flight，status=%d", response.StatusCode)
	}
}
