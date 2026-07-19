// Package models 验证 04 订单实例级可靠写入、SQLite 汇合、失败保留和删除并发边界。
package models

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v3"
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

func (action *failingInsertAction) Insert(interface{}) error { return action.err }
func (action *failingInsertAction) Clone() persistencetypes.IDataAction {
	return action
}

func (action *blockingInsertAction) Insert(data interface{}) error {
	action.once.Do(func() { close(action.entered) })
	<-action.release
	return action.IDataAction.Insert(data)
}

func (action *blockingInsertAction) Clone() persistencetypes.IDataAction { return action }

func newOrderWriteTestHarness(
	t *testing.T,
	root string,
	action persistencetypes.IDataAction,
	autoSync bool,
) (*OrderWriteStore, *OrderWriteRuntime) {
	t.Helper()
	basePath := filepath.Join(root, "orders-badger")
	resolvedPath := filepath.Join(basePath, "shop-performance-test", "dc-0", "machine-0")
	badgerConfig := nosql.DefaultProductionConfig(resolvedPath)
	badgerConfig.EnableLogger = false
	badgerConfig.AutoSync = autoSync
	badgerConfig.SyncBatchDelay = 20 * time.Millisecond
	store, err := NewOrderWriteStore(
		nosql.ServiceIdentity{ServiceName: "shop-performance-test"},
		action,
		nosql.ReliableWriteStoreConfig{
			BasePath: basePath,
			Badger:   badgerConfig,
			Batch: nosql.BatchCommitConfig{
				MaxBatch:      32,
				CollectWindow: time.Millisecond,
				QueueCapacity: 128,
			},
			Admission: nosql.WriteAdmissionConfig{
				MaxConcurrent:  128,
				AcquireTimeout: time.Second,
			},
			CloseTimeout: 3 * time.Second,
		},
	)
	require.NoError(t, err)
	runtime := NewOrderWriteRuntime()
	require.NoError(t, runtime.Bind(store))
	t.Cleanup(func() {
		runtime.Unbind()
		_ = store.Close(context.Background())
		_ = nosql.CloseSharedManager(resolvedPath)
	})
	return store, runtime
}

func TestOrderWriteRuntimeInstancesDoNotShareStore(t *testing.T) {
	first := NewOrderWriteRuntime()
	second := NewOrderWriteRuntime()

	require.ErrorIs(t, first.Save(context.Background(), NewOrder()), ErrOrderWriteStoreUnavailable)
	require.ErrorIs(t, second.Save(context.Background(), NewOrder()), ErrOrderWriteStoreUnavailable)
}

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
	store, runtime := newOrderWriteTestHarness(t, root, blocking, true)
	var releaseOnce sync.Once
	releaseSync := func() { releaseOnce.Do(func() { close(blocking.release) }) }
	t.Cleanup(releaseSync)

	createdAt := time.Now().UTC().Truncate(time.Second)
	order := NewOrder()
	order.SetID(1001)
	order.SetCreatedAt(createdAt)
	order.UserID = "user-a"
	order.ProductID = 2001
	order.Quantity = 2
	require.NoError(t, runtime.Save(context.Background(), order))

	select {
	case <-blocking.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("后台同步未开始")
	}
	pending, err := store.PendingByUser(context.Background(), "user-a")
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, uint(1001), pending[0].ID)

	releaseSync()
	require.NoError(t, runtime.FlushOrders(context.Background()))
	persisted, err := NewOrder().FindByIDWith(action, 1001)
	require.NoError(t, err)
	require.NotNil(t, persisted)
	require.Eventually(t, func() bool {
		items, scanErr := store.PendingByUser(context.Background(), "user-a")
		return scanErr == nil && len(items) == 0
	}, 3*time.Second, 20*time.Millisecond)
}

func TestOrderWriteStoreAllowsSameUserProductWhenIDsDiffer(t *testing.T) {
	root := t.TempDir()
	utils.TESTPATH = root
	action := oltp.NewSqlite()
	require.NoError(t, ensureModelWith(action, NewOrder()))
	store, runtime := newOrderWriteTestHarness(t, root, action, false)

	first := NewOrder()
	first.SetID(1)
	first.UserID = "user-a"
	first.ProductID = 2
	second := NewOrder()
	second.SetID(2)
	second.UserID = "user-a"
	second.ProductID = 2

	require.NoError(t, runtime.Save(context.Background(), first))
	require.NoError(t, runtime.Save(context.Background(), second))
	pending, err := store.PendingByUser(context.Background(), "user-a")
	require.NoError(t, err)
	require.Len(t, pending, 2)
}

func TestOrderUsesUserPrefixedLocalKey(t *testing.T) {
	order := NewOrder()
	order.SetID(42)
	order.UserID = "user-a"

	require.Equal(t, "42", order.GetHash())
	require.Equal(t, orderPendingUserPrefix("user-a")+"42", order.GetLocalKey())
	require.NotContains(t, order.GetLocalKey(), "user-a")
}

