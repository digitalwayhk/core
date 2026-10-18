// 本文件在暂停写入的检查点独立核对缓存计数、同步索引及未同步事实，保留合法已同步缓存。
package nosql

import (
	"bytes"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/digitalwayhk/core/pkg/json"
	"github.com/stretchr/testify/require"
)

// TestPendingSnapshotMatchesFactsAndTombstones 验证待同步值、删除墓碑和已同步缓存三种状态。
func TestPendingSnapshotMatchesFactsAndTombstones(t *testing.T) {
	db := newTestDBLocalWithConfig(t, newTestConfig(t.TempDir()))
	db.syncLock.Lock()
	db.syncDB = true
	db.syncLock.Unlock()
	for _, name := range []string{"pending", "deleted", "synced-cache"} {
		require.NoError(t, db.Set(newFund(name, "spot", 1), 0))
	}
	require.NoError(t, db.DeleteByItem(newFund("deleted", "spot", 1)))
	key := db.generateKey(newFund("synced-cache", "spot", 1))
	wrapper, err := db.getWrapper(key)
	require.NoError(t, err)
	_, err = db.confirmSyncSuccess([]string{key}, map[string]time.Time{key: wrapper.UpdatedAt}, nil)
	require.NoError(t, err)
	cached := db.GetCachedPendingSyncCount()
	facts, pending, tombstones, queues := 0, 0, 0, 0
	require.NoError(t, db.manager.db.View(func(tx *badger.Txn) error {
		it := tx.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			key := it.Item().KeyCopy(nil)
			if bytes.HasPrefix(key, []byte(db.syncQueuePrefix())) {
				queues++
				_, err := tx.Get([]byte(db.dataKeyFromSyncQueueKey(string(key))))
				if err != nil {
					return err
				}
			}
			if !bytes.HasPrefix(key, []byte(db.prefix)) {
				continue
			}
			facts++
			var item SyncQueueItem[testFund]
			if err := it.Item().Value(func(v []byte) error { return json.Unmarshal(v, &item) }); err != nil {
				return err
			}
			if item.IsSynced {
				continue
			}
			pending++
			if item.IsDeleted {
				tombstones++
			}
			if _, err := tx.Get([]byte(db.syncQueueKey(string(key)))); err != nil {
				return err
			}
		}
		return nil
	}))
	require.Equal(t, 3, facts)
	require.Equal(t, 1, tombstones)
	require.Equal(t, 2, pending)
	require.Equal(t, pending, queues)
	require.Equal(t, pending, cached)
}
