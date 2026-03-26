package nosql

import (
	"sync"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/digitalwayhk/core/pkg/json"
	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/stretchr/testify/require"
)

// ============================================================
// 问题 1: batchUpdateSyncedStatus 的 CAS 检查缺陷
//
// batchUpdateSyncedStatus 在第一次尝试 (attempt==0) 时跳过了
// UpdatedAt 时间戳检查 (仅在 attempt > 0 时检查)。
// 当用户在 syncBatch 完成 MySQL 同步后、batchUpdateSyncedStatus
// 更新 BadgerDB 标记之前写入了新数据，新数据会被错误地标记
// 为 IsSynced=true，但实际上新数据从未同步到 MySQL。
//
// 预期行为: 新数据 (item_v2) 应保持 IsSynced=false
// 实际行为: 新数据被错误标记为 IsSynced=true → 数据丢失
// ============================================================

func TestIssue_BatchUpdateSyncedStatus_SkipsCASOnFirstAttempt(t *testing.T) {
	dir := t.TempDir()
	config := newTestConfig(dir)
	db, err := NewSharedBadgerDB[testLedger](config.Path, config)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	dbName := "test_issue_cas_v1"

	// 步骤 1: 写入 item_v1 到 BadgerDB，标记为未同步
	item_v1 := newLedger("user_cas", "BTC", 100, dbName)
	key := db.prefix + item_v1.GetHash()

	wrapper_v1 := &SyncQueueItem[testLedger]{
		Key:       key,
		Item:      item_v1,
		Op:        OpInsert,
		CreatedAt: time.Now().Add(-10 * time.Second),
		UpdatedAt: time.Now().Add(-10 * time.Second),
		IsSynced:  false,
	}
	err = db.manager.db.Update(func(txn *badger.Txn) error {
		data, marshalErr := json.Marshal(wrapper_v1)
		if marshalErr != nil {
			return marshalErr
		}
		return txn.Set([]byte(key), data)
	})
	require.NoError(t, err)

	// 步骤 2: 模拟 syncBatch 已将 item_v1 成功同步到 MySQL
	//         此时 syncBatch 返回了 successKeys = [key]
	successKeys := []string{key}

	// 步骤 3: 在 batchUpdateSyncedStatus 调用之前，用户写入了新数据 item_v2
	//         模拟并发: Set() 更新了同一个 key，UpdatedAt 变成现在的时间
	time.Sleep(5 * time.Millisecond) // 确保时间戳差异
	item_v2 := newLedger("user_cas", "BTC", 999, dbName)
	nowV2 := time.Now()
	wrapper_v2 := &SyncQueueItem[testLedger]{
		Key:       key,
		Item:      item_v2,
		Op:        OpUpdate,
		CreatedAt: wrapper_v1.CreatedAt,
		UpdatedAt: nowV2,
		IsSynced:  false, // 新数据未同步
	}
	err = db.manager.db.Update(func(txn *badger.Txn) error {
		data, marshalErr := json.Marshal(wrapper_v2)
		if marshalErr != nil {
			return marshalErr
		}
		return txn.Set([]byte(key), data)
	})
	require.NoError(t, err)

	// 步骤 4: 调用 batchUpdateSyncedStatus
	//         它应该检测到 wrapper 已被更新 (UpdatedAt 变了)，不该标记为已同步
	time.Sleep(5 * time.Millisecond)
	snapshotTimes := map[string]time.Time{key: wrapper_v1.UpdatedAt}
	err = db.batchUpdateSyncedStatus(successKeys, snapshotTimes)
	require.NoError(t, err)

	// 步骤 5: 验证 — 新数据 (item_v2) 不应被标记为 IsSynced=true
	var finalWrapper SyncQueueItem[testLedger]
	err = db.manager.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &finalWrapper)
		})
	})
	require.NoError(t, err)

	// 修复后，新数据 (item_v2) 不应被标记为 IsSynced=true
	if finalWrapper.IsSynced {
		t.Errorf("【修复未生效】batchUpdateSyncedStatus 将从未同步的新数据 (Amount=%.0f) "+
			"错误标记为 IsSynced=true。\n"+
			"  wrapper.UpdatedAt = %v\n"+
			"  期望: IsSynced=false（新数据应等待下一轮同步）\n"+
			"  实际: IsSynced=true（数据丢失——MySQL 仍是旧版本）",
			(*finalWrapper.Item).Amount, finalWrapper.UpdatedAt)
	} else {
		t.Logf("✅ CAS 检查正确: 新数据 (Amount=%.0f) 保持 IsSynced=false", (*finalWrapper.Item).Amount)
	}
}

