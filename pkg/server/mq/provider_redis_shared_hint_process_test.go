// 本文件用实际SIGKILL及两个新PID恢复同一SharedKeys组，既有租约自然到期，不重置PEL或游标。
package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// TestRedisSharedHintProcessCrashRecovery保留原Stream ID、失败前序和原生租约，独立key在接管等待中仍推进。
func TestRedisSharedHintProcessCrashRecovery(t *testing.T) {
	base := os.Getenv("CORE_SHARED_GAP_RECOVERY_ROOT")
	if base == "" {
		t.Skip("NOT_RUN: CORE_SHARED_GAP_RECOVERY_ROOT with isolated Redis")
	}
	root := filepath.Join(base, fmt.Sprintf("recover-%d", time.Now().UnixNano()))
	require.NoError(t, os.MkdirAll(root, 0700))
	p := newWindowRedisProvider(t)
	ctx := context.Background()
	var children []*exec.Cmd
	var logs []*os.File
	defer func() {
		_ = os.WriteFile(filepath.Join(root, "stop"), nil, 0600)
		for _, c := range children {
			if c.ProcessState == nil {
				_ = c.Process.Kill()
				_ = c.Wait()
			}
		}
		for _, f := range logs {
			_ = f.Close()
		}
	}()
	start := func(name string, crash bool) *exec.Cmd {
		f, err := os.Create(filepath.Join(root, name+".log"))
		require.NoError(t, err)
		logs = append(logs, f)
		c := exec.Command(os.Args[0], "-test.run=^TestRedisSharedKeySuccessorGapProcesses$", "-test.v", "-test.timeout=180s")
		flag := "0"
		if crash {
			flag = "1"
		}
		c.Env = append(os.Environ(), "GOMAXPROCS=2", "CORE_SHARED_GAP_ROOT="+root, "CORE_SHARED_GAP_WORKER="+name, "CORE_SHARED_GAP_PREFIX="+p.prefix, "CORE_SHARED_GAP_CRASH="+flag)
		c.Stdout = f
		c.Stderr = f
		require.NoError(t, c.Start())
		children = append(children, c)
		require.Eventually(t, func() bool { _, e := os.Stat(filepath.Join(root, "ready-"+name)); return e == nil }, 5*time.Second, 10*time.Millisecond)
		return c
	}
	old := start("old", true)
	for seq := 0; seq < 2; seq++ {
		require.NoError(t, p.Publish(ctx, "events", []byte(fmt.Sprintf("0:%d", seq)), &PublishOptions{OrderingKey: "key-0"}))
	}
	var original string
	require.Eventually(t, func() bool {
		b, e := os.ReadFile(filepath.Join(root, "crash.ready"))
		if e != nil {
			return false
		}
		var m struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(b, &m) != nil {
			return false
		}
		original = m.ID
		return original != ""
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, old.Process.Kill())
	require.Error(t, old.Wait(), "必须实际硬杀旧PID，不能正常退出冒充")
	first, second := start("new-1", false), start("new-2", false)
	require.NotEqual(t, first.Process.Pid, second.Process.Pid)
	require.NotEqual(t, old.Process.Pid, first.Process.Pid)
	require.NoError(t, p.Publish(ctx, "events", []byte("1:0"), &PublishOptions{OrderingKey: "key-1"}))
	require.Eventually(t, func() bool {
		rows, e := p.client.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: p.streamKey("events"), Group: "workers", Start: "-", End: "+", Count: 8}).Result()
		if e != nil {
			return false
		}
		for _, row := range rows {
			if row.ID == original {
				return true
			}
		}
		return false
	}, 3*time.Second, time.Millisecond)
	// 不改lease TTL或锁；健康key应在原key租约自然到期之前确认。
	require.Eventually(t, func() bool {
		groups, e := p.client.XInfoGroups(ctx, p.streamKey("events")).Result()
		if e != nil || len(groups) != 1 || groups[0].Lag != 0 {
			return false
		}
		return p.client.XPending(ctx, p.streamKey("events"), "workers").Val().Count == 2
	}, 5*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool { return p.client.XPending(ctx, p.streamKey("events"), "workers").Val().Count == 0 }, ownerLeaseTTL(ReliableSubscribeOptions{})+10*time.Second, 10*time.Millisecond)
	require.NoError(t, os.WriteFile(filepath.Join(root, "stop"), nil, 0600))
	require.NoError(t, first.Wait())
	require.NoError(t, second.Wait())
	var recovered []sharedGapEvent
	count := 0
	for _, name := range []string{"new-1", "new-2"} {
		b, e := os.ReadFile(filepath.Join(root, "events-"+name+".jsonl"))
		require.NoError(t, e)
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			var event sharedGapEvent
			require.NoError(t, json.Unmarshal([]byte(line), &event))
			if event.Kind == "handler" {
				count++
				if event.Key == 0 {
					recovered = append(recovered, event)
				}
			}
		}
	}
	require.Equal(t, 3, count)
	require.Len(t, recovered, 2)
	sort.Slice(recovered, func(i, j int) bool { return recovered[i].Start < recovered[j].Start })
	require.Equal(t, original, recovered[0].ID)
	require.Equal(t, 0, recovered[0].Sequence)
	require.Equal(t, 1, recovered[1].Sequence)
	require.GreaterOrEqual(t, recovered[1].Start, recovered[0].End)
	t.Logf("old PID=%d, fresh PID=%d/%d; original ID retained; healthy key progresses; evidence=%s", old.Process.Pid, first.Process.Pid, second.Process.Pid, root)
}
