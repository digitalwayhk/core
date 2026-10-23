// 本文件验证07可选失败处置通过真实ServiceContext注册并按逆序关闭，不启动HTTP/MySQL。
package orderservice

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/transaction"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type failureLifecycleService struct{}

func (failureLifecycleService) ServiceName() string            { return "order-failure-lifecycle" }
func (failureLifecycleService) Routers() []servertypes.IRouter { return nil }

type orderedReceiptResource struct {
	*business.OrderFailureReceipts
	beforeClose func()
}

func (r *orderedReceiptResource) Close(ctx context.Context) error {
	r.beforeClose()
	return r.OrderFailureReceipts.Close(ctx)
}

// TestOrderFailureServiceContextLifecycle 验证正式组合根与资源逆序：先停止Core store，再关闭应用独立接管库。
func TestOrderFailureServiceContextLifecycle(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SHOP_LOCAL_PENDING_DIR", filepath.Join(root, "core"))
	cfg := config.NewServiceDefaultConfig("order-failure-lifecycle", 0)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	sc := router.NewServiceContextWithConfig(failureLifecycleService{}, cfg)
	t.Cleanup(func() { sc.SetRunState(false) })
	receipts, err := business.NewOrderFailureReceipts(filepath.Join(root, "app"))
	require.NoError(t, err)
	service := &Service{FailurePolicy: &nosql.WriteBehindFailurePolicy{StoreID: "order-failure-lifecycle:instance-a", MaxAttempts: 2, MinRetryInterval: 50 * time.Millisecond, MaxRetryInterval: time.Second, HandlerMaxAttempts: 4, HandlerTimeout: time.Second, HandlerWorkers: 1, ScanLimit: 8}}
	store, err := service.newOrderWriteStore(sc)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = store.Close(context.Background())
		_ = nosql.CloseSharedManager(filepath.Join(root, "core", "order-failure-lifecycle", "dc-0", "machine-0"))
	})
	var checked atomic.Bool
	service.FailureHandler = &orderedReceiptResource{OrderFailureReceipts: receipts, beforeClose: func() {
		_, err := store.FindLocalByRequest(context.Background(), 7, "lifecycle")
		require.ErrorIs(t, err, nosql.ErrWriteStoreClosed)
		checked.Store(true)
	}}
	require.NoError(t, service.bindOrderWriteStore(sc, store))
	// 受控关闭业务准入，确保正式OrderWriteBehindTarget在访问MySQL前返回未知失败。
	service.syncEnabled.Store(false)
	order := transaction.NewOrder()
	order.ID = 501
	order.UserID = 7
	order.SupplierID = 10
	order.ProductID = 20
	order.RequestID = "lifecycle"
	order.RequestFingerprint = "original"
	order.Quantity = 1
	order.UnitPrice = decimal.NewFromInt(9)
	order.TotalAmount = decimal.NewFromInt(9)
	require.NoError(t, service.ensureRuntime().Save(context.Background(), order))
	require.Eventually(t, func() bool { m, e := store.WriteBehindDispositionMetrics(); return e == nil && m.HandedOff == 1 }, 2*time.Second, time.Millisecond)
	r, _, err := store.ListWriteBehindFailures("", 10)
	require.NoError(t, err)
	require.Len(t, r, 1)
	sc.SetRunState(false)
	require.True(t, checked.Load())
	_, err = receipts.Read(r[0].ID)
	require.ErrorIs(t, err, badger.ErrDBClosed)
}