// ============================================================
// 问题 1b: 端到端验证 — 在 syncBatch 完成 MySQL 写入后，
//          手动写入新数据再调 batchUpdateSyncedStatus，
//          新数据会被错误标记为已同步
// ============================================================

func TestIssue_ConcurrentSetDuringSync_DataNeverSynced(t *testing.T) {
	dir := t.TempDir()
	config := newTestConfig(dir)
	dbName := "test_issue_cas_e2e"

	cfg := &oltp.Config{
		Host:     "127.0.0.1",
		Port:     3306,
		Username: "root",
		Password: "your_password",
	}

	action := oltp.NewMySQL(cfg)
	list := entity.NewModelList[testLedger](action)

	// 手动绑定同步（不启动后台 goroutine）
	db := newManualSyncDBWithConfig(t, config, list)
	t.Cleanup(func() { cleanupNamedMySQLDB(dbName) })

	// 写入 v1 并同步到 MySQL
	v1 := newLedger("user_e2e", "ETH", 500, dbName)
	require.NoError(t, db.Set(v1, 0))
	synced, err := db.processSyncQueue()
	require.NoError(t, err)
	require.Equal(t, 1, synced, "v1 应成功同步")

	// 确认 MySQL 中有 v1
	found := findByHashWithAction(t, action, v1)
	require.NotNil(t, found, "MySQL 应有 v1")
	require.Equal(t, 500.0, found.Amount)

	// 更新为 v2
	v2 := newLedger("user_e2e", "ETH", 999, dbName)
	require.NoError(t, db.Set(v2, 0))

	// 取出 v2 的未同步 wrapper，再通过 syncBatch 同步到 MySQL
	unsyncedItems, err := db.getUnsyncedBatch(100)
	require.NoError(t, err)
	require.Len(t, unsyncedItems, 1, "应有一条未同步记录")

	// syncBatch 把 v2 同步到 MySQL 并调用 batchUpdateSyncedStatus
	successKeys, err := db.syncBatch(unsyncedItems)
	require.NoError(t, err)
	require.Len(t, successKeys, 1)

	// 验证 MySQL 已有 v2
	foundV2 := findByHashWithAction(t, action, v2)
	require.NotNil(t, foundV2)
	require.Equal(t, 999.0, foundV2.Amount, "MySQL 应已有 v2")

	// 步骤: 将 BadgerDB 中的 key 重置为未同步的 v3（模拟并发写入）
	v3 := newLedger("user_e2e", "ETH", 12345, dbName)
	key := db.prefix + v3.GetHash()
	nowV3 := time.Now()
	wrapper_v3 := &SyncQueueItem[testLedger]{
		Key:       key,
		Item:      v3,
		Op:        OpUpdate,
		CreatedAt: nowV3,
		UpdatedAt: nowV3,
		IsSynced:  false,
	}
	err = db.manager.db.Update(func(txn *badger.Txn) error {
		data, marshalErr := json.Marshal(wrapper_v3)
		if marshalErr != nil {
			return marshalErr
		}
		return txn.Set([]byte(key), data)
	})
	require.NoError(t, err)

	// 再次调用 batchUpdateSyncedStatus（用「上一轮 syncBatch」返回的 keys）
	// 这些 keys 对应的是 v2 的同步，不是 v3
	time.Sleep(5 * time.Millisecond)
	v2SnapshotTimes := map[string]time.Time{}
	for _, item := range unsyncedItems {
		v2SnapshotTimes[item.Key] = item.UpdatedAt
	}
	err = db.batchUpdateSyncedStatus(successKeys, v2SnapshotTimes)
	require.NoError(t, err)

	// 读取最终状态
	var afterWrapper SyncQueueItem[testLedger]
	err = db.manager.db.View(func(txn *badger.Txn) error {
		item, e := txn.Get([]byte(key))
		if e != nil {
			return e
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &afterWrapper)
		})
	})
	require.NoError(t, err)

	// MySQL 中此时只有 v2 (Amount=999)
	foundAfter := findByHashWithAction(t, action, v3)
	require.NotNil(t, foundAfter)
	mysqlAmount := foundAfter.Amount

	// 修复后，并发 Set 不应导致数据丢失
	if afterWrapper.IsSynced && afterWrapper.Item.Amount != mysqlAmount {
		t.Errorf("【修复未生效】并发 Set 导致数据丢失：\n"+
			"  BadgerDB: Amount=%.0f, IsSynced=%v\n"+
			"  MySQL:    Amount=%.0f",
			afterWrapper.Item.Amount, afterWrapper.IsSynced, mysqlAmount)
	} else {
		t.Logf("✅ CAS 保护生效: IsSynced=%v, Amount=%.0f", afterWrapper.IsSynced, afterWrapper.Item.Amount)
	}
}

