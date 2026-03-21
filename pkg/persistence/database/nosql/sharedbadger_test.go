package nosql

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/digitalwayhk/core/pkg/json"
	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/types"
)

// ============================================================
// 测试用 Model（实现 types.IModel + types.IRowCode）
// ============================================================

type testFund struct {
	*entity.Model
	UserID  string  `json:"user_id"`
	Market  string  `json:"market"`
	Balance float64 `json:"balance"`
	Status  int     `json:"status"`
}

func NewtestFund() *testFund {
	return &testFund{Model: entity.NewModel()}
}
func (f *testFund) NewModel() {
	if f.Model == nil {
		f.Model = entity.NewModel()
	}
}
func (f *testFund) GetHash() string {
	return f.UserID + ":" + f.Market
}
func (f *testFund) IsSyncAfterDelete() bool {
	return f.Status == 2
}
func (own *testFund) GetLocalDBName() string {
	return "test_funds"
}
func (own *testFund) GetRemoteDBName() string {
	return "test_funds_remote" + fmt.Sprintf("%d", own.Status)
}

// newFund 快速构造测试用 fund
func newFund(userID, market string, balance float64) *testFund {
	return &testFund{
		Model:   entity.NewModel(),
		UserID:  userID,
		Market:  market,
		Balance: balance,
	}
}

// ============================================================
// 测试基础设施
// ============================================================

// newTestDB 创建带 MySQL 同步的 PrefixedBadgerDB（MySQL 需已运行）。
func newTestDB(t *testing.T) *PrefixedBadgerDB[testFund] {
	t.Helper()
	db, err := NewSharedBadgerDB[testFund](t.TempDir())
	if err != nil {
		t.Fatalf("NewSharedBadgerDB 失败: %v", err)
	}
	cfg := &oltp.Config{
		Host:     "127.0.0.1",
		Port:     3306,
		Username: "root",
		Password: "your_password",
	}
	action := oltp.NewMySQL(cfg)
	list := entity.NewModelList[testFund](action)
	db.SetSyncDB(list)
	t.Cleanup(func() { db.Close() })
	return db
}