func TestOrderWriteStoreRejectsMissingID(t *testing.T) {
	root := t.TempDir()
	utils.TESTPATH = root
	action := oltp.NewSqlite()
	require.NoError(t, ensureModelWith(action, NewOrder()))
	_, runtime := newOrderWriteTestHarness(t, root, action, false)

	order := NewOrder()
	order.UserID = "user-a"
	order.ProductID = 2
	require.ErrorContains(t, runtime.Save(context.Background(), order), "订单 ID 不能为空")
}

func TestOrderWriteStorePerformanceSnapshotSeparatesCommitAndSync(t *testing.T) {
	root := t.TempDir()
	utils.TESTPATH = root
	action := oltp.NewSqlite()
	require.NoError(t, ensureModelWith(action, NewOrder()))
	store, runtime := newOrderWriteTestHarness(t, root, action, false)

	order := NewOrder()
	order.SetID(2001)
	order.UserID = "metrics-user"
	order.ProductID = 2002
	order.Quantity = 1
	require.NoError(t, runtime.Save(context.Background(), order))
	require.NoError(t, runtime.FlushOrders(context.Background()))

	snapshot := store.PerformanceSnapshot()
	metrics := store.reliable.Metrics()
	require.Equal(t, uint64(1), snapshot.GroupCommit.Committed)
	require.GreaterOrEqual(t, snapshot.Sync.SyncedItems, uint64(1))
	require.Zero(t, snapshot.PendingOrders)
	require.Equal(t, metrics.BadgerLSMBytes+metrics.BadgerVLogBytes, snapshot.BadgerDiskBytes)
	require.Greater(t, snapshot.LifetimeAPIConfirmedTPS, float64(0))
	require.Greater(t, snapshot.LifetimeSQLiteConvergenceTPS, float64(0))
}

func TestOrderWriteStoreKeepsPendingWhenSQLiteSyncFails(t *testing.T) {
	root := t.TempDir()
	utils.TESTPATH = root
	action := oltp.NewSqlite()
	require.NoError(t, ensureModelWith(action, NewOrder()))
	failing := &failingInsertAction{IDataAction: action, err: errors.New("sqlite unavailable")}
	store, runtime := newOrderWriteTestHarness(t, root, failing, false)

	order := NewOrder()
	order.SetID(3001)
	order.UserID = "failure-user"
	order.ProductID = 3002
	require.NoError(t, runtime.Save(context.Background(), order))
	require.Error(t, runtime.FlushOrders(context.Background()))
	pending, err := store.PendingByUser(context.Background(), "failure-user")
	require.NoError(t, err)
	require.Len(t, pending, 1)
	var pendingErr *nosql.PendingSyncError
	require.ErrorAs(t, store.Close(context.Background()), &pendingErr)
}

func TestOrderWriteStoreDeleteKeepsTombstoneUntilSQLiteConfirms(t *testing.T) {
	root := t.TempDir()
	utils.TESTPATH = root
	action := oltp.NewSqlite()
	require.NoError(t, ensureModelWith(action, NewOrder()))
	blocking := &blockingInsertAction{
		IDataAction: action,
		entered:     make(chan struct{}),
		release:     make(chan struct{}),
	}
	store, runtime := newOrderWriteTestHarness(t, root, blocking, true)
	var releaseOnce sync.Once
	releaseSync := func() { releaseOnce.Do(func() { close(blocking.release) }) }
	t.Cleanup(releaseSync)

	order := NewOrder()
	order.SetID(4001)
	order.UserID = "delete-race-user"
	order.ProductID = 4002
	order.Quantity = 1
	require.NoError(t, runtime.Save(context.Background(), order))
	select {
	case <-blocking.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("后台同步未开始")
	}

	deleteDone := make(chan error, 1)
	go func() { deleteDone <- runtime.DeleteAndSync(context.Background(), order) }()
	require.Eventually(t, func() bool {
		_, err := store.reliable.GetLocal(context.Background(), order.GetLocalKey())
		return errors.Is(err, badger.ErrKeyNotFound) && store.reliable.Metrics().Pending == 1
	}, 3*time.Second, 10*time.Millisecond)
	select {
	case err := <-deleteDone:
		t.Fatalf("SQLite 尚未确认删除时 DeleteAndSync 不得返回: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	releaseSync()
	require.NoError(t, <-deleteDone)
	require.Zero(t, store.reliable.Metrics().Pending)
}

func TestOrderDeleteDoesNotResurrectAfterLocalSync(t *testing.T) {
	root := t.TempDir()
	utils.TESTPATH = root
	action := oltp.NewSqlite()
	require.NoError(t, ensureModelWith(action, NewOrder()))
	_, runtime := newOrderWriteTestHarness(t, root, action, false)

	order := NewOrder()
	order.SetID(5001)
	order.UserID = "delete-user"
	order.ProductID = 5002
	order.Quantity = 1
	require.NoError(t, runtime.Save(context.Background(), order))
	require.NoError(t, runtime.FlushOrders(context.Background()))
	require.NoError(t, runtime.DeleteAndSync(context.Background(), order))

	visible, err := runtime.QueryVisibleOrders(context.Background(), "delete-user")
	require.NoError(t, err)
	require.Empty(t, visible)
	persisted, err := NewOrder().FindByIDWith(action, 5001)
	require.NoError(t, err)
	require.Nil(t, persisted)
}