// ============================================================
// 问题 2: batchInsertWithErrorHandling 缺少致命错误中断检查
//
// batchUpdateWithErrorHandling 遇到 "transaction has already
// been committed/rolled back" 或 "Rows are closed" 时会 break，
// 但 batchInsertWithErrorHandling 没有这些检查。
// ============================================================

type fatalErrorOnNthInsertAction struct {
	base  types.IDataAction
	state *fatalInsertState
}

type fatalInsertState struct {
	insertCount int32
	failAfterN  int32
}

func newFatalErrorOnNthInsertAction(failAfterN int32) *fatalErrorOnNthInsertAction {
	return &fatalErrorOnNthInsertAction{
		base: oltp.NewMySQL(&oltp.Config{
			Host:     "127.0.0.1",
			Port:     3306,
			Username: "root",
			Password: "your_password",
		}),
		state: &fatalInsertState{failAfterN: failAfterN},
	}
}

func (a *fatalErrorOnNthInsertAction) Clone() types.IDataAction {
	clonedBase := a.base
	if cloner, ok := a.base.(IActionCloner); ok {
		clonedBase = cloner.Clone()
	}
	return &fatalErrorOnNthInsertAction{base: clonedBase, state: a.state}
}
func (a *fatalErrorOnNthInsertAction) GetMaxOpenConns() int {
	if h, ok := a.base.(IMaxConcurrencyHint); ok {
		return h.GetMaxOpenConns()
	}
	return 0
}
func (a *fatalErrorOnNthInsertAction) GetSyncPoolKey(data interface{}) string {
	if p, ok := a.base.(ISyncPoolKeyProvider); ok {
		return p.GetSyncPoolKey(data)
	}
	return ""
}
func (a *fatalErrorOnNthInsertAction) Exists(data interface{}) (bool, error) {
	if ec, ok := a.base.(interface {
		Exists(data interface{}) (bool, error)
	}); ok {
		return ec.Exists(data)
	}
	return false, nil
}
func (a *fatalErrorOnNthInsertAction) Transaction() error { return a.base.Transaction() }
func (a *fatalErrorOnNthInsertAction) Load(item *types.SearchItem, result interface{}) error {
	return a.base.Load(item, result)
}
func (a *fatalErrorOnNthInsertAction) Insert(data interface{}) error {
	a.state.insertCount++
	if a.state.insertCount > a.state.failAfterN {
		return fatalTxError{}
	}
	return a.base.Insert(data)
}
func (a *fatalErrorOnNthInsertAction) Update(data interface{}) error { return a.base.Update(data) }
func (a *fatalErrorOnNthInsertAction) Delete(data interface{}) error { return a.base.Delete(data) }
func (a *fatalErrorOnNthInsertAction) Raw(s string, d interface{}) error {
	return a.base.Raw(s, d)
}
func (a *fatalErrorOnNthInsertAction) Exec(s string, d interface{}) error {
	return a.base.Exec(s, d)
}
func (a *fatalErrorOnNthInsertAction) GetModelDB(m interface{}) (interface{}, error) {
	return a.base.GetModelDB(m)
}
func (a *fatalErrorOnNthInsertAction) Commit() error         { return a.base.Commit() }
func (a *fatalErrorOnNthInsertAction) GetRunDB() interface{} { return a.base.GetRunDB() }
func (a *fatalErrorOnNthInsertAction) Rollback() error       { return a.base.Rollback() }

