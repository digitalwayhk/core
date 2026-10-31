// Package orderservice 验证 07 订单服务本地可靠 store 的实例目录和装配回滚边界。
package orderservice

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/transaction"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// newOrderWriteAssemblyTestContext 构造只包含实例身份的 ServiceContext；它故意不含资源管理器，用于验证注册失败回滚。
func newOrderWriteAssemblyTestContext() *router.ServiceContext {
	return &router.ServiceContext{
		Service: &servertypes.Service{Name: contract.OrderServiceName},
		Config:  &config.ServerConfig{DataCenterID: 3, MachineID: 9},
	}
}

// TestNewOrderWriteStoreUsesCurrentServiceIdentity 验证工厂使用当前服务和副本身份隔离 Badger 目录。
func TestNewOrderWriteStoreUsesCurrentServiceIdentity(t *testing.T) {
	basePath := t.TempDir()
	t.Setenv("SHOP_LOCAL_PENDING_DIR", basePath)
	sc := newOrderWriteAssemblyTestContext()
	service := &Service{}

	store, err := service.newOrderWriteStore(sc)
	require.NoError(t, err)
	resolvedPath := filepath.Join(basePath, contract.OrderServiceName, "dc-3", "machine-9")
	t.Cleanup(func() {
		_ = store.Close(context.Background())
		_ = nosql.CloseSharedManager(resolvedPath)
	})
	require.DirExists(t, resolvedPath)
}

// TestBindOrderWriteStoreUnbindsWhenResourceRegistrationFails 验证失败注册不会留下可被业务继续使用的 runtime。
func TestBindOrderWriteStoreUnbindsWhenResourceRegistrationFails(t *testing.T) {
	basePath := t.TempDir()
	t.Setenv("SHOP_LOCAL_PENDING_DIR", basePath)
	sc := newOrderWriteAssemblyTestContext()
	service := &Service{}
	store, err := service.newOrderWriteStore(sc)
	require.NoError(t, err)
	resolvedPath := filepath.Join(basePath, contract.OrderServiceName, "dc-3", "machine-9")
	t.Cleanup(func() { _ = nosql.CloseSharedManager(resolvedPath) })

	err = service.bindOrderWriteStore(sc, store)
	require.ErrorIs(t, err, router.ErrResourceManagerClosed)
	require.ErrorIs(
		t,
		service.ensureRuntime().Save(context.Background(), transaction.NewOrder()),
		transaction.ErrOrderWriteStoreUnavailable,
	)
}

type automaticOrderTarget struct{ seen chan struct{} }

func (target automaticOrderTarget) SyncBatch(_ context.Context, items []*nosql.SyncQueueItem[transaction.Order]) (*nosql.WriteBehindResult, error) {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.Key)
	}
	select {
	case target.seen <- struct{}{}:
	default:
	}
	return &nosql.WriteBehindResult{ConfirmedKeys: keys}, nil
}

// TestOrderFactoryAutomaticallyFlushesSmallBatch 验证低于阈值的订单也由框架 worker 自动同步。
func TestOrderFactoryAutomaticallyFlushesSmallBatch(t *testing.T) {
	base := t.TempDir()
	t.Setenv("SHOP_LOCAL_PENDING_DIR", base)
	service := &Service{}
	store, err := service.newOrderWriteStore(newOrderWriteAssemblyTestContext())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = store.Close(context.Background())
		_ = nosql.CloseSharedManager(filepath.Join(base, contract.OrderServiceName, "dc-3", "machine-9"))
	})
	seen := make(chan struct{}, 1)
	require.NoError(t, store.UseWriteBehind(automaticOrderTarget{seen: seen}))
	order := transaction.NewOrder()
	order.ID, order.UserID, order.SupplierID, order.ProductID = 123, 7, 10, 20
	order.RequestID, order.RequestFingerprint, order.Quantity = "automatic", "fingerprint", 1
	order.UnitPrice, order.TotalAmount = decimal.NewFromInt(9), decimal.NewFromInt(9)
	require.NoError(t, store.Save(context.Background(), order))
	select {
	case <-seen:
	case <-time.After(2 * time.Second):
		t.Fatal("低流量订单未被框架 worker 自动同步")
	}
	require.Eventually(t, func() bool { return store.Metrics().Pending == 0 }, time.Second, 10*time.Millisecond)
}
