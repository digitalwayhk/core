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

func TestOrderWriteStoreRejectsSameUserProductAndSecond(t *testing.T) {
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
	require.ErrorContains(t, store.Add(second), "每秒只能购买一次")
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
