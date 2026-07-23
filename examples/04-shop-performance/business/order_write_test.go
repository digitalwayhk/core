// Package business 提供业务测试使用的实例级订单 ReliableWriteStore harness。
package business

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/04-shop-performance/models"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/require"
)

func newBusinessOrderRuntime(t *testing.T) *models.OrderWriteRuntime {
	t.Helper()
	require.NoError(t, models.EnsureStorage())
	basePath := filepath.Join(utils.Getpath(), "data", "business-order-test")
	resolvedPath := filepath.Join(basePath, "shop-performance-business-test", "dc-0", "machine-0")
	badgerConfig := nosql.DefaultProductionConfig(resolvedPath)
	badgerConfig.EnableLogger = false
	badgerConfig.AutoSync = false
	store, err := models.NewOrderWriteStore(
		nosql.ServiceIdentity{ServiceName: "shop-performance-business-test"},
		models.CloneDataAction(),
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
	runtime := models.NewOrderWriteRuntime()
	require.NoError(t, runtime.Bind(store))
	t.Cleanup(func() {
		runtime.Unbind()
		_ = store.Close(context.Background())
		_ = nosql.CloseSharedManager(resolvedPath)
	})
	return runtime
}