// newTestDBLocal 创建不带 MySQL 同步的纯本地 BadgerDB（不依赖外部服务）。
func newTestDBLocal(t *testing.T) *PrefixedBadgerDB[testFund] {
	t.Helper()
	db, err := NewSharedBadgerDB[testFund](t.TempDir())
	if err != nil {
		t.Fatalf("NewSharedBadgerDB 失败: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// triggerSync 在测试中手动触发一次同步队列处理。
func triggerSync(t *testing.T, db *PrefixedBadgerDB[testFund]) {
	t.Helper()
	if err := db.processSyncQueue(); err != nil {
		t.Fatalf("processSyncQueue 失败: %v", err)
	}
}

// cleanupMySQL 删除 MySQL 中指定 userID 的所有测试数据，用于 setUp/tearDown。
func cleanupMySQL(t *testing.T, db *PrefixedBadgerDB[testFund], userID string) {
	t.Helper()
	if db.syncList == nil {
		return
	}
	items, err := db.syncList.SearchWhere("user_id", userID)
	if err != nil || len(items) == 0 {
		return
	}
	si := db.syncList.GetSearchItem()
	action := db.syncList.GetDBAdapter(si)
	if action == nil {
		return
	}
	for _, item := range items {
		_ = action.Delete(item)
	}
}

// findInMySQL 按 GetHash 在 MySQL 中查找记录，未找到返回 nil。
func findInMySQL(t *testing.T, db *PrefixedBadgerDB[testFund], item *testFund) *testFund {
	t.Helper()
	if db.syncList == nil {
		t.Fatal("syncList 未初始化，无法查询 MySQL")
	}
	got, _ := db.syncList.SearchHash(item.GetHash())
	return got
}

// isSyncedInBadger 读取 BadgerDB 中 wrapper 的 IsSynced 字段。
func isSyncedInBadger(t *testing.T, db *PrefixedBadgerDB[testFund], item *testFund) bool {
	t.Helper()
	w := getWrapperFromBadger(t, db, item)
	return w != nil && w.IsSynced
}

// keyExistsInBadger 检查 key 是否存在于 BadgerDB（含软删除条目）。
func keyExistsInBadger(t *testing.T, db *PrefixedBadgerDB[testFund], item *testFund) bool {
	t.Helper()
	key := db.prefix + item.GetHash()
	err := db.manager.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get([]byte(key))
		return err
	})
	if err == badger.ErrKeyNotFound {
		return false
	}
	if err != nil {
		t.Fatalf("keyExistsInBadger 失败: %v", err)
	}
	return true
}

// getWrapperFromBadger 直接读取 BadgerDB 中的 wrapper（测试用状态检查）。
func getWrapperFromBadger(t *testing.T, db *PrefixedBadgerDB[testFund], item *testFund) *SyncQueueItem[testFund] {
	t.Helper()
	w, err := db.getWrapper(db.prefix + item.GetHash())
	if err == badger.ErrKeyNotFound {
		return nil
	}
	if err != nil {
		t.Fatalf("getWrapper 失败: %v", err)
	}
	return w
}

// writeRawWrapper 绕过业务逻辑，直接向 BadgerDB 写入指定同步状态的 wrapper（测试辅助）。
func writeRawWrapper(t *testing.T, db *PrefixedBadgerDB[testFund], item *testFund, synced bool) {
	t.Helper()
	now := time.Now()
	key := db.prefix + item.GetHash()
	wrapper := &SyncQueueItem[testFund]{
		Key:       key,
		Item:      item,
		Op:        OpInsert,
		CreatedAt: now,
		UpdatedAt: now,
		IsSynced:  synced,
		SyncedAt:  now,
	}
	err := db.manager.db.Update(func(txn *badger.Txn) error {
		data, marshalErr := json.Marshal(wrapper)
		if marshalErr != nil {
			return marshalErr
		}
		return txn.Set([]byte(key), data)
	})
	if err != nil {
		t.Fatalf("writeRawWrapper 失败 [%s]: %v", key, err)
	}
}

// ============================================================
// 一、纯本地 BadgerDB CRUD（不依赖 MySQL）
// ============================================================

// TestLocal_Set_Get 基础写读
func TestLocal_Set_Get(t *testing.T) {
	db := newTestDBLocal(t)
	item := newFund("local_u1", "BTC", 100.5)

	if err := db.Set(item, 0); err != nil {
		t.Fatalf("Set 失败: %v", err)
	}
	got, err := db.Get("local_u1:BTC")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.Balance != 100.5 || got.UserID != "local_u1" {
		t.Fatalf("数据不匹配: %+v", got)
	}
}

// TestLocal_Set_Update 更新已有 key 后 Op 变为 OpUpdate，Balance 被覆盖
func TestLocal_Set_Update(t *testing.T) {
	db := newTestDBLocal(t)
	item := newFund("local_u2", "ETH", 50.0)

	db.Set(item, 0)
	item.Balance = 99.0
	db.Set(item, 0)

	w := getWrapperFromBadger(t, db, item)
	if w == nil {
		t.Fatal("wrapper 不存在")
	}
	if w.Op != OpUpdate {
		t.Fatalf("期望 Op=OpUpdate，实际 %s", w.Op)
	}
	if w.Item.Balance != 99.0 {
		t.Fatalf("期望 Balance=99.0，实际 %v", w.Item.Balance)
	}
}

// TestLocal_Delete_GetReturnsNotFound 软删除后 Get 返回 ErrKeyNotFound
func TestLocal_Delete_GetReturnsNotFound(t *testing.T) {
	db := newTestDBLocal(t)
	item := newFund("local_u3", "SOL", 10.0)

	db.Set(item, 0)
	if err := db.DeleteByItem(item); err != nil {
		t.Fatalf("DeleteByItem 失败: %v", err)
	}
	if _, err := db.Get("local_u3:SOL"); err != badger.ErrKeyNotFound {
		t.Fatalf("软删除后 Get 应返回 ErrKeyNotFound，实际: %v", err)
	}
}

// TestLocal_Delete_PhysicalDeleteWhenNoSync 无 syncDB 时 DeleteByItem 物理删除 key（不产生软删除 wrapper）
func TestLocal_Delete_PhysicalDeleteWhenNoSync(t *testing.T) {
	db := newTestDBLocal(t) // syncDB=false
	item := newFund("local_u4", "USDT", 1.0)

	db.Set(item, 0)
	if err := db.DeleteByItem(item); err != nil {
		t.Fatalf("DeleteByItem 失败: %v", err)
	}

	// 无 syncDB 时物理删除，key 不再存在
	if keyExistsInBadger(t, db, item) {
		t.Error("无 syncDB 时 DeleteByItem 应物理删除 key")
	}
	// getWrapper 返回 nil（物理键已删除）
	if getWrapperFromBadger(t, db, item) != nil {
		t.Error("物理删除后 wrapper 应为 nil")
	}
}

// TestLocal_Scan_FiltersDeleted Scan 不返回软删除条目
func TestLocal_Scan_FiltersDeleted(t *testing.T) {
	db := newTestDBLocal(t)

	live := newFund("scan_u", "LIVE", 1.0)
	dead := newFund("scan_u", "DEAD", 2.0)
	db.Set(live, 0)
	db.Set(dead, 0)
	db.DeleteByItem(dead)

	results, err := db.Scan("scan_u:", 100)
	if err != nil {
		t.Fatalf("Scan 失败: %v", err)
	}
	if len(results) != 1 || results[0].Market != "LIVE" {
		t.Fatalf("期望 1 条 LIVE，实际 %d 条: %+v", len(results), results)
	}
}

// TestLocal_BatchInsert_LargeDataset 2500 条不触发 ErrTxnTooBig
func TestLocal_BatchInsert_LargeDataset(t *testing.T) {
	db := newTestDBLocal(t)
	const total = 2500
	items := make([]*testFund, total)
	for i := 0; i < total; i++ {
		items[i] = newFund("batch_u", fmt.Sprintf("M%04d", i), float64(i))
	}

	if err := db.BatchInsert(items); err != nil {
		t.Fatalf("BatchInsert %d 条失败: %v", total, err)
	}
	results, err := db.ScanAll()
	if err != nil {
		t.Fatalf("ScanAll 失败: %v", err)
	}
	if len(results) != total {
		t.Fatalf("期望 %d 条，实际 %d 条", total, len(results))
	}
}

// TestLocal_BatchLoad_AllMarkedAsSynced BatchLoad 写入的条目 IsSynced=true，PendingCount=0
func TestLocal_BatchLoad_AllMarkedAsSynced(t *testing.T) {
	db := newTestDBLocal(t)
	items := []*testFund{
		newFund("load_u", "A", 1),
		newFund("load_u", "B", 2),
		newFund("load_u", "C", 3),
	}

	if err := db.BatchLoad(items...); err != nil {
		t.Fatalf("BatchLoad 失败: %v", err)
	}
	for _, item := range items {
		if !isSyncedInBadger(t, db, item) {
			t.Errorf("BatchLoad 写入的 %s 应为 IsSynced=true", item.GetHash())
		}
	}
	count, _ := db.GetPendingSyncCount()
	if count != 0 {
		t.Fatalf("BatchLoad 后待同步计数应为 0，实际 %d", count)
	}
}

// TestLocal_PendingCount_IncreasesWithSet Set 写入后 PendingCount 正确增加
func TestLocal_PendingCount_IncreasesWithSet(t *testing.T) {
	db := newTestDB(t) // 需要启用 syncDB 才会计数

	for i := 0; i < 3; i++ {
		db.Set(newFund("cnt_u", fmt.Sprintf("M%d", i), float64(i)), 0)
	}
	count, err := db.GetPendingSyncCount()
	if err != nil {
		t.Fatalf("GetPendingSyncCount: %v", err)
	}
	if count != 3 {
		t.Fatalf("期望 3，实际 %d", count)
	}
}

// TestLocal_DeleteBySubPrefix_SyncedImmediate 已同步的立即删，未同步的保留
func TestLocal_DeleteBySubPrefix_SyncedImmediate(t *testing.T) {
	db := newTestDBLocal(t)

	writeRawWrapper(t, db, newFund("del_u", "A", 1), true)
	writeRawWrapper(t, db, newFund("del_u", "B", 2), true)
	writeRawWrapper(t, db, newFund("del_u", "C", 3), false)

	deleted, err := db.DeleteBySubPrefix("del_u:")
	if err != nil {
		t.Fatalf("DeleteBySubPrefix 失败: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("期望立即删除 2 条，实际 %d 条", deleted)
	}
	// 未同步的 C 仍存在
	if !keyExistsInBadger(t, db, newFund("del_u", "C", 3)) {
		t.Error("未同步的 C 不应被立即删除")
	}
}

// TestLocal_DeleteBySubPrefix_NoOtherUsersAffected 前缀删除不影响其他用户
func TestLocal_DeleteBySubPrefix_NoOtherUsersAffected(t *testing.T) {
	db := newTestDBLocal(t)

	writeRawWrapper(t, db, newFund("target_u", "X", 1), true)
	writeRawWrapper(t, db, newFund("safe_u", "X", 1), true)

	db.DeleteBySubPrefix("target_u:")

	if !keyExistsInBadger(t, db, newFund("safe_u", "X", 1)) {
		t.Error("safe_u 的数据不应被误删")
	}
}

// TestLocal_IsClosed_AfterClose 关闭后操作返回错误
func TestLocal_IsClosed_AfterClose(t *testing.T) {
	db, _ := NewSharedBadgerDB[testFund](t.TempDir())
	db.Close()

	if !db.IsClosed() {
		t.Fatal("Close 后 IsClosed() 应为 true")
	}
	if err := db.Set(newFund("x", "x", 0), 0); err == nil {
		t.Fatal("关闭后 Set 应返回错误")
	}
}

// TestLocal_Close_Idempotent 重复 Close 不 panic
func TestLocal_Close_Idempotent(t *testing.T) {
	db, _ := NewSharedBadgerDB[testFund](t.TempDir())
	db.Close()
	db.Close() // 不应 panic
}

// ============================================================
// 二、同步集成测试（需要 MySQL 连接）
// ============================================================

// TestSync_Insert_WritesToMySQL
// 流程：Set → PendingCount=1 → processSyncQueue → IsSynced=true → MySQL 存在
func TestSync_Insert_WritesToMySQL(t *testing.T) {
	const userID = "sync_ins_u"
	db := newTestDB(t)
	cleanupMySQL(t, db, userID)
	t.Cleanup(func() { cleanupMySQL(t, db, userID) })

	item := newFund(userID, "BTC", 500.0)

	if err := db.Set(item, 0); err != nil {
		t.Fatalf("Set 失败: %v", err)
	}

	// 同步前：IsSynced=false，PendingCount=1
	if isSyncedInBadger(t, db, item) {
		t.Error("同步前 IsSynced 应为 false")
	}
	if count, _ := db.GetPendingSyncCount(); count != 1 {
		t.Errorf("同步前 PendingCount 应为 1，实际 %d", count)
	}

	triggerSync(t, db)

	// 同步后：IsSynced=true，PendingCount=0
	if !isSyncedInBadger(t, db, item) {
		t.Error("同步后 IsSynced 应为 true")
	}
	if count, _ := db.GetPendingSyncCount(); count != 0 {
		t.Errorf("同步后 PendingCount 应为 0，实际 %d", count)
	}

	// MySQL 中存在该记录
	got := findInMySQL(t, db, item)
	if got == nil {
		t.Fatal("同步后 MySQL 中应存在该记录")
	}
	if got.Balance != 500.0 {
		t.Errorf("MySQL Balance 应为 500.0，实际 %v", got.Balance)
	}
}

// TestSync_Update_UpdatesMySQL
// 流程：Insert+Sync → 修改 Balance → Set → Sync → MySQL 记录已更新
func TestSync_Update_UpdatesMySQL(t *testing.T) {
	const userID = "sync_upd_u"
	db := newTestDB(t)
	cleanupMySQL(t, db, userID)
	t.Cleanup(func() { cleanupMySQL(t, db, userID) })

	item := newFund(userID, "ETH", 100.0)

	db.Set(item, 0)
	triggerSync(t, db)

	// 修改并再次同步
	item.Balance = 999.0
	db.Set(item, 0)

	if isSyncedInBadger(t, db, item) {
		t.Error("更新后同步前 IsSynced 应为 false")
	}

	triggerSync(t, db)

	if !isSyncedInBadger(t, db, item) {
		t.Error("再次同步后 IsSynced 应为 true")
	}

	got := findInMySQL(t, db, item)
	if got == nil {
		t.Fatal("MySQL 中应存在更新后的记录")
	}
	if got.Balance != 999.0 {
		t.Errorf("MySQL Balance 应为 999.0，实际 %v", got.Balance)
	}
}

// TestSync_SoftDelete_RemovesFromMySQL
// 流程：Insert+Sync → DeleteByItem(软删) → Sync → MySQL 无记录，BadgerDB 物理键删除
func TestSync_SoftDelete_RemovesFromMySQL(t *testing.T) {
	const userID = "sync_del_u"
	db := newTestDB(t)
	cleanupMySQL(t, db, userID)
	t.Cleanup(func() { cleanupMySQL(t, db, userID) })

	item := newFund(userID, "SOL", 200.0)

	// 插入并同步
	db.Set(item, 0)
	triggerSync(t, db)

	if findInMySQL(t, db, item) == nil {
		t.Fatal("前置条件失败：MySQL 中应有记录")
	}

	// 软删除
	if err := db.DeleteByItem(item); err != nil {
		t.Fatalf("DeleteByItem 失败: %v", err)
	}

	w := getWrapperFromBadger(t, db, item)
	if w == nil || !w.IsDeleted || w.Op != OpDelete {
		t.Error("软删除后 IsDeleted 应为 true，Op 应为 OpDelete")
	}
	if w.IsSynced {
		t.Error("软删除后 IsSynced 应为 false（待同步删除到 MySQL）")
	}

	// 触发同步（删除到 MySQL，然后物理删除本地 key）
	triggerSync(t, db)

	// BadgerDB 物理键已删除
	if keyExistsInBadger(t, db, item) {
		t.Error("同步后 BadgerDB 中软删除条目应被物理删除")
	}

	// MySQL 无记录
	if findInMySQL(t, db, item) != nil {
		t.Error("同步后 MySQL 中不应再有该记录")
	}
}

// TestSync_IsSyncAfterDelete_PhysicallyDeletedLocally
// Status=2 (IsSyncAfterDelete=true)：插入并同步后 MySQL 存在，BadgerDB 物理键删除
func TestSync_IsSyncAfterDelete_PhysicallyDeletedLocally(t *testing.T) {
	const userID = "sync_sad_u"
	db := newTestDB(t)
	cleanupMySQL(t, db, userID)
	t.Cleanup(func() { cleanupMySQL(t, db, userID) })

	item := newFund(userID, "BNB", 300.0)
	item.Status = 2 // IsSyncAfterDelete() = true

	db.Set(item, 0)
	triggerSync(t, db)

	// MySQL 有记录
	if findInMySQL(t, db, item) == nil {
		t.Fatal("MySQL 中应存在该记录")
	}

	// BadgerDB 物理键已删除（因为 IsSyncAfterDelete=true）
	if keyExistsInBadger(t, db, item) {
		t.Error("IsSyncAfterDelete=true 时同步后 BadgerDB 本地缓存应物理删除")
	}
}

// TestSync_InsertConflict_FallsBackToUpdate
// 同一 hash 已在 MySQL，再次同步相同 hash（Balance 变化）应 fallback 为更新
func TestSync_InsertConflict_FallsBackToUpdate(t *testing.T) {
	const userID = "sync_cfl_u"
	db := newTestDB(t)
	cleanupMySQL(t, db, userID)
	t.Cleanup(func() { cleanupMySQL(t, db, userID) })

	item := newFund(userID, "ADA", 10.0)

	// 第一次：插入并同步
	db.Set(item, 0)
	triggerSync(t, db)

	if findInMySQL(t, db, item) == nil {
		t.Fatal("前置条件失败：MySQL 中应有记录")
	}

	// 第二次：修改 Balance，触发更新路径
	item.Balance = 77.7
	db.Set(item, 0)
	triggerSync(t, db)

	got := findInMySQL(t, db, item)
	if got == nil {
		t.Fatal("冲突更新后 MySQL 中应仍有记录")
	}
	if got.Balance != 77.7 {
		t.Errorf("Balance 应为 77.7，实际 %v", got.Balance)
	}
}

// ============================================================
// 三、BatchInsert 同步测试
// ============================================================

// TestSync_BatchInsert_AllSyncToMySQL 5 条 BatchInsert 后同步全部到 MySQL
func TestSync_BatchInsert_AllSyncToMySQL(t *testing.T) {
	const userID = "sync_bi_u"
	db := newTestDB(t)
	cleanupMySQL(t, db, userID)
	t.Cleanup(func() { cleanupMySQL(t, db, userID) })

	markets := []string{"BTC", "ETH", "SOL", "BNB", "ADA"}
	items := make([]*testFund, len(markets))
	for i, m := range markets {
		items[i] = newFund(userID, m, float64(i+1)*10)
	}

	if err := db.BatchInsert(items); err != nil {
		t.Fatalf("BatchInsert 失败: %v", err)
	}

	if count, _ := db.GetPendingSyncCount(); count != len(items) {
		t.Fatalf("BatchInsert 后 PendingCount 应为 %d，实际 %d", len(items), count)
	}

	triggerSync(t, db)

	if count, _ := db.GetPendingSyncCount(); count != 0 {
		t.Fatalf("同步后 PendingCount 应为 0，实际 %d", count)
	}

	for _, item := range items {
		if findInMySQL(t, db, item) == nil {
			t.Errorf("MySQL 中缺失记录: %s", item.GetHash())
		}
	}
}

// TestSync_BatchInsert_LargeBatch_NoTxnTooBig 大批量 BatchInsert 不触发事务超限
func TestSync_BatchInsert_LargeBatch_NoTxnTooBig(t *testing.T) {
	const userID = "sync_big_u"
	const total = 100
	db := newTestDB(t)
	cleanupMySQL(t, db, userID)
	t.Cleanup(func() { cleanupMySQL(t, db, userID) })

	items := make([]*testFund, total)
	for i := 0; i < total; i++ {
		items[i] = newFund(userID, fmt.Sprintf("M%04d", i), float64(i))
	}

	if err := db.BatchInsert(items); err != nil {
		t.Fatalf("BatchInsert %d 条失败: %v", total, err)
	}
	triggerSync(t, db)

	if count, _ := db.GetPendingSyncCount(); count != 0 {
		t.Fatalf("同步后 PendingCount 应为 0，实际 %d", count)
	}
}

// ============================================================
// 四、BatchLoad 同步隔离测试
// ============================================================

// TestSync_BatchLoad_NotWrittenToMySQL
// BatchLoad 标记 IsSynced=true，processSyncQueue 无待同步项，MySQL 中无该数据
func TestSync_BatchLoad_NotWrittenToMySQL(t *testing.T) {
	const userID = "load_u"
	db := newTestDB(t)
	cleanupMySQL(t, db, userID)

	items := []*testFund{
		newFund(userID, "X1", 1),
		newFund(userID, "X2", 2),
	}

	if err := db.BatchLoad(items...); err != nil {
		t.Fatalf("BatchLoad 失败: %v", err)
	}

	// PendingCount=0
	if count, _ := db.GetPendingSyncCount(); count != 0 {
		t.Fatalf("BatchLoad 后 PendingCount 应为 0，实际 %d", count)
	}

	triggerSync(t, db)

	// MySQL 中不应有这些记录
	for _, item := range items {
		if findInMySQL(t, db, item) != nil {
			t.Errorf("BatchLoad 的数据不应被同步写入 MySQL: %s", item.GetHash())
		}
	}

	// BadgerDB 中应存在
	for _, item := range items {
		if !keyExistsInBadger(t, db, item) {
			t.Errorf("BatchLoad 的数据应在 BadgerDB 中存在: %s", item.GetHash())
		}
	}
}

// TestSync_BatchLoad_ThenSet_SyncsChange
// BatchLoad 引入远端数据 → 本地修改 (Set) → Sync → MySQL 存在更新后的记录
func TestSync_BatchLoad_ThenSet_SyncsChange(t *testing.T) {
	const userID = "load_set_u"
	db := newTestDB(t)
	cleanupMySQL(t, db, userID)
	t.Cleanup(func() { cleanupMySQL(t, db, userID) })

	// 模拟从远端加载：Balance=10（IsSynced=true，不触发同步）
	remote := newFund(userID, "MATIC", 10.0)
	db.BatchLoad(remote)

	// 本地修改，触发同步
	local := newFund(userID, "MATIC", 55.5)
	db.Set(local, 0)

	if isSyncedInBadger(t, db, local) {
		t.Error("Set 后 IsSynced 应为 false")
	}

	triggerSync(t, db)

	got := findInMySQL(t, db, local)
	if got == nil {
		t.Fatal("本地修改同步后 MySQL 中应有记录")
	}
	if got.Balance != 55.5 {
		t.Errorf("MySQL Balance 应为 55.5，实际 %v", got.Balance)
	}
}

// ============================================================
// 五、DeleteBySubPrefix 同步综合测试
// ============================================================

// TestSync_DeleteBySubPrefix_AfterSync_CleansBadger
// 全部已同步 (IsSynced=true) → DeleteBySubPrefix → 全部立即物理删除
func TestSync_DeleteBySubPrefix_AfterSync_CleansBadger(t *testing.T) {
	const userID = "dp_sync_u"
	db := newTestDB(t)
	cleanupMySQL(t, db, userID)
	t.Cleanup(func() { cleanupMySQL(t, db, userID) })

	items := []*testFund{
		newFund(userID, "A", 1),
		newFund(userID, "B", 2),
		newFund(userID, "C", 3),
	}
	for _, item := range items {
		db.Set(item, 0)
	}
	triggerSync(t, db) // 全部变为 IsSynced=true

	deleted, err := db.DeleteBySubPrefix(userID + ":")
	if err != nil {
		t.Fatalf("DeleteBySubPrefix 失败: %v", err)
	}
	if deleted != len(items) {
		t.Fatalf("期望立即删除 %d 条，实际 %d 条", len(items), deleted)
	}
	for _, item := range items {
		if keyExistsInBadger(t, db, item) {
			t.Errorf("BadgerDB 中 %s 应已被物理删除", item.GetHash())
		}
	}
}

// TestSync_DeleteBySubPrefix_PendingKeptThenSync
// 有未同步数据 → DeleteBySubPrefix 跳过未同步 → 手动同步 → 后台 goroutine 自动清理
func TestSync_DeleteBySubPrefix_PendingKeptThenSync(t *testing.T) {
	if testing.Short() {
		t.Skip("需要后台 goroutine 等待，-short 跳过")
	}
	const userID = "dp_pend_u"
	db := newTestDB(t)
	cleanupMySQL(t, db, userID)
	t.Cleanup(func() { cleanupMySQL(t, db, userID) })

	// 2 条已同步，1 条未同步
	synced1 := newFund(userID, "S1", 1)
	synced2 := newFund(userID, "S2", 2)
	pending := newFund(userID, "P1", 3)

	db.Set(synced1, 0)
	db.Set(synced2, 0)
	triggerSync(t, db)

	db.Set(pending, 0) // P1 此时 IsSynced=false

	deleted, err := db.DeleteBySubPrefix(userID + ":")
	if err != nil {
		t.Fatalf("DeleteBySubPrefix 失败: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("期望立即删除 2 条，实际 %d 条", deleted)
	}

	// P1 仍在 BadgerDB（等待同步后由后台 goroutine 清除）
	if !keyExistsInBadger(t, db, pending) {
		t.Error("P1 未同步，不应立即删除")
	}

	// 触发同步（P1 写入 MySQL，IsSynced 变为 true）
	triggerSync(t, db)

	// 等待后台 goroutine（waitAndDeleteKeys）在 15s 内完成
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !keyExistsInBadger(t, db, pending) {
			return // 后台已清除
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Error("后台 goroutine 未在 15s 内清除已同步的 pending 条目")
}

// TestSync_DeleteBySubPrefix_MultiUserIsolation
// 删除 userA 的数据，userB 的数据在 BadgerDB 和 MySQL 中均不受影响
func TestSync_DeleteBySubPrefix_MultiUserIsolation(t *testing.T) {
	const userA = "dp_a_u"
	const userB = "dp_b_u"
	db := newTestDB(t)
	cleanupMySQL(t, db, userA)
	cleanupMySQL(t, db, userB)
	t.Cleanup(func() {
		cleanupMySQL(t, db, userA)
		cleanupMySQL(t, db, userB)
	})

	itemA := newFund(userA, "X", 1)
	itemB := newFund(userB, "X", 2)

	db.Set(itemA, 0)
	db.Set(itemB, 0)
	triggerSync(t, db)

	db.DeleteBySubPrefix(userA + ":")

	// userB 的数据在 BadgerDB 中仍存在
	if !keyExistsInBadger(t, db, itemB) {
		t.Error("userB 在 BadgerDB 中的数据不应被删除")
	}
	// userB 的数据在 MySQL 中仍存在
	if findInMySQL(t, db, itemB) == nil {
		t.Error("userB 在 MySQL 中的数据不应被删除")
	}
}

// ============================================================
// 六、高并发测试（增删改同步正确性 + 多用户数据隔离）
// ============================================================

// minInt 返回两个 int 中较小的值（避免与包内 time.Duration 版 min 冲突）
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// concUID 生成并发测试专用用户 ID（前缀隔离，避免跨测试干扰）
func concUID(prefix string, u int) string {
	return fmt.Sprintf("%s_u%02d", prefix, u)
}

// concMkt 生成并发测试的市场代码
func concMkt(m int) string {
	return fmt.Sprintf("M%03d", m)
}

// concFund 构造并发测试用 fund，balance = u*1000+m（每条记录唯一，方便验证更新）
func concFund(prefix string, u, m int) *testFund {
	return newFund(concUID(prefix, u), concMkt(m), float64(u*1000+m))
}

// countMySQLByUser 查询 MySQL 中指定 userID 的记录数
func countMySQLByUser(t *testing.T, db *PrefixedBadgerDB[testFund], userID string) int {
	t.Helper()
	if db.syncList == nil {
		return 0
	}
	items, _ := db.syncList.SearchWhere("user_id", userID)
	return len(items)
}

// runParallel 并发运行 n 个任务，收集失败信息，等待全部完成
func runParallel(n int, fn func(i int) error) []string {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []string
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if err := fn(idx); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Sprintf("[%d] %v", idx, err))
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	return errs
}

// TestConcurrent_Insert_AllSyncToMySQL
// 5 用户 × 10 市场 = 50 goroutine 同时写入 → 单次 processSyncQueue → MySQL 全部存在
func TestConcurrent_Insert_AllSyncToMySQL(t *testing.T) {
	const (
		prefix   = "ci"
		nUsers   = 5
		nMarkets = 10
		total    = nUsers * nMarkets
	)
	db := newTestDB(t)
	for u := 0; u < nUsers; u++ {
		uid := concUID(prefix, u)
		cleanupMySQL(t, db, uid)
		t.Cleanup(func() { cleanupMySQL(t, db, uid) })
	}

	// 50 goroutine 并发写入（每条写不同 key，不产生 key 竞争）
	var writeCount int64
	errs := runParallel(nUsers*nMarkets, func(i int) error {
		u, m := i/nMarkets, i%nMarkets
		item := concFund(prefix, u, m)
		if err := db.Set(item, 0); err != nil {
			return err
		}
		atomic.AddInt64(&writeCount, 1)
		return nil
	})
	if len(errs) > 0 {
		t.Fatalf("并发写入失败 %d 条: %v", len(errs), errs[:minInt(3, len(errs))])
	}

	// 同步前验证计数
	if count, _ := db.GetPendingSyncCount(); count != total {
		t.Errorf("同步前 PendingCount 应为 %d，实际 %d", total, count)
	}

	triggerSync(t, db)

	// 同步后 PendingCount=0
	if count, _ := db.GetPendingSyncCount(); count != 0 {
		t.Errorf("同步后 PendingCount 应为 0，实际 %d", count)
	}

	// 验证 MySQL 中全部存在
	missing := 0
	for u := 0; u < nUsers; u++ {
		for m := 0; m < nMarkets; m++ {
			item := concFund(prefix, u, m)
			if findInMySQL(t, db, item) == nil {
				missing++
			}
		}
	}
	if missing > 0 {
		t.Errorf("MySQL 中有 %d/%d 条记录缺失", missing, total)
	}

	// 验证每个用户在 MySQL 中恰好 nMarkets 条
	for u := 0; u < nUsers; u++ {
		uid := concUID(prefix, u)
		if n := countMySQLByUser(t, db, uid); n != nMarkets {
			t.Errorf("userID=%s 应有 %d 条，实际 %d 条", uid, nMarkets, n)
		}
	}
}

// TestConcurrent_Update_AllSyncToMySQL
// 预先插入并同步 → 50 goroutine 并发修改 Balance → 单次同步 → MySQL 全部为新值
func TestConcurrent_Update_AllSyncToMySQL(t *testing.T) {
	const (
		prefix   = "cu"
		nUsers   = 5
		nMarkets = 10
	)
	db := newTestDB(t)
	for u := 0; u < nUsers; u++ {
		uid := concUID(prefix, u)
		cleanupMySQL(t, db, uid)
		t.Cleanup(func() { cleanupMySQL(t, db, uid) })
	}

	// 预插入：balance = 原始值 (u*1000+m)
	for u := 0; u < nUsers; u++ {
		for m := 0; m < nMarkets; m++ {
			db.Set(concFund(prefix, u, m), 0)
		}
	}
	triggerSync(t, db) // 全部同步到 MySQL

	// 并发更新：balance 变为原值 + 9999（唯一标记，方便验证）
	const delta = 9999.0
	errs := runParallel(nUsers*nMarkets, func(i int) error {
		u, m := i/nMarkets, i%nMarkets
		item := concFund(prefix, u, m)
		item.Balance += delta
		return db.Set(item, 0)
	})
	if len(errs) > 0 {
		t.Fatalf("并发更新失败 %d 条: %v", len(errs), errs[:minInt(3, len(errs))])
	}

	triggerSync(t, db)

	// 验证 MySQL 中全部为新值
	wrongCount := 0
	for u := 0; u < nUsers; u++ {
		for m := 0; m < nMarkets; m++ {
			item := concFund(prefix, u, m)
			got := findInMySQL(t, db, item)
			if got == nil {
				t.Errorf("MySQL 中缺失记录 %s", item.GetHash())
				wrongCount++
				continue
			}
			expected := float64(u*1000+m) + delta
			if got.Balance != expected {
				t.Errorf("%s Balance 应为 %.f，实际 %.f", item.GetHash(), expected, got.Balance)
				wrongCount++
			}
		}
	}
	if wrongCount > 0 {
		t.Errorf("共 %d 条记录更新验证失败", wrongCount)
	}
}

// TestConcurrent_Delete_AllRemovedFromMySQL
// 预先插入并同步 → 50 goroutine 并发软删除 → 单次同步 → MySQL 全部消失，BadgerDB 键全部物理删除
func TestConcurrent_Delete_AllRemovedFromMySQL(t *testing.T) {
	const (
		prefix   = "cd"
		nUsers   = 5
		nMarkets = 10
	)
	db := newTestDB(t)
	for u := 0; u < nUsers; u++ {
		uid := concUID(prefix, u)
		cleanupMySQL(t, db, uid)
		t.Cleanup(func() { cleanupMySQL(t, db, uid) })
	}

	// 预插入并同步
	for u := 0; u < nUsers; u++ {
		for m := 0; m < nMarkets; m++ {
			db.Set(concFund(prefix, u, m), 0)
		}
	}
	triggerSync(t, db)

	// 验证前置条件
	for u := 0; u < nUsers; u++ {
		uid := concUID(prefix, u)
		if n := countMySQLByUser(t, db, uid); n != nMarkets {
			t.Fatalf("前置条件失败：userID=%s 应有 %d 条，实际 %d", uid, nMarkets, n)
		}
	}

	// 并发软删除（syncDB=true，DeleteByItem 走软删除路径）
	errs := runParallel(nUsers*nMarkets, func(i int) error {
		u, m := i/nMarkets, i%nMarkets
		item := concFund(prefix, u, m)
		return db.DeleteByItem(item)
	})
	if len(errs) > 0 {
		t.Fatalf("并发删除失败 %d 条: %v", len(errs), errs[:minInt(3, len(errs))])
	}

	// 同步（执行删除到 MySQL，然后物理删除 BadgerDB 键）
	triggerSync(t, db)

	// 验证 MySQL 中全部消失
	for u := 0; u < nUsers; u++ {
		uid := concUID(prefix, u)
		if n := countMySQLByUser(t, db, uid); n != 0 {
			t.Errorf("同步后 userID=%s 在 MySQL 中应有 0 条，实际 %d 条", uid, n)
		}
	}

	// 验证 BadgerDB 键全部物理删除
	physicallyPresent := 0
	for u := 0; u < nUsers; u++ {
		for m := 0; m < nMarkets; m++ {
			if keyExistsInBadger(t, db, concFund(prefix, u, m)) {
				physicallyPresent++
			}
		}
	}
	if physicallyPresent > 0 {
		t.Errorf("BadgerDB 中仍有 %d 条软删除键未被物理删除", physicallyPresent)
	}
}

// TestConcurrent_Mixed_InsertUpdateDelete
// 60 goroutine 同时执行三种操作（20 插入 + 20 更新 + 20 删除），单次同步后验证三类结果
func TestConcurrent_Mixed_InsertUpdateDelete(t *testing.T) {
	const (
		prefix   = "cm"
		nMarkets = 10
		// 用户分组：0-1 用于插入，2-3 用于更新，4-5 用于删除
		insertGroup = 2
		updateGroup = 2
		deleteGroup = 2
	)
	db := newTestDB(t)
	for u := 0; u < insertGroup+updateGroup+deleteGroup; u++ {
		uid := concUID(prefix, u)
		cleanupMySQL(t, db, uid)
		t.Cleanup(func() { cleanupMySQL(t, db, uid) })
	}

	// 预先插入「更新组」和「删除组」的数据并同步
	for u := insertGroup; u < insertGroup+updateGroup+deleteGroup; u++ {
		for m := 0; m < nMarkets; m++ {
			db.Set(concFund(prefix, u, m), 0)
		}
	}
	triggerSync(t, db)

	// 三组任务加入同一 WaitGroup，同时启动
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		allErrs []string
	)
	addErr := func(msg string) {
		mu.Lock()
		allErrs = append(allErrs, msg)
		mu.Unlock()
	}

	// 组 A：并发插入（用户 0-1，全新记录）
	for u := 0; u < insertGroup; u++ {
		for m := 0; m < nMarkets; m++ {
			wg.Add(1)
			go func(u, m int) {
				defer wg.Done()
				if err := db.Set(concFund(prefix, u, m), 0); err != nil {
					addErr(fmt.Sprintf("INSERT u%d m%d: %v", u, m, err))
				}
			}(u, m)
		}
	}

	// 组 B：并发更新（用户 2-3，balance + 8888）
	const updDelta = 8888.0
	for u := insertGroup; u < insertGroup+updateGroup; u++ {
		for m := 0; m < nMarkets; m++ {
			wg.Add(1)
			go func(u, m int) {
				defer wg.Done()
				item := concFund(prefix, u, m)
				item.Balance += updDelta
				if err := db.Set(item, 0); err != nil {
					addErr(fmt.Sprintf("UPDATE u%d m%d: %v", u, m, err))
				}
			}(u, m)
		}
	}

	// 组 C：并发删除（用户 4-5）
	for u := insertGroup + updateGroup; u < insertGroup+updateGroup+deleteGroup; u++ {
		for m := 0; m < nMarkets; m++ {
			wg.Add(1)
			go func(u, m int) {
				defer wg.Done()
				if err := db.DeleteByItem(concFund(prefix, u, m)); err != nil {
					addErr(fmt.Sprintf("DELETE u%d m%d: %v", u, m, err))
				}
			}(u, m)
		}
	}

	wg.Wait() // 等待全部 60 个 goroutine 完成

	if len(allErrs) > 0 {
		t.Fatalf("并发操作失败 %d 处: %v", len(allErrs), allErrs[:minInt(5, len(allErrs))])
	}

	// 统一触发一次同步
	triggerSync(t, db)

	// 组 A 验证：插入的记录应在 MySQL 中存在
	for u := 0; u < insertGroup; u++ {
		for m := 0; m < nMarkets; m++ {
			item := concFund(prefix, u, m)
			if findInMySQL(t, db, item) == nil {
				t.Errorf("[INSERT] MySQL 中缺失 %s", item.GetHash())
			}
		}
	}

	// 组 B 验证：更新的记录应在 MySQL 中存在，且 Balance 为新值
	for u := insertGroup; u < insertGroup+updateGroup; u++ {
		for m := 0; m < nMarkets; m++ {
			item := concFund(prefix, u, m)
			got := findInMySQL(t, db, item)
			if got == nil {
				t.Errorf("[UPDATE] MySQL 中缺失 %s", item.GetHash())
				continue
			}
			expected := float64(u*1000+m) + updDelta
			if got.Balance != expected {
				t.Errorf("[UPDATE] %s Balance 应为 %.f，实际 %.f", item.GetHash(), expected, got.Balance)
			}
		}
	}

	// 组 C 验证：删除的记录不应在 MySQL 中存在，BadgerDB 键也应消失
	for u := insertGroup + updateGroup; u < insertGroup+updateGroup+deleteGroup; u++ {
		for m := 0; m < nMarkets; m++ {
			item := concFund(prefix, u, m)
			if findInMySQL(t, db, item) != nil {
				t.Errorf("[DELETE] %s 同步后 MySQL 中应已删除", item.GetHash())
			}
			if keyExistsInBadger(t, db, item) {
				t.Errorf("[DELETE] %s 同步后 BadgerDB 键应已物理删除", item.GetHash())
			}
		}
	}
}

// TestConcurrent_MultiUser_DataIsolation
// 5 用户各 20 goroutine 同时写入 → 同步 → 验证每用户恰好 20 条，无跨用户污染
func TestConcurrent_MultiUser_DataIsolation(t *testing.T) {
	const (
		prefix   = "co"
		nUsers   = 5
		nMarkets = 20 // 每用户 20 条
	)
	db := newTestDB(t)
	for u := 0; u < nUsers; u++ {
		uid := concUID(prefix, u)
		cleanupMySQL(t, db, uid)
		t.Cleanup(func() { cleanupMySQL(t, db, uid) })
	}

	// 5×20 = 100 goroutine 并发写入（每 goroutine 写不同用户的不同市场）
	errs := runParallel(nUsers*nMarkets, func(i int) error {
		u, m := i/nMarkets, i%nMarkets
		return db.Set(concFund(prefix, u, m), 0)
	})
	if len(errs) > 0 {
		t.Fatalf("并发写入失败 %d 条: %v", len(errs), errs[:minInt(3, len(errs))])
	}

	triggerSync(t, db)

	if count, _ := db.GetPendingSyncCount(); count != 0 {
		t.Errorf("同步后 PendingCount 应为 0，实际 %d", count)
	}

	// 验证每个用户恰好 nMarkets 条，且只有自己的数据
	for u := 0; u < nUsers; u++ {
		uid := concUID(prefix, u)
		n := countMySQLByUser(t, db, uid)
		if n != nMarkets {
			t.Errorf("userID=%s 应有 %d 条，实际 %d 条（可能有跨用户污染）", uid, nMarkets, n)
		}
	}

	// 逐条验证：每条记录的 UserID 字段与检索用的前缀一致
	for u := 0; u < nUsers; u++ {
		for m := 0; m < nMarkets; m++ {
			item := concFund(prefix, u, m)
			got := findInMySQL(t, db, item)
			if got == nil {
				t.Errorf("MySQL 中缺失 %s", item.GetHash())
				continue
			}
			// 确认 UserID 未被其他用户数据覆盖
			if got.UserID != concUID(prefix, u) {
				t.Errorf("%s 的 UserID 应为 %s，实际 %s（数据污染！）",
					item.GetHash(), concUID(prefix, u), got.UserID)
			}
		}
	}
}

// ============================================================
// 七、多远程 DB 路由测试（GetRemoteDBName 根据 Status 返回不同库名）
// ============================================================

// findInMySQLRouted 通过 item.Status 让 GetRemoteDBName() 自动路由到正确的 MySQL DB。
// 利用 SearchHash 的 fn 回调将 item 设为 SearchItem.Model，从而驱动 MySQL.GetDBName 路由。
func findInMySQLRouted(t *testing.T, db *PrefixedBadgerDB[testFund], item *testFund) *testFund {
	t.Helper()
	if db.syncList == nil {
		t.Fatal("syncList 未初始化，无法查询 MySQL")
	}
	got, _ := db.syncList.SearchHash(item.GetHash(), func(si *types.SearchItem) {
		si.Model = item // Status 字段驱动 GetRemoteDBName() 路由
	})
	return got
}

// cleanupMySQLRouted 按 status 路由到正确 DB，删除同 userID 的全部记录。
func cleanupMySQLRouted(t *testing.T, db *PrefixedBadgerDB[testFund], userID string, status int) {
	t.Helper()
	if db.syncList == nil {
		return
	}
	probe := &testFund{Model: entity.NewModel(), UserID: userID, Status: status}
	items, err := db.syncList.SearchWhere("user_id", userID, func(si *types.SearchItem) {
		si.Model = probe
	})
	if err != nil || len(items) == 0 {
		return
	}
	si := db.syncList.GetSearchItem()
	si.Model = probe
	action := db.syncList.GetDBAdapter(si)
	if action == nil {
		return
	}
	for _, item := range items {
		_ = action.Delete(item)
	}
}

// countMySQLRouted 按 status 路由到正确 DB，统计 userID 的记录数。
func countMySQLRouted(t *testing.T, db *PrefixedBadgerDB[testFund], userID string, status int) int {
	t.Helper()
	if db.syncList == nil {
		return 0
	}
	probe := &testFund{Model: entity.NewModel(), UserID: userID, Status: status}
	items, _ := db.syncList.SearchWhere("user_id", userID, func(si *types.SearchItem) {
		si.Model = probe
	})
	return len(items)
}

// TestSync_MultiDB_RoutingByStatus
//
// 验证 GetRemoteDBName() 按 Status 自动路由到不同库：
//   - Status=0 → test_funds_remote0，Status=1 → test_funds_remote1
//   - 单次 processSyncQueue 混合同步两种 Status
//   - 验证全部使用同一 syncList 的动态路由（无额外 DB 连接）
//   - 串库检测：用"反向 Status 探针"通过相同路由逻辑查询另一个库
func TestSync_MultiDB_RoutingByStatus(t *testing.T) {
	const (
		insertUID0 = "mdb_ins0_u" // Status=0，纯插入
		insertUID1 = "mdb_ins1_u" // Status=1，纯插入
		updateUID0 = "mdb_upd0_u" // Status=0，先插后改
		updateUID1 = "mdb_upd1_u" // Status=1，先插后改
		deleteUID0 = "mdb_del0_u" // Status=0，先插后删
		deleteUID1 = "mdb_del1_u" // Status=1，先插后删
	)
	allUIDs := []string{insertUID0, insertUID1, updateUID0, updateUID1, deleteUID0, deleteUID1}
	markets := []string{"BTC", "ETH", "SOL"}

	db := newTestDB(t)

	// 初始化：按各自 Status 路由，清除两个库中的遗留数据
	for _, uid := range allUIDs {
		for _, s := range []int{0, 1} {
			cleanupMySQLRouted(t, db, uid, s)
		}
	}
	t.Cleanup(func() {
		for _, uid := range allUIDs {
			for _, s := range []int{0, 1} {
				cleanupMySQLRouted(t, db, uid, s)
			}
		}
	})

	// ── 阶段 1：预插入「更新组」和「删除组」并同步 ──────────
	for _, m := range markets {
		u0 := newFund(updateUID0, m, 500)
		u0.Status = 0
		u1 := newFund(updateUID1, m, 600)
		u1.Status = 1
		d0 := newFund(deleteUID0, m, 700)
		d0.Status = 0
		d1 := newFund(deleteUID1, m, 800)
		d1.Status = 1
		db.Set(u0, 0)
		db.Set(u1, 0)
		db.Set(d0, 0)
		db.Set(d1, 0)
	}
	triggerSync(t, db)

	// ── 阶段 2：三类操作一次性写入，单次 processSyncQueue ────
	const updDelta = 5555.0

	for _, m := range markets {
		i0 := newFund(insertUID0, m, 100)
		i0.Status = 0
		i1 := newFund(insertUID1, m, 200)
		i1.Status = 1
		db.Set(i0, 0)
		db.Set(i1, 0)

		u0 := newFund(updateUID0, m, 500+updDelta)
		u0.Status = 0
		u1 := newFund(updateUID1, m, 600+updDelta)
		u1.Status = 1
		db.Set(u0, 0)
		db.Set(u1, 0)

		d0 := newFund(deleteUID0, m, 700)
		d0.Status = 0
		d1 := newFund(deleteUID1, m, 800)
		d1.Status = 1
		db.DeleteByItem(d0)
		db.DeleteByItem(d1)
	}

	triggerSync(t, db)

	if count, _ := db.GetPendingSyncCount(); count != 0 {
		t.Errorf("同步后 PendingCount 应为 0，实际 %d", count)
	}

	// crossProbe 构造同 hash 但 Status 取反的探针，用于串库检测
	crossProbe := func(item *testFund) *testFund {
		p := newFund(item.UserID, item.Market, 0)
		p.Status = 1 - item.Status // 路由到另一个库
		return p
	}

	// ── 验证：插入组 ─────────────────────────────────────────
	t.Run("Insert_DB0", func(t *testing.T) {
		for _, m := range markets {
			item := newFund(insertUID0, m, 100)
			item.Status = 0
			if findInMySQLRouted(t, db, item) == nil {
				t.Errorf("[DB0] 插入组 %s 缺失", item.GetHash())
			}
			if findInMySQLRouted(t, db, crossProbe(item)) != nil {
				t.Errorf("[串库] 插入组 %s 出现在 DB1（应在 DB0）", item.GetHash())
			}
		}
	})
	t.Run("Insert_DB1", func(t *testing.T) {
		for _, m := range markets {
			item := newFund(insertUID1, m, 200)
			item.Status = 1
			if findInMySQLRouted(t, db, item) == nil {
				t.Errorf("[DB1] 插入组 %s 缺失", item.GetHash())
			}
			if findInMySQLRouted(t, db, crossProbe(item)) != nil {
				t.Errorf("[串库] 插入组 %s 出现在 DB0（应在 DB1）", item.GetHash())
			}
		}
	})

	// ── 验证：更新组 ─────────────────────────────────────────
	t.Run("Update_DB0", func(t *testing.T) {
		for _, m := range markets {
			item := newFund(updateUID0, m, 0)
			item.Status = 0
			got := findInMySQLRouted(t, db, item)
			if got == nil {
				t.Errorf("[DB0] 更新组 %s 缺失", item.GetHash())
				return
			}
			if got.Balance != 500+updDelta {
				t.Errorf("[DB0] 更新组 %s Balance 应为 %.f，实际 %.f",
					item.GetHash(), 500+updDelta, got.Balance)
			}
			if findInMySQLRouted(t, db, crossProbe(item)) != nil {
				t.Errorf("[串库] 更新组 %s 出现在 DB1", item.GetHash())
			}
		}
	})
	t.Run("Update_DB1", func(t *testing.T) {
		for _, m := range markets {
			item := newFund(updateUID1, m, 0)
			item.Status = 1
			got := findInMySQLRouted(t, db, item)
			if got == nil {
				t.Errorf("[DB1] 更新组 %s 缺失", item.GetHash())
				return
			}
			if got.Balance != 600+updDelta {
				t.Errorf("[DB1] 更新组 %s Balance 应为 %.f，实际 %.f",
					item.GetHash(), 600+updDelta, got.Balance)
			}
			if findInMySQLRouted(t, db, crossProbe(item)) != nil {
				t.Errorf("[串库] 更新组 %s 出现在 DB0", item.GetHash())
			}
		}
	})

	// ── 验证：删除组 ─────────────────────────────────────────
	t.Run("Delete_DB0", func(t *testing.T) {
		for _, m := range markets {
			item := newFund(deleteUID0, m, 700)
			item.Status = 0
			if findInMySQLRouted(t, db, item) != nil {
				t.Errorf("[DB0] 删除组 %s 同步后应已从 DB0 删除", item.GetHash())
			}
			if keyExistsInBadger(t, db, item) {
				t.Errorf("[BadgerDB] 删除组 %s 同步后键应物理删除", item.GetHash())
			}
		}
	})
	t.Run("Delete_DB1", func(t *testing.T) {
		for _, m := range markets {
			item := newFund(deleteUID1, m, 800)
			item.Status = 1
			if findInMySQLRouted(t, db, item) != nil {
				t.Errorf("[DB1] 删除组 %s 同步后应已从 DB1 删除", item.GetHash())
			}
			if keyExistsInBadger(t, db, item) {
				t.Errorf("[BadgerDB] 删除组 %s 同步后键应物理删除", item.GetHash())
			}
		}
	})

	// ── 验证：各库记录数（确认无多余数据） ───────────────────
	t.Run("RecordCount", func(t *testing.T) {
		// DB0（Status=0）：插入组和更新组各 len(markets) 条，删除组为 0
		if n := countMySQLRouted(t, db, insertUID0, 0); n != len(markets) {
			t.Errorf("DB0 insertUID0 应有 %d 条，实际 %d 条", len(markets), n)
		}
		if n := countMySQLRouted(t, db, updateUID0, 0); n != len(markets) {
			t.Errorf("DB0 updateUID0 应有 %d 条，实际 %d 条", len(markets), n)
		}
		if n := countMySQLRouted(t, db, deleteUID0, 0); n != 0 {
			t.Errorf("DB0 deleteUID0 应有 0 条（已删除），实际 %d 条", n)
		}
		// DB1（Status=1）：同理
		if n := countMySQLRouted(t, db, insertUID1, 1); n != len(markets) {
			t.Errorf("DB1 insertUID1 应有 %d 条，实际 %d 条", len(markets), n)
		}
		if n := countMySQLRouted(t, db, updateUID1, 1); n != len(markets) {
			t.Errorf("DB1 updateUID1 应有 %d 条，实际 %d 条", len(markets), n)
		}
		if n := countMySQLRouted(t, db, deleteUID1, 1); n != 0 {
			t.Errorf("DB1 deleteUID1 应有 0 条（已删除），实际 %d 条", n)
		}
		// 串库：用反向 Status 路由查询，均应为 0 条
		for _, uid := range []string{insertUID1, updateUID1, deleteUID1} {
			if n := countMySQLRouted(t, db, uid, 0); n != 0 {
				t.Errorf("[串库] DB0 中不应含 %s 的数据，实际 %d 条", uid, n)
			}
		}
		for _, uid := range []string{insertUID0, updateUID0, deleteUID0} {
			if n := countMySQLRouted(t, db, uid, 1); n != 0 {
				t.Errorf("[串库] DB1 中不应含 %s 的数据，实际 %d 条", uid, n)
			}
		}
	})
}
