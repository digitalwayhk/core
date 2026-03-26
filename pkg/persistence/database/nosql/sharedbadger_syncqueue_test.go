package nosql

import (
	"testing"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/digitalwayhk/core/pkg/json"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/stretchr/testify/require"
)

func newTestFundItem(userID, market string, balance float64) *testFund {
	return &testFund{
		Model:   entity.NewModel(),
		UserID:  userID,
		Market:  market,
		Balance: balance,
	}
}

// TestSyncQueue_SetCreatesSyncQueueEntry 验证 Set 写入时同步队列索引条目随数据原子创建
func TestSyncQueue_SetCreatesSyncQueueEntry(t *testing.T) {
	dir := t.TempDir()
	config := newTestConfig(dir)
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// 设置同步标志（不需要真正的远端 DB）
	db.syncLock.Lock()
	db.syncDB = true
	db.syncLock.Unlock()

	item := newTestFundItem("sq_user1", "BTC", 100)
	err = db.Set(item, 0)
	require.NoError(t, err)

	// 检查同步队列条目是否存在
	dataKey := db.generateKey(item)
	sqKey := db.syncQueueKey(dataKey)

	err = db.manager.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get([]byte(sqKey))
		return err
	})
	require.NoError(t, err, "Set 后应存在同步队列索引条目")

	// 验证 GetPendingSyncCount 通过队列索引统计
	count, err := db.GetPendingSyncCount()
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

// TestSyncQueue_BatchInsertCreatesSyncQueueEntries 验证 BatchInsert 批量创建队列条目
func TestSyncQueue_BatchInsertCreatesSyncQueueEntries(t *testing.T) {
	dir := t.TempDir()
	config := newTestConfig(dir)
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	db.syncLock.Lock()
	db.syncDB = true
	db.syncLock.Unlock()

	items := make([]*testFund, 5)
	for i := range items {
		items[i] = newTestFundItem("sq_batch", []string{"BTC", "ETH", "USDT", "SOL", "DOT"}[i], float64(i+1))
	}

	err = db.BatchInsert(items)
	require.NoError(t, err)

	count, err := db.GetPendingSyncCount()
	require.NoError(t, err)
	require.Equal(t, 5, count)
}

// TestSyncQueue_DeleteCreatesSyncQueueEntry 验证 Delete 软删除时创建队列条目
func TestSyncQueue_DeleteCreatesSyncQueueEntry(t *testing.T) {
	dir := t.TempDir()
	config := newTestConfig(dir)
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	db.syncLock.Lock()
	db.syncDB = true
	db.syncLock.Unlock()

	// 先写入一条已同步的数据
	item := newTestFundItem("sq_del", "BTC", 100)
	key := db.generateKey(item)
	wrapper := &SyncQueueItem[testFund]{
		Key:       key,
		Item:      item,
		Op:        OpInsert,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		IsSynced:  true, // 已同步
	}
	err = db.manager.db.Update(func(txn *badger.Txn) error {
		data, _ := json.Marshal(wrapper)
		return txn.Set([]byte(key), data)
	})
	require.NoError(t, err)

	// 此时队列应为空
	count, err := db.GetPendingSyncCount()
	require.NoError(t, err)
	require.Equal(t, 0, count)

	// 执行 Delete → 软删除，应创建队列条目
	err = db.DeleteByItem(item)
	require.NoError(t, err)

	count, err = db.GetPendingSyncCount()
	require.NoError(t, err)
	require.Equal(t, 1, count, "Delete 后应有 1 条待同步的删除操作")
}

// TestSyncQueue_BatchLoadNoQueueEntry 验证 BatchLoad 不创建队列条目
func TestSyncQueue_BatchLoadNoQueueEntry(t *testing.T) {
	dir := t.TempDir()
	config := newTestConfig(dir)
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	db.syncLock.Lock()
	db.syncDB = true
	db.syncLock.Unlock()

	items := []*testFund{
		newTestFundItem("sq_load", "BTC", 100),
		newTestFundItem("sq_load", "ETH", 200),
	}
	err = db.BatchLoad(items...)
	require.NoError(t, err)

	// BatchLoad 标记 IsSynced=true，不应创建队列条目
	count, err := db.GetPendingSyncCount()
	require.NoError(t, err)
	require.Equal(t, 0, count, "BatchLoad 不应产生待同步条目")
}

