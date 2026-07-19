// Package nosql 验证 ReliableWriteStore 配置默认值与服务实例路径隔离契约。
package nosql

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResolveReliableWritePathUsesServiceAndMachineIdentity(t *testing.T) {
	path, err := resolveReliableWritePath("/data/pending", ServiceIdentity{
		ServiceName: "Shop-Order", DataCenterID: 2, MachineID: 7,
	})
	require.NoError(t, err)
	require.Equal(t, filepath.Join("/data/pending", "shop-order", "dc-2", "machine-7"), path)
}

func TestResolveReliableWritePathRejectsUnsafeIdentity(t *testing.T) {
	for _, identity := range []ServiceIdentity{
		{ServiceName: "", DataCenterID: 1, MachineID: 1},
		{ServiceName: "../order", DataCenterID: 1, MachineID: 1},
		{ServiceName: "order/a", DataCenterID: 1, MachineID: 1},
		{ServiceName: "order\\a", DataCenterID: 1, MachineID: 1},
		{ServiceName: "order", DataCenterID: -1, MachineID: 1},
		{ServiceName: "order", DataCenterID: 1, MachineID: -1},
	} {
		_, err := resolveReliableWritePath(t.TempDir(), identity)
		require.Error(t, err, "identity=%+v", identity)
	}
}

func TestResolveReliableWritePathSeparatesServicesAndMachines(t *testing.T) {
	base := t.TempDir()
	serviceA, err := resolveReliableWritePath(base, ServiceIdentity{ServiceName: "order-a", DataCenterID: 1, MachineID: 3})
	require.NoError(t, err)
	serviceB, err := resolveReliableWritePath(base, ServiceIdentity{ServiceName: "order-b", DataCenterID: 1, MachineID: 3})
	require.NoError(t, err)
	machineB, err := resolveReliableWritePath(base, ServiceIdentity{ServiceName: "order-a", DataCenterID: 1, MachineID: 4})
	require.NoError(t, err)
	require.NotEqual(t, serviceA, serviceB)
	require.NotEqual(t, serviceA, machineB)
}

func TestReliableWriteStoreConfigNormalizesDefaultsAndBadgerPath(t *testing.T) {
	base := t.TempDir()
	config := ReliableWriteStoreConfig{
		BasePath: base,
		Badger:   DefaultProductionConfig("/caller/must/not/win"),
	}

	normalized, err := config.normalized(ServiceIdentity{ServiceName: "order", DataCenterID: 3, MachineID: 9})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(base, "order", "dc-3", "machine-9"), normalized.Badger.Path)
	require.Equal(t, normalized.Badger.Path, normalized.BasePath)
	require.Equal(t, 128, normalized.Batch.MaxBatch)
	require.Equal(t, time.Millisecond, normalized.Batch.CollectWindow)
	require.Equal(t, 1024, normalized.Batch.QueueCapacity)
	require.Equal(t, 10*time.Second, normalized.CloseTimeout)
}

func TestReliableWriteStoreConfigRejectsEmptyBasePath(t *testing.T) {
	_, err := (ReliableWriteStoreConfig{}).normalized(ServiceIdentity{ServiceName: "order", DataCenterID: 1, MachineID: 1})
	require.Error(t, err)
}

func TestReliableWriteStoreConfigDefaultsBadgerProductionMode(t *testing.T) {
	normalized, err := (ReliableWriteStoreConfig{BasePath: t.TempDir()}).normalized(ServiceIdentity{
		ServiceName: "order", DataCenterID: 1, MachineID: 1,
	})
	require.NoError(t, err)
	require.Equal(t, "production", normalized.Badger.Mode)
	require.True(t, normalized.Badger.SyncWrites)
}

func TestReliableWriteStoreConfigRejectsQueueSmallerThanBatch(t *testing.T) {
	config := ReliableWriteStoreConfig{
		BasePath: t.TempDir(),
		Batch: BatchCommitConfig{
			MaxBatch:      32,
			CollectWindow: time.Millisecond,
			QueueCapacity: 16,
		},
	}
	_, err := config.normalized(ServiceIdentity{ServiceName: "order", DataCenterID: 1, MachineID: 1})
	require.ErrorIs(t, err, ErrInvalidReliableWriteConfig)
}
