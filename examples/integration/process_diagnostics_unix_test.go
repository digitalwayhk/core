//go:build linux || darwin || freebsd

package integration

import (
	"github.com/stretchr/testify/require"
	_ "github.com/zeromicro/go-zero/core/proc"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticChild(t *testing.T) {
	if os.Getenv("CORE_DIAGNOSTIC_HELPER") != "1" {
		t.Skip("仅诊断子进程使用")
	}
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, os.WriteFile(os.Getenv("CORE_DIAGNOSTIC_READY"), []byte("ready"), 0600))
	for {
		time.Sleep(time.Second)
	}
}

// TestSuiteDumpsOwnedProcess 验证得到真实子进程栈而非父进程或旧文件。
func TestSuiteDumpsOwnedProcess(t *testing.T) {
	private := t.TempDir()
	ready := filepath.Join(private, "ready")
	cmd := exec.Command(os.Args[0], "-test.run=^TestDiagnosticChild$", "-test.timeout=20s")
	cmd.Env = append(os.Environ(), "CORE_DIAGNOSTIC_HELPER=1", "CORE_DIAGNOSTIC_READY="+ready, "TMPDIR="+private)
	require.NoError(t, cmd.Start())
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	require.Eventually(t, func() bool { _, err := os.Stat(ready); return err == nil }, 5*time.Second, 10*time.Millisecond)
	suite := &Suite{command: cmd, Binary: os.Args[0], Environment: map[string]string{"TMPDIR": private}}
	target := filepath.Join(t.TempDir(), "owned-goroutines.dump")
	require.NoError(t, suite.DumpGoroutines(target))
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	require.True(t, strings.Contains(string(data), "TestDiagnosticChild"))
	info, err := os.Stat(target)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
}