// TestSyncQueue_GetUnsyncedBatchUsesIndex 验证 getUnsyncedBatch 优先走队列索引
func TestSyncQueue_GetUnsyncedBatchUsesIndex(t *testing.T) {
	dir := t.TempDir()
	config := newTestConfig(dir)
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	db.syncLock.Lock()
	db.syncDB = true
	db.syncLock.Unlock()

	// 写入 3 条未同步 + 2 条已同步（BatchLoad）
	for i := 0; i < 3; i++ {
		item := newTestFundItem("sq_idx", []string{"BTC", "ETH", "SOL"}[i], float64(i))
		require.NoError(t, db.Set(item, 0))
	}
	loadItems := []*testFund{
		newTestFundItem("sq_idx", "USDT", 100),
		newTestFundItem("sq_idx", "DOT", 200),
	}
	require.NoError(t, db.BatchLoad(loadItems...))

	// getUnsyncedBatch 应只返回 3 条
	unsyncedItems, err := db.getUnsyncedBatch(100)
	require.NoError(t, err)
	require.Equal(t, 3, len(unsyncedItems), "应只返回通过 Set 写入的 3 条未同步数据")
}

// TestSyncQueue_RebuildOnUpgrade 验证启动时为旧数据补建队列索引
func TestSyncQueue_RebuildOnUpgrade(t *testing.T) {
	dir := t.TempDir()
	config := newTestConfig(dir)
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// 模拟旧版本数据：直接写入 IsSynced=false 的条目，不写队列索引
	for i := 0; i < 5; i++ {
		item := newTestFundItem("sq_old", []string{"BTC", "ETH", "SOL", "USDT", "DOT"}[i], float64(i))
		key := db.generateKey(item)
		wrapper := &SyncQueueItem[testFund]{
			Key:       key,
			Item:      item,
			Op:        OpInsert,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			IsSynced:  false,
		}
		err := db.manager.db.Update(func(txn *badger.Txn) error {
			data, _ := json.Marshal(wrapper)
			return txn.Set([]byte(key), data)
		})
		require.NoError(t, err)
	}

	// 此时队列索引为空
	count, err := db.GetPendingSyncCount()
	require.NoError(t, err)
	require.Equal(t, 0, count, "旧数据无队列索引，计数应为 0")

	// 执行重建
	rebuilt, err := db.rebuildSyncQueue()
	require.NoError(t, err)
	require.Equal(t, 5, rebuilt, "应补建 5 条队列索引")

	// 重建后可以正常统计
	count, err = db.GetPendingSyncCount()
	require.NoError(t, err)
	require.Equal(t, 5, count, "重建后应统计到 5 条待同步数据")

	// getUnsyncedBatch 也应正常工作
	items, err := db.getUnsyncedBatch(100)
	require.NoError(t, err)
	require.Equal(t, 5, len(items))
}

// TestSyncQueue_RebuildIdempotent 验证重复执行 rebuildSyncQueue 幂等
func TestSyncQueue_RebuildIdempotent(t *testing.T) {
	dir := t.TempDir()
	config := newTestConfig(dir)
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	db.syncLock.Lock()
	db.syncDB = true
	db.syncLock.Unlock()

	// 通过正常路径写入 3 条（已有队列索引）
	for i := 0; i < 3; i++ {
		item := newTestFundItem("sq_idem", []string{"BTC", "ETH", "SOL"}[i], float64(i))
		require.NoError(t, db.Set(item, 0))
	}

	// 第一次 rebuild 不应补建
	rebuilt, err := db.rebuildSyncQueue()
	require.NoError(t, err)
	require.Equal(t, 0, rebuilt, "已有索引的数据不应重复补建")

	// 计数不变
	count, err := db.GetPendingSyncCount()
	require.NoError(t, err)
	require.Equal(t, 3, count)
}

