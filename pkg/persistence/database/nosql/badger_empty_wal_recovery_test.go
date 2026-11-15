// 本文件在真实可靠卷中固定零字节memtable残留；不删业务记录，不以重试掩盖第一次打开失败。
package nosql

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func emptyWALFixtureRoot(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("空WAL恢复当前仅验收Darwin/Linux，其它平台保持原Badger行为")
	}
	if root := os.Getenv("CORE_EMPTY_WAL_EVIDENCE"); root != "" {
		path, err := os.MkdirTemp(root, "fixture-")
		require.NoError(t, err)
		t.Logf("保留独立测试卷：%s", path)
		return path
	}
	return t.TempDir()
}

// TestReliableStoreReopensZeroByteMemtable 验证首次打开恢复全部pending及原预算，不依赖下一笔Save。
func TestReliableStoreReopensZeroByteMemtable(t *testing.T) {
	root := emptyWALFixtureRoot(t)
	target := &cleanupTestTarget{}
	s := openIndependentStore(t, root, false, 2, target)
	require.NoError(t, s.Save(context.Background(), groupRecord(t, "original", "group-A", 1, false, &cleanupTestProbe{})))
	path := s.config.BasePath
	var pending *PendingSyncError
	require.ErrorAs(t, s.Close(context.Background()), &pending)
	require.Equal(t, 1, pending.Count)
	require.NoError(t, CloseSharedManager(path))
	// 对应上游Delete已Truncate(0)、尚未Remove的真实磁盘布局；只有空技术文件，没有截断原件。
	empty := filepath.Join(path, "00001.mem")
	f, err := os.OpenFile(empty, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	require.NoError(t, err)
	require.NoError(t, f.Sync())
	require.NoError(t, f.Close())
	fi, err := os.Stat(empty)
	require.NoError(t, err)
	require.Zero(t, fi.Size())
	reopened := openIndependentStore(t, root, false, 2, target)
	v, err := reopened.GetLocal(context.Background(), "original")
	require.NoError(t, err)
	require.Equal(t, 1, v.Revision)
	require.Equal(t, 1, reopened.Metrics().Pending)
	records, _, err := reopened.ListWriteBehindFailures("", 16)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, WriteBehindReady, records[0].State)
	require.Zero(t, records[0].Attempts)
}
