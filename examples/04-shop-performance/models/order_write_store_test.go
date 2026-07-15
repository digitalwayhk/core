package models

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/require"
)

type blockingInsertAction struct {
	persistencetypes.IDataAction
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

type failingInsertAction struct {
	persistencetypes.IDataAction
	err error
}

func (a *failingInsertAction) Insert(interface{}) error            { return a.err }
func (a *failingInsertAction) Clone() persistencetypes.IDataAction { return a }

func (a *blockingInsertAction) Insert(data interface{}) error {
	a.once.Do(func() { close(a.entered) })
	<-a.release
	return a.IDataAction.Insert(data)
}

func (a *blockingInsertAction) Clone() persistencetypes.IDataAction { return a }

func TestOrderWriteStorePersistsLocallyThenConvergesToSQLite(t *testing.T) {
	root := t.TempDir()
	utils.TESTPATH = root
	action := oltp.NewSqlite()
	require.NoError(t, ensureModelWith(action, NewOrder()))
	blocking := &blockingInsertAction{
		IDataAction: action,
		entered:     make(chan struct{}),
		release:     make(chan struct{}),
	}
	config := nosql.DefaultProductionConfig(filepath.Join(root, "orders-badger"))
	config.EnableLogger = false
	store, err := newOrderWriteStore(config.Path, blocking, config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close(3 * time.Second) })

	createdAt := time.Now().UTC().Truncate(time.Second)
	order := NewOrder()
	order.SetID(1001)
	order.SetCreatedAt(createdAt)
	order.UserID = "user-a"
	order.ProductID = 2001
	order.Quantity = 2
	require.NoError(t, store.Add(order))

	select {
	case <-blocking.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("后台同步未开始")
	}
	pending, err := store.PendingByUser("user-a")
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, uint(1001), pending[0].ID)

	close(blocking.release)
	require.NoError(t, store.Flush())
	persisted, err := NewOrder().FindByIDWith(action, 1001)
	require.NoError(t, err)
	require.NotNil(t, persisted)
	require.Eventually(t, func() bool {
		items, scanErr := store.PendingByUser("user-a")
		return scanErr == nil && len(items) == 0
	}, 3*time.Second, 20*time.Millisecond)
}

func TestOrderWriteStoreAllowsSameUserProductWhenIDsDiffer(t *testing.T) {
	root := t.TempDir()
	utils.TESTPATH = root
	action := oltp.NewSqlite()
	require.NoError(t, ensureModelWith(action, NewOrder()))
	config := nosql.DefaultProductionConfig(filepath.Join(root, "orders-badger"))
	config.EnableLogger = false
	config.SyncBatchDelay = time.Second
	store, err := newOrderWriteStore(config.Path, action, config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close(3 * time.Second) })

	createdAt := time.Now().UTC().Truncate(time.Second)
	first := NewOrder()
	first.SetID(1)
	first.SetCreatedAt(createdAt)
	first.UserID = "user-a"
	first.ProductID = 2
	second := NewOrder()
	second.SetID(2)
	second.SetCreatedAt(createdAt)
	second.UserID = "user-a"
	second.ProductID = 2

	require.NoError(t, store.Add(first))
	require.NoError(t, store.Add(second), "GetHash 使用 ID 后同用户同商品同秒可并存")
	pending, err := store.PendingByUser("user-a")
	require.NoError(t, err)
	require.Len(t, pending, 2)
}

// TestOrderUsesUserPrefixedLocalKey 确保 Badger 可以按用户前缀扫描，
// 同时保持 SQLite 和公共 GetHash 仍以订单 ID 为唯一契约。
func TestOrderUsesUserPrefixedLocalKey(t *testing.T) {
	order := NewOrder()
	order.SetID(42)
	order.UserID = "user-a"

	require.Equal(t, "42", order.GetHash())
	require.Equal(t, orderPendingUserPrefix("user-a")+"42", order.GetLocalKey())
	require.NotContains(t, order.GetLocalKey(), "user-a", "Badger 键不应暴露原始用户 ID")
}

func TestOrderWriteStoreRejectsMissingID(t *testing.T) {
	root := t.TempDir()
	utils.TESTPATH = root
	action := oltp.NewSqlite()
	require.NoError(t, ensureModelWith(action, NewOrder()))
	config := nosql.DefaultProductionConfig(filepath.Join(root, "orders-badger"))
	config.EnableLogger = false
	store, err := newOrderWriteStore(config.Path, action, config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close(3 * time.Second) })

	order := NewOrder()
	order.UserID = "user-a"
	order.ProductID = 2
	require.ErrorContains(t, store.Add(order), "订单 ID 不能为空")
}

func TestOrderWriteStorePerformanceSnapshotSeparatesCommitAndSync(t *testing.T) {
	root := t.TempDir()
	utils.TESTPATH = root
	action := oltp.NewSqlite()
	require.NoError(t, ensureModelWith(action, NewOrder()))
	config := nosql.DefaultProductionConfig(filepath.Join(root, "orders-badger"))
	config.EnableLogger = false
	store, err := newOrderWriteStore(config.Path, action, config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close(3 * time.Second) })

	order := NewOrder()
	order.SetID(2001)
	order.UserID = "metrics-user"
	order.ProductID = 2002
	order.Quantity = 1
	require.NoError(t, store.Add(order))
	require.NoError(t, store.Flush())

	snapshot := store.PerformanceSnapshot()
	require.Equal(t, uint64(1), snapshot.GroupCommit.CommittedOrders)
	require.GreaterOrEqual(t, snapshot.Sync.SyncedItems, uint64(1))
	require.Zero(t, snapshot.PendingOrders)
	require.Greater(t, snapshot.BadgerDiskBytes, int64(0))
	require.Greater(t, snapshot.APIConfirmedTPS, float64(0))
	require.Greater(t, snapshot.SQLiteConvergenceTPS, float64(0))
}