type fatalTxError struct{}

func (fatalTxError) Error() string { return "transaction has already been rolled back" }

func TestIssue_BatchInsertMissingFatalErrorBreak(t *testing.T) {
	dir := t.TempDir()
	config := newTestConfig(dir)
	dbName := "test_issue_fatal_ins"

	fatalAction := newFatalErrorOnNthInsertAction(2)
	list := entity.NewModelList[testLedger](fatalAction)
	db := newManualSyncDBWithConfig(t, config, list)
	t.Cleanup(func() { cleanupNamedMySQLDB(dbName) })

	for i := 0; i < 10; i++ {
		ledger := newLedger("user_fatal", "C_"+string(rune('A'+i)), float64(i*100), dbName)
		require.NoError(t, db.Set(ledger, 0))
	}

	db.processSyncQueue()

	t.Logf("Insert 计数: %d (理想 ≤ 3, 若为 10 则未中断)", fatalAction.state.insertCount)

	if fatalAction.state.insertCount > 5 {
		t.Errorf("【问题复现】batchInsertWithErrorHandling 未在致命错误后中断循环，"+
			"继续执行了 %d 次 Insert (期望 ≤ 3 次)", fatalAction.state.insertCount)
	}
}

// ============================================================
// 问题 3: syncBatch 信号量缺少 defer 保护
//
// panic 后信号量永远不会释放，后续 syncBatch 永久阻塞。
// ============================================================

type panicOnInsertAction struct {
	base        types.IDataAction
	shouldPanic int32
}

func newPanicOnInsertAction() *panicOnInsertAction {
	return &panicOnInsertAction{
		base: oltp.NewMySQL(&oltp.Config{
			Host:     "127.0.0.1",
			Port:     3306,
			Username: "root",
			Password: "your_password",
		}),
		shouldPanic: 1,
	}
}

func (a *panicOnInsertAction) Clone() types.IDataAction {
	clonedBase := a.base
	if cloner, ok := a.base.(IActionCloner); ok {
		clonedBase = cloner.Clone()
	}
	return &panicOnInsertAction{base: clonedBase, shouldPanic: a.shouldPanic}
}
func (a *panicOnInsertAction) GetMaxOpenConns() int {
	if h, ok := a.base.(IMaxConcurrencyHint); ok {
		return h.GetMaxOpenConns()
	}
	return 0
}
func (a *panicOnInsertAction) GetSyncPoolKey(data interface{}) string {
	if p, ok := a.base.(ISyncPoolKeyProvider); ok {
		return p.GetSyncPoolKey(data)
	}
	return ""
}
func (a *panicOnInsertAction) Transaction() error { return a.base.Transaction() }
func (a *panicOnInsertAction) Load(item *types.SearchItem, result interface{}) error {
	return a.base.Load(item, result)
}
func (a *panicOnInsertAction) Insert(data interface{}) error {
	if a.shouldPanic == 1 {
		panic("simulated Insert panic for semaphore leak test")
	}
	return a.base.Insert(data)
}
func (a *panicOnInsertAction) Update(data interface{}) error { return a.base.Update(data) }
func (a *panicOnInsertAction) Delete(data interface{}) error { return a.base.Delete(data) }
func (a *panicOnInsertAction) Raw(s string, d interface{}) error {
	return a.base.Raw(s, d)
}
func (a *panicOnInsertAction) Exec(s string, d interface{}) error {
	return a.base.Exec(s, d)
}
func (a *panicOnInsertAction) GetModelDB(m interface{}) (interface{}, error) {
	return a.base.GetModelDB(m)
}
func (a *panicOnInsertAction) Commit() error         { return a.base.Commit() }
func (a *panicOnInsertAction) GetRunDB() interface{} { return a.base.GetRunDB() }
func (a *panicOnInsertAction) Rollback() error       { return a.base.Rollback() }

