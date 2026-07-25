// Package transaction 验证 07 订单 runtime 实例隔离和框架 ACK 驱动的 pending 收敛。
package transaction

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type confirmOrderWriteTarget struct{}

func (confirmOrderWriteTarget) SyncBatch(
	_ context.Context,
	items []*nosql.SyncQueueItem[Order],
) (*nosql.WriteBehindResult, error) {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		if item != nil {
			keys = append(keys, item.Key)
		}
	}
	return &nosql.WriteBehindResult{ConfirmedKeys: keys}, nil
}

func newOrderWriteStoreTestRuntime(t *testing.T, service string) (*OrderWriteStore, *OrderWriteRuntime) {
	t.Helper()
	basePath := t.TempDir()
	resolvedPath := filepath.Join(basePath, service, "dc-0", "machine-0")
	badgerConfig := nosql.DefaultProductionConfig(resolvedPath)
	badgerConfig.EnableLogger = false
	badgerConfig.AutoSync = false
	store, err := NewOrderWriteStore(
		nosql.ServiceIdentity{ServiceName: service},
		nosql.ReliableWriteStoreConfig{
			BasePath: basePath,
			Badger:   badgerConfig,
			Batch: nosql.BatchCommitConfig{
				MaxBatch:      8,
				CollectWindow: time.Millisecond,
				QueueCapacity: 32,
			},
			Admission: nosql.WriteAdmissionConfig{
				MaxConcurrent:  32,
				AcquireTimeout: time.Second,
			},
			CloseTimeout: 3 * time.Second,
		},
	)
	require.NoError(t, err)
	require.NoError(t, store.UseWriteBehind(confirmOrderWriteTarget{}))
	runtime := NewOrderWriteRuntime()
	require.NoError(t, runtime.Bind(store))
	t.Cleanup(func() {
		runtime.Unbind()
		_ = store.Close(context.Background())
		_ = nosql.CloseSharedManager(resolvedPath)
	})
	return store, runtime
}

func newReliableOrder(id, userID uint, requestID string) *Order {
	order := NewOrder()
	order.ID = id
	order.UserID = userID
	order.RequestID = requestID
	order.RequestFingerprint = "fingerprint-" + requestID
	order.SupplierID = 10
	order.ProductID = 20
	order.Quantity = 1
	order.UnitPrice = decimal.NewFromInt(9)
	order.TotalAmount = decimal.NewFromInt(9)
	return order
}

func TestOrderWriteRuntimeInstancesAreIsolated(t *testing.T) {
	_, first := newOrderWriteStoreTestRuntime(t, "shop-order-runtime-a")
	_, second := newOrderWriteStoreTestRuntime(t, "shop-order-runtime-b")
	firstOrder := newReliableOrder(101, 7, "same-request")
	secondOrder := newReliableOrder(202, 7, "same-request")

	require.NoError(t, first.Save(context.Background(), firstOrder))
	require.NoError(t, second.Save(context.Background(), secondOrder))
	firstFound, err := first.FindLocalByRequest(context.Background(), 7, "same-request")
	require.NoError(t, err)
	secondFound, err := second.FindLocalByRequest(context.Background(), 7, "same-request")
	require.NoError(t, err)
	require.Equal(t, uint(101), firstFound.ID)
	require.Equal(t, uint(202), secondFound.ID)
}

func TestOrderWriteStorePendingFollowsFrameworkAck(t *testing.T) {
	store, runtime := newOrderWriteStoreTestRuntime(t, "shop-order-pending-test")
	order := newReliableOrder(303, 8, "pending-request")

	require.NoError(t, runtime.Save(context.Background(), order))
	require.Equal(t, 1, runtime.Metrics().Pending)
	result, err := runtime.ForceSyncBatch(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, result.Confirmed)
	require.Zero(t, result.Remaining)
	require.Zero(t, runtime.Metrics().Pending)
	require.Eventually(t, func() bool {
		found, findErr := store.FindLocalByRequest(context.Background(), 8, "pending-request")
		return findErr == nil && found == nil
	}, time.Second, 10*time.Millisecond)
}

func TestFindLocalByRequestUsesConstantCostLookup(t *testing.T) {
	store, _ := newOrderWriteStoreTestRuntime(t, "shop-order-idempotency-lookup")
	orders := make([]*Order, 256)
	for index := range orders {
		orders[index] = newReliableOrder(
			uint(10_000+index),
			99,
			fmt.Sprintf("request-%03d", index),
		)
		orders[index].prepareForLocalInsert()
	}
	result, err := store.reliable.SaveBatch(context.Background(), orders)
	require.NoError(t, err)
	require.Equal(t, len(orders), result.Committed)

	allocations := testing.AllocsPerRun(3, func() {
		found, findErr := store.FindLocalByRequest(context.Background(), 99, "request-255")
		require.NoError(t, findErr)
		require.NotNil(t, found)
		require.Equal(t, uint(10_255), found.ID)
	})
	require.Less(t, allocations, float64(500), "幂等键查询不应扫描并反序列化用户全部 pending")
}
