// Package shoporderscale 提供 07 集成测试与基准使用的实例级订单 store harness。
package shoporderscale

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	orderbusiness "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/transaction"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/stretchr/testify/require"
)

func newIntegrationOrderRuntime(
	t testing.TB,
	remote orderbusiness.RemoteOrderStore,
) *transaction.OrderWriteRuntime {
	t.Helper()
	basePath := t.TempDir()
	resolvedPath := filepath.Join(basePath, "shop-order-integration-test", "dc-0", "machine-0")
	badgerConfig := nosql.DefaultProductionConfig(resolvedPath)
	badgerConfig.EnableLogger = false
	badgerConfig.AutoSync = false
	store, err := transaction.NewOrderWriteStore(
		nosql.ServiceIdentity{ServiceName: "shop-order-integration-test"},
		nosql.ReliableWriteStoreConfig{
			BasePath: basePath,
			Badger:   badgerConfig,
			Batch: nosql.BatchCommitConfig{
				MaxBatch:      128,
				CollectWindow: time.Millisecond,
				QueueCapacity: 1024,
			},
			Admission: nosql.WriteAdmissionConfig{
				MaxConcurrent:  500,
				AcquireTimeout: 2 * time.Second,
			},
			CloseTimeout: 10 * time.Second,
		},
	)
	require.NoError(t, err)
	require.NoError(t, store.UseWriteBehind(orderbusiness.OrderWriteBehindTarget{Remote: remote}))
	runtime := transaction.NewOrderWriteRuntime()
	require.NoError(t, runtime.Bind(store))
	t.Cleanup(func() {
		runtime.Unbind()
		closeErr := store.Close(context.Background())
		var pendingErr *nosql.PendingSyncError
		if closeErr != nil && !errors.As(closeErr, &pendingErr) {
			require.NoError(t, closeErr)
		}
		require.NoError(t, nosql.CloseSharedManager(resolvedPath))
	})
	return runtime
}