func TestIssue_SyncBatchSemaphoreLeakOnPanic(t *testing.T) {
	dir := t.TempDir()
	config := newTestConfig(dir)
	dbName := "test_issue_sema"

	panicAction := newPanicOnInsertAction()
	list := entity.NewModelList[testLedger](panicAction)
	db := newManualSyncDBWithConfig(t, config, list)
	t.Cleanup(func() { cleanupNamedMySQLDB(dbName) })

	ledger := newLedger("user_sema", "BTC", 100, dbName)
	require.NoError(t, db.Set(ledger, 0))

	items, err := db.getUnsyncedBatch(100)
	require.NoError(t, err)
	require.Len(t, items, 1)

	// syncBatch 会在内部 panic → 如果没有 defer 释放信号量，后续会死锁
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
				t.Logf("捕获到 panic: %v", r)
			}
		}()
		db.syncBatch(items)
	}()
	require.True(t, panicked, "应触发 panic")

	// 禁用 panic，尝试正常同步
	panicAction.shouldPanic = 0

	ledger2 := newLedger("user_sema", "ETH", 200, dbName)
	require.NoError(t, db.Set(ledger2, 0))

	items2, err := db.getUnsyncedBatch(100)
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer func() {
			recover()
			close(done)
		}()
		db.syncBatch(items2)
	}()

	select {
	case <-done:
		t.Log("信号量正确释放，后续 syncBatch 正常完成")
	case <-time.After(5 * time.Second):
		t.Error("【问题复现】syncBatch 信号量泄漏：panic 后信号量未释放，" +
			"后续 syncBatch 永久阻塞 (5秒超时)")
	}
}

// ============================================================
// 问题 4: ConnectionManager.SetConnection 硬编码覆盖连接池配置
// ============================================================

func TestIssue_ConnectionManagerOverridesPoolConfig(t *testing.T) {
	cfg := &oltp.Config{
		Host:         "127.0.0.1",
		Port:         3306,
		Username:     "root",
		Password:     "your_password",
		MaxOpenConns: 20,
		MaxIdleConns: 10,
		MaxLifetime:  30 * time.Minute,
	}

	adapter := oltp.NewMySQL(cfg)
	adapter.Name = "test_issue_pool_cfg"
	t.Cleanup(func() {
		adapter.Name = "test_issue_pool_cfg"
		_ = adapter.DeleteDB()
	})

	reportedMax := adapter.GetMaxOpenConns()
	require.Equal(t, 20, reportedMax)

	gormDB, err := adapter.GetDB()
	require.NoError(t, err)

	sqlDB, err := gormDB.DB()
	require.NoError(t, err)

	stats := sqlDB.Stats()
	actualMaxOpen := stats.MaxOpenConnections

	if actualMaxOpen != reportedMax {
		t.Errorf("【问题复现】连接池参数不一致：\n"+
			"  Config.MaxOpenConns: %d\n"+
			"  实际 MaxOpenConnections: %d\n"+
			"  原因: ConnectionManager.SetConnection 硬编码 SetMaxOpenConns(10)\n"+
			"  影响: 信号量容量 (%d×75%%=%d) > 实际连接池 (%d)",
			reportedMax, actualMaxOpen,
			reportedMax, reportedMax*3/4, actualMaxOpen)
	}
}

