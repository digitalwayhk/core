// 本文件验证副本身份持久化、进程锁及损坏状态拒绝。
package identity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPersistentInstanceIdentityRestart 验证恢复同一身份且启动标识独立。
func TestPersistentInstanceIdentityRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	require.NoError(t, Create(path, "orders", "orders-a"))
	f, err := Open(path, "orders", "orders-a")
	require.NoError(t, err)
	_, err = Open(path, "orders", "orders-a")
	require.Error(t, err)
	f.State.Assigned = true
	f.State.GeneratorState = GeneratorState{Assigned: true, Namespace: "test", Authority: "local:test", AuthorityEpoch: "epoch-a", Encoding: "legacy-decimal-v1", WorkerID: 10, DataCenterID: 1, MachineID: 0}
	require.NoError(t, f.Save())
	// 替换身份文件后依然持有独立锁，第二个进程不能绕过。
	_, err = Open(path, "orders", "orders-a")
	require.Error(t, err)
	require.NoError(t, f.Close())
	g, err := Open(path, "orders", "orders-a")
	require.NoError(t, err)
	defer g.Close()
	require.Equal(t, "orders-a", g.State.InstanceID)
	require.True(t, g.State.Assigned)
	require.Equal(t, uint16(10), g.State.WorkerID)
}

// TestIdentityFileRejectsMissingOrCorrupt 验证普通启动不生成替代身份。
func TestIdentityFileRejectsMissingOrCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	_, err := Open(path, "orders", "")
	require.Error(t, err)
	require.NoFileExists(t, path)
	require.NoError(t, Create(path, "orders", "orders-a"))
	require.Error(t, Create(path, "orders", "orders-b"))
	_, err = Open(path, "other", "orders-a")
	require.Error(t, err)
	require.NoError(t, os.WriteFile(path, []byte("{}"), 0600))
	_, err = Open(path, "orders", "orders-a")
	require.Error(t, err)
}

// TestIdentityRejectsInvalidGeneratorState 验证已分配状态不能省略布局及编号来源。
func TestIdentityRejectsInvalidGeneratorState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	require.NoError(t, Create(path, "orders", "a"))
	f, err := Open(path, "orders", "a")
	require.NoError(t, err)
	f.State.Assigned = true
	f.State.WorkerID = 64
	require.NoError(t, f.Save())
	require.NoError(t, f.Close())
	_, err = Open(path, "orders", "a")
	require.Error(t, err)
}
