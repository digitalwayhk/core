// 本文件验证只读核对器发现双向索引损坏，且不改变被检查目录。
package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/dgraph-io/badger/v4"
	"github.com/stretchr/testify/require"
)

// TestInspectionInvariants 验证原生队列及Core缓存、pending和删除墓碑的区别。
func TestInspectionInvariants(t *testing.T) {
	p := fmt.Sprintf("p:%x", sha256.Sum256([]byte("7:test")))
	q := "q:00000000000000000001"
	fact := `{"orderID":1,"userID":7,"requestID":"test","requestFingerprint":"fp"}`
	for _, tc := range []struct {
		name, format            string
		data                    map[string]string
		facts, queues, unsynced int64
		broken                  bool
	}{
		{"native-good", "native", map[string]string{p: fact, q: p}, 1, 1, 1, false},
		{"native-p-without-q", "native", map[string]string{p: fact}, 1, 0, 1, true},
		{"native-q-without-p", "native", map[string]string{q: p}, 0, 1, 0, true},
		{"native-wrong-id", "native", map[string]string{p: fact, "q:00000000000000000002": p}, 1, 1, 1, true},
		{"native-wrong-hash", "native", map[string]string{"p:wrong": fact, q: "p:wrong"}, 1, 1, 1, true},
		{"native-malformed", "native", map[string]string{p: `{`, q: p}, 1, 1, 1, true},
		{"core-synced-cache", "core", map[string]string{"Order:1": `{"key":"Order:1","item":{},"is_synced":true}`}, 1, 0, 0, false},
		{"core-pending", "core", map[string]string{"Order:1": `{"key":"Order:1","item":{},"is_synced":false}`, "\x00sq:Order:1": ""}, 1, 1, 1, false},
		{"core-delete", "core", map[string]string{"Order:1": `{"key":"Order:1","is_synced":false,"is_deleted":true}`, "\x00sq:Order:1": ""}, 1, 1, 1, false},
		{"core-missing-index", "core", map[string]string{"Order:1": `{"key":"Order:1","item":{},"is_synced":false}`}, 1, 0, 1, true},
		{"core-dangling-index", "core", map[string]string{"\x00sq:Order:1": ""}, 0, 1, 0, true},
		{"core-missing-state", "core", map[string]string{"Order:1": `{"key":"Order:1","item":{}}`}, 1, 0, 0, true},
		{"core-synced-with-index", "core", map[string]string{"Order:1": `{"key":"Order:1","item":{},"is_synced":true}`, "\x00sq:Order:1": ""}, 1, 1, 0, true},
		{"core-wrong-key", "core", map[string]string{"Order:1": `{"key":"Order:2","item":{},"is_synced":true}`}, 1, 0, 0, true},
		{"core-missing-item", "core", map[string]string{"Order:1": `{"key":"Order:1","is_synced":false}`}, 1, 0, 0, true},
		{"core-scalar-item", "core", map[string]string{"Order:1": `{"key":"Order:1","item":7,"is_synced":true}`}, 1, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := t.TempDir()
			db, err := badger.Open(badger.DefaultOptions(path).WithLogger(nil).
				WithSyncWrites(true).WithMemTableSize(4 << 20).WithValueLogFileSize(1 << 20).
				WithValueThreshold(1 << 16).WithBlockCacheSize(1 << 20))
			require.NoError(t, err)
			require.NoError(t, db.Update(func(tx *badger.Txn) error {
				for k, v := range tc.data {
					if err := tx.Set([]byte(k), []byte(v)); err != nil {
						return err
					}
				}
				return nil
			}))
			require.NoError(t, db.Close())
			before := directoryHashes(t, path)
			report, err := inspectPending(path, tc.format, "Order:")
			if tc.broken {
				require.Error(t, err)
				require.NotEmpty(t, report.Issues)
			} else {
				require.NoError(t, err)
				require.Empty(t, report.Issues)
			}
			require.Equal(t, tc.facts, report.Facts)
			require.Equal(t, tc.queues, report.Queues)
			require.Equal(t, tc.unsynced, report.Unsynced)
			require.Equal(t, before, directoryHashes(t, path))
		})
	}
}

// TestInspectionRejectsMissingDirectory 验证错误路径不会被自动创建。
func TestInspectionRejectsMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")
	_, err := inspectPending(path, "native", "")
	require.Error(t, err)
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err))
}

func directoryHashes(t *testing.T, path string) map[string]string {
	t.Helper()
	result := map[string]string{}
	require.NoError(t, filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		name, err := filepath.Rel(path, p)
		if err != nil {
			return err
		}
		result[name] = fmt.Sprintf("%x", sha256.Sum256(data))
		return nil
	}))
	return result
}