// ============================================================
// 问题 5: batchUpdateWithErrorHandling 中 shouldInsert=true 时
//         Insert Duplicate entry 未降级为 Update
// ============================================================

type existsFailAction struct {
	base types.IDataAction
}

func newExistsFailAction() *existsFailAction {
	return &existsFailAction{
		base: oltp.NewMySQL(&oltp.Config{
			Host:     "127.0.0.1",
			Port:     3306,
			Username: "root",
			Password: "your_password",
		}),
	}
}

func (a *existsFailAction) Clone() types.IDataAction {
	clonedBase := a.base
	if cloner, ok := a.base.(IActionCloner); ok {
		clonedBase = cloner.Clone()
	}
	return &existsFailAction{base: clonedBase}
}
func (a *existsFailAction) GetMaxOpenConns() int {
	if h, ok := a.base.(IMaxConcurrencyHint); ok {
		return h.GetMaxOpenConns()
	}
	return 0
}
func (a *existsFailAction) GetSyncPoolKey(data interface{}) string {
	if p, ok := a.base.(ISyncPoolKeyProvider); ok {
		return p.GetSyncPoolKey(data)
	}
	return ""
}
func (a *existsFailAction) Exists(data interface{}) (bool, error) {
	return false, fatalTxError{}
}
func (a *existsFailAction) Transaction() error { return a.base.Transaction() }
func (a *existsFailAction) Load(item *types.SearchItem, result interface{}) error {
	return a.base.Load(item, result)
}
func (a *existsFailAction) Insert(data interface{}) error { return a.base.Insert(data) }
func (a *existsFailAction) Update(data interface{}) error { return a.base.Update(data) }
func (a *existsFailAction) Delete(data interface{}) error { return a.base.Delete(data) }
func (a *existsFailAction) Raw(s string, d interface{}) error {
	return a.base.Raw(s, d)
}
func (a *existsFailAction) Exec(s string, d interface{}) error {
	return a.base.Exec(s, d)
}
func (a *existsFailAction) GetModelDB(m interface{}) (interface{}, error) {
	return a.base.GetModelDB(m)
}
func (a *existsFailAction) Commit() error         { return a.base.Commit() }
func (a *existsFailAction) GetRunDB() interface{} { return a.base.GetRunDB() }
func (a *existsFailAction) Rollback() error       { return a.base.Rollback() }

func TestIssue_BatchUpdateInsertDuplicateNotHandled(t *testing.T) {
	dir := t.TempDir()
	config := newTestConfig(dir)
	dbName := "test_issue_dup_upd"

	normalCfg := &oltp.Config{
		Host:     "127.0.0.1",
		Port:     3306,
		Username: "root",
		Password: "your_password",
	}
	normalAction := oltp.NewMySQL(normalCfg)
	normalList := entity.NewModelList[testLedger](normalAction)
	dbNormal := newManualSyncDBWithConfig(t, config, normalList)
	t.Cleanup(func() { cleanupNamedMySQLDB(dbName) })

	ledger := newLedger("user_dup", "BTC", 100, dbName)
	require.NoError(t, dbNormal.Set(ledger, 0))
	synced, err := dbNormal.processSyncQueue()
	require.NoError(t, err)
	require.Equal(t, 1, synced)

	found := findByHashWithAction(t, normalAction, ledger)
	require.NotNil(t, found)
	require.Equal(t, 100.0, found.Amount)

	existsFailAct := newExistsFailAction()
	existsFailList := entity.NewModelList[testLedger](existsFailAct)
	dir2 := t.TempDir()
	config2 := newTestConfig(dir2)
	dbExistsFail := newManualSyncDBWithConfig(t, config2, existsFailList)

	ledgerUpdate := newLedger("user_dup", "BTC", 999, dbName)
	require.NoError(t, dbExistsFail.Set(ledgerUpdate, 0))

	// 手动将 Op 改为 OpUpdate
	key := dbExistsFail.prefix + ledgerUpdate.GetHash()
	err = dbExistsFail.manager.db.Update(func(txn *badger.Txn) error {
		item, e := txn.Get([]byte(key))
		if e != nil {
			return e
		}
		var wrapper SyncQueueItem[testLedger]
		if e := item.Value(func(val []byte) error {
			return json.Unmarshal(val, &wrapper)
		}); e != nil {
			return e
		}
		wrapper.Op = OpUpdate
		data, e := json.Marshal(&wrapper)
		if e != nil {
			return e
		}
		return txn.Set([]byte(key), data)
	})
	require.NoError(t, err)

	synced2, err := dbExistsFail.processSyncQueue()

	foundAfter := findByHashWithAction(t, normalAction, ledger)
	require.NotNil(t, foundAfter)

	if foundAfter.Amount != 999 {
		t.Errorf("【问题复现】batchUpdateWithErrorHandling 中 Exists 失败后 "+
			"Insert Duplicate entry 未降级为 Update：\n"+
			"  MySQL Amount: %.0f (期望: 999)\n"+
			"  synced=%d, err=%v",
			foundAfter.Amount, synced2, err)
	}
}