func TestOrderWriteStoreKeepsPendingWhenSQLiteSyncFails(t *testing.T) {
	root := t.TempDir()
	utils.TESTPATH = root
	action := oltp.NewSqlite()
	require.NoError(t, ensureModelWith(action, NewOrder()))
	failing := &failingInsertAction{IDataAction: action, err: errors.New("sqlite unavailable")}
	config := nosql.DefaultProductionConfig(filepath.Join(root, "orders-badger"))
	config.EnableLogger = false
	config.SyncBatchDelay = time.Second
	store, err := newOrderWriteStore(config.Path, failing, config)
	require.NoError(t, err)

	order := NewOrder()
	order.SetID(3001)
	order.SetCreatedAt(time.Now().UTC().Truncate(time.Second))
	order.UserID = "failure-user"
	order.ProductID = 3002
	require.NoError(t, store.Add(order))
	require.Error(t, store.Flush(), "SQLite 同步失败必须向调用方返回错误")
	pending, err := store.PendingByUser("failure-user")
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Error(t, store.Close(3*time.Second), "关闭时仍同步失败必须返回错误")
}

// TestRemoveLocalWaitsForInflightSyncThenPurgesPending 确保业务删除不越过已取得快照的远端同步。
// 同步完成后删除才返回，使调用方紧随其后的 SQLite Delete 不会被迟到 insert 复活。
func TestRemoveLocalWaitsForInflightSyncThenPurgesPending(t *testing.T) {
	root := t.TempDir()
	utils.TESTPATH = root
	action := oltp.NewSqlite()
	require.NoError(t, ensureModelWith(action, NewOrder()))
	blocking := &blockingInsertAction{
		IDataAction: action,
		entered:     make(chan struct{}),
		release:     make(chan struct{}),
	}
	config := nosql.DefaultProductionConfig(filepath.Join(root, "orders-badger"))
	config.EnableLogger = false
	store, err := newOrderWriteStore(config.Path, blocking, config)
	require.NoError(t, err)
	var releaseOnce sync.Once
	releaseSync := func() { releaseOnce.Do(func() { close(blocking.release) }) }
	t.Cleanup(func() {
		releaseSync()
		_ = store.Close(3 * time.Second)
	})

	order := NewOrder()
	order.SetID(4001)
	order.SetCreatedAt(time.Now().UTC().Truncate(time.Second))
	order.UserID = "purge-user"
	order.ProductID = 4002
	order.Quantity = 1
	require.NoError(t, store.Add(order))
	select {
	case <-blocking.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("后台同步未开始")
	}
	pending, err := store.PendingByUser("purge-user")
	require.NoError(t, err)
	require.Len(t, pending, 1)

	removeDone := make(chan error, 1)
	go func() { removeDone <- store.RemoveLocal(order) }()
	select {
	case err := <-removeDone:
		t.Fatalf("远端同步尚未完成时 RemoveLocal 不得返回: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	releaseSync()
	require.NoError(t, <-removeDone)
	pending, err = store.PendingByUser("purge-user")
	require.NoError(t, err)
	require.Empty(t, pending, "ForceDeleteLocal 必须立即清除未同步订单，避免合并读复活")
}

// TestOrderDeleteDoesNotResurrectAfterLocalCleared 删除顺序：先本地后 SQLite。
func TestOrderDeleteDoesNotResurrectAfterLocalCleared(t *testing.T) {
	root := t.TempDir()
	utils.TESTPATH = root
	action := oltp.NewSqlite()
	require.NoError(t, ensureModelWith(action, NewOrder()))
	config := nosql.DefaultProductionConfig(filepath.Join(root, "orders-badger"))
	config.EnableLogger = false
	store, err := newOrderWriteStore(config.Path, action, config)
	require.NoError(t, err)

	globalOrderWriteStoreMu.Lock()
	globalOrderWriteState = &orderWriteStoreState{path: config.Path, store: store}
	// once 已完成，避免 Start 覆盖注入的 store。
	globalOrderWriteState.once.Do(func() {})
	globalOrderWriteStoreMu.Unlock()
	t.Cleanup(func() {
		_ = store.Close(3 * time.Second)
		globalOrderWriteStoreMu.Lock()
		globalOrderWriteState = nil
		globalOrderWriteStoreMu.Unlock()
		dataActionOnce = sync.Once{}
		dataAction = nil
	})
	dataActionOnce = sync.Once{}
	dataAction = action

	order := NewOrder()
	order.SetID(5001)
	order.SetCreatedAt(time.Now().UTC().Truncate(time.Second))
	order.UserID = "delete-user"
	order.ProductID = 5002
	order.Quantity = 1
	require.NoError(t, store.Add(order))
	require.NoError(t, store.Flush())
	require.Eventually(t, func() bool {
		items, scanErr := store.PendingByUser("delete-user")
		return scanErr == nil && len(items) == 0
	}, 3*time.Second, 20*time.Millisecond)

	require.NoError(t, order.Delete())
	visible, err := QueryVisibleOrders("delete-user")
	require.NoError(t, err)
	require.Empty(t, visible)
	persisted, err := NewOrder().FindByIDWith(action, 5001)
	require.NoError(t, err)
	require.Nil(t, persisted)
}
