// 本文件复用现有测试二进制子进程模式，验证独立进程并发登记，不把goroutine当进程。
package mq

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestRedisOwnershipDirectoryRegistrationProcesses(t *testing.T) {
	if prefix := os.Getenv("CORE_OWNED_REGISTER_PREFIX"); prefix != "" {
		p := NewRedisStreamProvider(os.Getenv("CORE_TEST_REDIS_ADDR"), prefix, 0)
		require.NoError(t, p.Connect(context.Background()))
		defer p.Close()
		var m ownership.Manifest
		require.NoError(t, json.Unmarshal([]byte(os.Getenv("CORE_OWNED_REGISTER_MANIFEST")), &m))
		ready := os.Getenv("CORE_OWNED_REGISTER_READY")
		require.NoError(t, os.WriteFile(ready, []byte("ready"), 0600))
		require.Eventually(t, func() bool { _, e := os.Stat(os.Getenv("CORE_OWNED_REGISTER_START")); return e == nil }, 5*time.Second, time.Millisecond)
		for i := 0; i < 16; i++ {
			require.NoError(t, p.RegisterOwnershipKey(context.Background(), m, "same-key"))
		}
		return
	}
	p, store, m := dynamicFixture(t)
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	root := t.TempDir()
	start := filepath.Join(root, "start")
	var commands []*exec.Cmd
	var outputs []*bytes.Buffer
	for _, id := range []string{"one", "two"} {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRedisOwnershipDirectoryRegistrationProcesses$")
		cmd.Env = append(os.Environ(), "CORE_OWNED_REGISTER_PREFIX="+p.prefix, "CORE_OWNED_REGISTER_MANIFEST="+string(raw), "CORE_OWNED_REGISTER_READY="+filepath.Join(root, id), "CORE_OWNED_REGISTER_START="+start)
		out := &bytes.Buffer{}
		cmd.Stdout = out
		cmd.Stderr = out
		require.NoError(t, cmd.Start())
		commands = append(commands, cmd)
		outputs = append(outputs, out)
	}
	require.NotEqual(t, commands[0].Process.Pid, commands[1].Process.Pid)
	require.Eventually(t, func() bool {
		_, a := os.Stat(filepath.Join(root, "one"))
		_, b := os.Stat(filepath.Join(root, "two"))
		return a == nil && b == nil
	}, 5*time.Second, time.Millisecond)
	require.NoError(t, os.WriteFile(start, []byte("go"), 0600))
	for i, cmd := range commands {
		require.NoError(t, cmd.Wait(), outputs[i].String())
	}
	page, err := store.(ownership.KeyRegistry).ListKeys(context.Background(), m, 0, 2)
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.Equal(t, []string{"same-key"}, page.Keys)
	t.Logf("两个独立注册PID=%d/%d，各16次并发幂等登记，实际目录总量=1", commands[0].Process.Pid, commands[1].Process.Pid)
}

func TestRedisOwnershipDirectoryDiscoveryInterruptionAndRecovery(t *testing.T) {
	p, store, m := dynamicFixture(t)
	ctx := context.Background()
	require.NoError(t, p.RegisterOwnershipKey(ctx, m, "A"))
	registry := store.(ownership.KeyRegistry)
	before, err := registry.ListKeys(ctx, m, 0, 2)
	require.NoError(t, err)
	// 本包使用独占测试Redis；真实Broker暂停响应，不清数据、不替换为Fake。
	require.NoError(t, p.client.Do(ctx, "CLIENT", "PAUSE", 200, "ALL").Err())
	probe, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	_, err = registry.ListKeys(probe, m, 0, 2)
	stop()
	require.Error(t, err, "目录读取响应中断不能伪造成功或空目录")
	require.Eventually(t, func() bool { _, e := registry.ListKeys(ctx, m, 0, 2); return e == nil }, time.Second, time.Millisecond)
	require.NoError(t, p.RegisterOwnershipKey(ctx, m, "B"))
	after, err := registry.ListKeys(ctx, m, 0, 2)
	require.NoError(t, err)
	require.Equal(t, 2, after.Total)
	require.Equal(t, before.Keys[0], after.Keys[0])
	require.Equal(t, "B", after.Keys[1])
}

func TestRedisOwnershipDirectoryTCPConnectionLossPreservesState(t *testing.T) {
	p, store, m := dynamicFixture(t)
	ctx := context.Background()
	require.NoError(t, p.RegisterOwnershipKey(ctx, m, "A"))
	c, err := store.Acquire(ctx, ownershipCandidate(m, "A", "owner"), m.Timing.Lease)
	require.NoError(t, err)
	require.NoError(t, store.Ready(ctx, c))
	oldID, err := p.client.ClientID(ctx).Result()
	require.NoError(t, err)
	admin := redis.NewClient(&redis.Options{Addr: os.Getenv("CORE_TEST_REDIS_ADDR"), PoolSize: 1})
	defer admin.Close()
	n, err := admin.ClientKillByFilter(ctx, "ID", strconv.FormatInt(oldID, 10)).Result()
	require.NoError(t, err)
	require.EqualValues(t, 1, n, "确实关闭本专项Provider的真实TCP连接")
	require.NoError(t, p.RegisterOwnershipKey(ctx, m, "B"))
	page, err := store.(ownership.KeyRegistry).ListKeys(ctx, m, 0, 2)
	require.NoError(t, err)
	require.Equal(t, []string{"A", "B"}, page.Keys)
	require.NoError(t, store.Renew(ctx, c, m.Timing.Lease))
	current, err := store.Resolve(ctx, m.Scope, "A")
	require.NoError(t, err)
	require.True(t, current.Same(c))
}