// ============================================================
// 问题 6: 并发 Set + Sync 最终一致性验证
// ============================================================

func TestIssue_ConcurrentSetAndSync_EventualConsistency(t *testing.T) {
	dir := t.TempDir()
	config := newTestConfig(dir)
	dbName := "test_issue_eventual"

	cfg := &oltp.Config{
		Host:     "127.0.0.1",
		Port:     3306,
		Username: "root",
		Password: "your_password",
	}
	action := oltp.NewMySQL(cfg)
	list := entity.NewModelList[testLedger](action)
	db := newManualSyncDBWithConfig(t, config, list)
	t.Cleanup(func() { cleanupNamedMySQLDB(dbName) })

	ledger := newLedger("user_eventual", "BTC", 100, dbName)
	require.NoError(t, db.Set(ledger, 0))
	synced, err := db.processSyncQueue()
	require.NoError(t, err)
	require.Equal(t, 1, synced)

	const writers = 10
	var wg sync.WaitGroup

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			amount := float64(1000 + idx*100)
			l := newLedger("user_eventual", "BTC", amount, dbName)
			_ = db.Set(l, 0)
		}(i)
	}

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(delay int) {
			defer wg.Done()
			time.Sleep(time.Duration(delay) * time.Millisecond)
			db.processSyncQueue()
		}(i)
	}

	wg.Wait()

	for i := 0; i < 3; i++ {
		db.processSyncQueue()
	}

	localItem, err := db.Get(ledger.GetHash())
	require.NoError(t, err)

	mysqlItem := findByHashWithAction(t, action, ledger)
	require.NotNil(t, mysqlItem, "MySQL 应有记录")

	if localItem.Amount != mysqlItem.Amount {
		t.Errorf("【问题复现】并发写入期间同步导致最终不一致：\n"+
			"  BadgerDB Amount: %.0f\n"+
			"  MySQL Amount:    %.0f",
			localItem.Amount, mysqlItem.Amount)
	} else {
		t.Logf("最终一致性验证: local=%.0f, mysql=%.0f", localItem.Amount, mysqlItem.Amount)
	}

	key := db.prefix + ledger.GetHash()
	var finalWrapper SyncQueueItem[testLedger]
	err = db.manager.db.View(func(txn *badger.Txn) error {
		item, e := txn.Get([]byte(key))
		if e != nil {
			return e
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &finalWrapper)
		})
	})
	require.NoError(t, err)

	if finalWrapper.IsSynced && finalWrapper.Item.Amount != mysqlItem.Amount {
		t.Errorf("【修复未生效】BadgerDB IsSynced=true 但与 MySQL 不一致：\n"+
			"  BadgerDB: Amount=%.0f, IsSynced=%v\n"+
			"  MySQL:    Amount=%.0f",
			finalWrapper.Item.Amount, finalWrapper.IsSynced, mysqlItem.Amount)
	}
}
