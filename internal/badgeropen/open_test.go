// 本文件验证只回收空技术WAL，目录互斥、非空数据和只读/绕锁边界不被放宽。
package badgeropen

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dgraph-io/badger/v4"
	"github.com/stretchr/testify/require"
)

func requireRecoveryPlatform(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("本回归仅在已验证的Darwin/Linux运行")
	}
}

// TestRecoverOnlyEmptyMemtable 验证不会把非空/未知命名/链接文件当空WAL清理。
func TestRecoverOnlyEmptyMemtable(t *testing.T) {
	requireRecoveryPlatform(t)
	root := t.TempDir()
	for name, data := range map[string][]byte{"00001.mem": {}, "00002.mem": []byte("nonempty-original"), "not-a-number.mem": {}, "000003.mem": {}, "ordinary.file": {}} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), data, 0600))
	}
	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.WriteFile(outside, []byte("external-original"), 0600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "00004.mem")))
	n, err := recoverEmptyMemtables(root)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	_, err = os.Lstat(filepath.Join(root, "00001.mem"))
	require.True(t, os.IsNotExist(err))
	for name, want := range map[string][]byte{"00002.mem": []byte("nonempty-original"), "not-a-number.mem": {}, "000003.mem": {}, "ordinary.file": {}, "00004.mem": []byte("external-original")} {
		data, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err)
		require.True(t, bytes.Equal(want, data))
	}
	n, err = recoverEmptyMemtables(root)
	require.NoError(t, err)
	require.Zero(t, n)
}

// TestActiveBadgerOwnerBlocksRecovery 验证使用真实Badger目录锁，无权清理仍有owner的技术文件。
func TestActiveBadgerOwnerBlocksRecovery(t *testing.T) {
	requireRecoveryPlatform(t)
	root := t.TempDir()
	db, err := badger.Open(badger.DefaultOptions(root).WithLogger(nil))
	require.NoError(t, err)
	defer db.Close()
	wal := filepath.Join(root, "90000.mem")
	require.NoError(t, os.WriteFile(wal, nil, 0600))
	n, err := recoverEmptyMemtables(root)
	require.Error(t, err)
	require.Zero(t, n)
	info, err := os.Stat(wal)
	require.NoError(t, err)
	require.Zero(t, info.Size())
	require.NoError(t, os.Remove(wal))
}

// TestReadOnlyAndBypassDoNotPreprocess 验证只读或显式绕锁不触发本层恢复；只读文件保持零字节，绕锁沿原Badger行为。
func TestReadOnlyAndBypassDoNotPreprocess(t *testing.T) {
	requireRecoveryPlatform(t)
	for _, kind := range []string{"readonly", "bypass"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			db, err := badger.Open(badger.DefaultOptions(root).WithLogger(nil))
			require.NoError(t, err)
			require.NoError(t, db.Close())
			wal := filepath.Join(root, "00001.mem")
			require.NoError(t, os.WriteFile(wal, nil, 0600))
			opts := badger.DefaultOptions(root).WithLogger(nil)
			if kind == "readonly" {
				opts.ReadOnly = true
			} else {
				opts.BypassLockGuard = true
			}
			// bypass交给原Badger后可能扩容空文件；本层不声称支持不安全绕锁恢复。
			db, err = Open(opts)
			require.Error(t, err)
			require.Nil(t, db)
			if kind == "readonly" {
				info, e := os.Stat(wal)
				require.NoError(t, e)
				require.Zero(t, info.Size())
			}
		})
	}
}