// TestSyncQueue_CleanupAfterSync 验证 batchUpdateSyncedStatus 后队列条目被清除
func TestSyncQueue_CleanupAfterSync(t *testing.T) {
	dir := t.TempDir()
	config := newTestConfig(dir)
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	db.syncLock.Lock()
	db.syncDB = true
	db.syncLock.Unlock()

	items := make([]*testFund, 3)
	keys := make([]string, 3)
	snapshotTimes := make(map[string]time.Time)

	for i := range items {
		items[i] = newTestFundItem("sq_clean", []string{"BTC", "ETH", "SOL"}[i], float64(i))
		require.NoError(t, db.Set(items[i], 0))
		keys[i] = db.generateKey(items[i])
	}

	// 获取快照时间
	for _, key := range keys {
		wrapper, err := db.getWrapper(key)
		require.NoError(t, err)
		snapshotTimes[key] = wrapper.UpdatedAt
	}

	count, err := db.GetPendingSyncCount()
	require.NoError(t, err)
	require.Equal(t, 3, count)

	// 模拟同步成功 → batchUpdateSyncedStatus
	err = db.batchUpdateSyncedStatus(keys, snapshotTimes)
	require.NoError(t, err)

	// 队列应被清空
	count, err = db.GetPendingSyncCount()
	require.NoError(t, err)
	require.Equal(t, 0, count, "同步成功后队列应为空")

	// 验证同步队列 key 已被物理删除
	for _, key := range keys {
		sqKey := db.syncQueueKey(key)
		err := db.manager.db.View(func(txn *badger.Txn) error {
			_, err := txn.Get([]byte(sqKey))
			return err
		})
		require.ErrorIs(t, err, badger.ErrKeyNotFound, "同步成功后队列索引应被删除")
	}
}

// TestSyncQueue_CASMismatchKeepsQueueEntry 验证 CAS 不匹配时队列条目保留
func TestSyncQueue_CASMismatchKeepsQueueEntry(t *testing.T) {
	dir := t.TempDir()
	config := newTestConfig(dir)
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	db.syncLock.Lock()
	db.syncDB = true
	db.syncLock.Unlock()

	item := newTestFundItem("sq_cas", "BTC", 100)
	require.NoError(t, db.Set(item, 0))

	key := db.generateKey(item)
	wrapper, err := db.getWrapper(key)
	require.NoError(t, err)
	oldSnapshot := map[string]time.Time{key: wrapper.UpdatedAt}

	// 模拟并发写入 → UpdatedAt 改变
	time.Sleep(2 * time.Millisecond)
	item2 := newTestFundItem("sq_cas", "BTC", 999)
	require.NoError(t, db.Set(item2, 0))

	// 使用旧快照调用 batchUpdateSyncedStatus → CAS 应不匹配
	err = db.batchUpdateSyncedStatus([]string{key}, oldSnapshot)
	require.NoError(t, err)

	// 队列条目应仍然存在
	count, err := db.GetPendingSyncCount()
	require.NoError(t, err)
	require.Equal(t, 1, count, "CAS 不匹配时队列条目应保留")

	// 数据仍为未同步
	wrapper2, err := db.getWrapper(key)
	require.NoError(t, err)
	require.False(t, wrapper2.IsSynced)
}

// TestSyncQueue_OrphanCleanup 验证 getUnsyncedBatch 自动清理孤儿队列条目
func TestSyncQueue_OrphanCleanup(t *testing.T) {
	dir := t.TempDir()
	config := newTestConfig(dir)
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// 手动写入一条孤儿队列条目（没有对应的数据 key）
	orphanDataKey := db.prefix + "nonexistent_key"
	orphanSQKey := db.syncQueueKey(orphanDataKey)
	err = db.manager.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(orphanSQKey), nil)
	})
	require.NoError(t, err)

	// getUnsyncedBatch 触发清理
	items, err := db.getUnsyncedBatch(100)
	require.NoError(t, err)
	require.Equal(t, 0, len(items), "孤儿条目不应作为有效数据返回")

	// 等待异步清理完成
	time.Sleep(50 * time.Millisecond)

	// 验证孤儿队列条目已被清除
	err = db.manager.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get([]byte(orphanSQKey))
		return err
	})
	require.ErrorIs(t, err, badger.ErrKeyNotFound, "孤儿队列条目应被清理")
}
