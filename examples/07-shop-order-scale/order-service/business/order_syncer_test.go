// 本文件验证 07 订单服务本地可靠写入与远程同步器的失败重试闭环。
package business

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/transaction"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"github.com/yitter/idgenerator-go/idgen"
)

type boundedOrderSyncStore struct {
	limit int
	calls int
}

func (store *boundedOrderSyncStore) ForceSyncBatch(_ context.Context, limit int) (nosql.ForceSyncResult, error) {
	store.limit = limit
	store.calls++
	return nosql.ForceSyncResult{Confirmed: 2, Remaining: 3}, nil
}

func TestRemoteOrderSyncerDrainOnceHonorsLimitWithoutRebinding(t *testing.T) {
	store := &boundedOrderSyncStore{}
	syncer := RemoteOrderSyncer{Store: store}

	first, err := syncer.DrainOnce(context.Background(), 2)
	require.NoError(t, err)
	second, err := syncer.DrainOnce(context.Background(), 2)
	require.NoError(t, err)
	require.Equal(t, 2, store.limit)
	require.Equal(t, 2, store.calls)
	require.Equal(t, 2, first.Confirmed)
	require.Equal(t, 3, second.Remaining)
}

// TestOrderSyncerRetriesRemoteFailure 验证远程失败时 Badger 本地订单保留，恢复后可同步成功。
func TestOrderSyncerRetriesRemoteFailure(t *testing.T) {
	remote := &retryRemoteStore{fail: true}
	store, runtime := newBusinessOrderWriteRuntime(t, remote)
	unique := uint(time.Now().UnixNano() % 1000000)
	requestID := fmt.Sprintf("syncer-retry-request-%d", unique)
	ids := newBusinessTestIDFactory(25)

	command := CreateOrderCommand{
		OrderID:            ids.NewID(),
		UserID:             140000 + unique,
		RequestID:          requestID,
		RequestFingerprint: "fingerprint-" + requestID,
		SupplierID:         240000 + unique,
		ProductID:          340000 + unique,
		Quantity:           2,
		UnitPrice:          decimal.NewFromInt(9),
		TraceID:            "trace-syncer-retry",
		ServiceName:        "shop-order",
		ServiceInstanceID:  "order-syncer-a",
	}

	writer := LocalOrderWriter{Store: runtime}
	orderID, err := writer.Accept(context.Background(), command)
	require.NoError(t, err)
	require.Equal(t, command.OrderID, orderID)

	syncer := RemoteOrderSyncer{Store: runtime}
	_, err = syncer.DrainOnce(context.Background(), 10000)
	require.Error(t, err)

	pending, err := runtime.FindLocalByRequest(context.Background(), command.UserID, command.RequestID)
	require.NoError(t, err)
	require.NotNil(t, pending)

	remote.fail = false
	remote.calls = 0
	remote.seen = make(map[uint]bool)
	result, err := syncer.DrainOnce(context.Background(), 10000)
	require.NoError(t, err)
	require.Equal(t, 1, result.Confirmed)
	require.GreaterOrEqual(t, remote.calls, 1)
	require.True(t, remote.seen[command.OrderID])

	require.Eventually(t, func() bool {
		pending, err = runtime.FindLocalByRequest(context.Background(), command.UserID, command.RequestID)
		return err == nil && pending == nil
	}, time.Second, 20*time.Millisecond)
	pendingItems, err := store.PendingByUser(context.Background(), command.UserID)
	require.NoError(t, err)
	for _, item := range pendingItems {
		require.NotEqual(t, command.RequestID, item.RequestID)
	}

	require.Equal(t, command.OrderID, remote.stored.ID)
}

func newBusinessOrderWriteRuntime(
	t *testing.T,
	remote RemoteOrderStore,
) (*transaction.OrderWriteStore, *transaction.OrderWriteRuntime) {
	t.Helper()
	basePath := t.TempDir()
	resolvedPath := filepath.Join(basePath, "shop-order-business-test", "dc-0", "machine-0")
	badgerConfig := nosql.DefaultProductionConfig(resolvedPath)
	badgerConfig.EnableLogger = false
	badgerConfig.AutoSync = false
	store, err := transaction.NewOrderWriteStore(
		nosql.ServiceIdentity{ServiceName: "shop-order-business-test"},
		nosql.ReliableWriteStoreConfig{
			BasePath: basePath,
			Badger:   badgerConfig,
			Batch: nosql.BatchCommitConfig{
				MaxBatch:      16,
				CollectWindow: time.Millisecond,
				QueueCapacity: 64,
			},
			Admission: nosql.WriteAdmissionConfig{
				MaxConcurrent:  64,
				AcquireTimeout: time.Second,
			},
			CloseTimeout: 3 * time.Second,
		},
	)
	require.NoError(t, err)
	require.NoError(t, store.UseWriteBehind(OrderWriteBehindTarget{Remote: remote}))
	runtime := transaction.NewOrderWriteRuntime()
	require.NoError(t, runtime.Bind(store))
	t.Cleanup(func() {
		runtime.Unbind()
		_ = store.Close(context.Background())
		_ = nosql.CloseSharedManager(resolvedPath)
	})
	return store, runtime
}

type businessTestIDFactory struct {
	worker idgen.ISnowWorker
}

func newBusinessTestIDFactory(machineID uint) businessTestIDFactory {
	return businessTestIDFactory{worker: utils.NewAlgorithmSnowFlake(machineID, 4)}
}

func (f businessTestIDFactory) NewID() uint { return uint(f.worker.NextId()) }

type retryRemoteStore struct {
	fail   bool
	calls  int
	seen   map[uint]bool
	stored *models.Order
}

func (s *retryRemoteStore) UpsertBatch(ctx context.Context, orders []*models.Order) ([]*models.Order, error) {
	if s.seen == nil {
		s.seen = make(map[uint]bool)
	}
	s.calls++
	for _, order := range orders {
		s.seen[order.ID] = true
	}
	if s.fail {
		return nil, errors.New("remote temporarily unavailable")
	}
	if len(orders) > 0 {
		s.stored = orders[0]
	}
	return orders, nil
}
