// 本文件以07真实订单store和独立应用接管库验证正式失败处置接线，不访问MySQL或业务环境。
package business

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/transaction"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type unavailableOrderTarget struct{}

func (unavailableOrderTarget) SyncBatch(context.Context, []*nosql.SyncQueueItem[transaction.Order]) (*nosql.WriteBehindResult, error) {
	return nil, errors.New("示例注入写结果未知，不代表已回滚")
}

// TestOrderDurableFailureHandoff 验证应用拥有完整订单副本，Core只结束技术投递并保留顺序屏障。
func TestOrderDurableFailureHandoff(t *testing.T) {
	root := t.TempDir()
	receipts, err := NewOrderFailureReceipts(filepath.Join(root, "app-owned"))
	require.NoError(t, err)
	defer receipts.Close(context.Background())
	cfg := nosql.DefaultProductionConfig(root)
	cfg.AutoSync = true
	cfg.WriteBehindFailure = &nosql.WriteBehindFailurePolicy{StoreID: "shop-order:example-instance:orders", MaxAttempts: 2, MinRetryInterval: 50 * time.Millisecond, MaxRetryInterval: time.Second, HandlerMaxAttempts: 3, HandlerTimeout: time.Second, HandlerWorkers: 1, ScanLimit: 8}
	store, err := transaction.NewOrderWriteStore(nosql.ServiceIdentity{ServiceName: "example-failure"}, nosql.ReliableWriteStoreConfig{BasePath: filepath.Join(root, "core"), Badger: cfg})
	require.NoError(t, err)
	defer func() {
		_ = store.Close(context.Background())
		_ = nosql.CloseSharedManager(filepath.Join(root, "core", "example-failure", "dc-0", "machine-0"))
	}()
	require.NoError(t, store.UseWriteBehindWithFailureHandler(unavailableOrderTarget{}, receipts))
	order := transaction.NewOrder()
	order.ID = 101
	order.UserID = 7
	order.SupplierID = 10
	order.ProductID = 20
	order.RequestID = "failure-demo"
	order.RequestFingerprint = "same-original"
	order.Quantity = 1
	order.UnitPrice = decimal.NewFromInt(9)
	order.TotalAmount = decimal.NewFromInt(9)
	require.NoError(t, store.Save(context.Background(), order))
	require.Eventually(t, func() bool {
		m, e := store.WriteBehindDispositionMetrics()
		return e == nil && m.HandedOff == 1 && store.Metrics().Pending == 0
	}, 2*time.Second, time.Millisecond)
	records, _, err := store.ListWriteBehindFailures("", 10)
	require.NoError(t, err)
	require.Len(t, records, 1)
	r := records[0]
	require.Equal(t, "user:7", r.Group)
	require.Equal(t, nosql.WriteBehindHandedOff, r.State)
	require.Zero(t, store.Metrics().Sync.SyncedItems)
	require.NoError(t, receipts.Close(context.Background()))
	receipts, err = NewOrderFailureReceipts(filepath.Join(root, "app-owned"))
	require.NoError(t, err)
	defer receipts.Close(context.Background())
	saved, err := receipts.Read(r.ID)
	require.NoError(t, err)
	require.Equal(t, uint(101), saved.Record.Snapshot.Item.ID)
	require.Equal(t, "owned_unresolved", saved.State)
	require.Equal(t, *r.Receipt, saved.Receipt)
}
