// Package shoporderscale 验证 07 水平扩容 benchmark 的副本拓扑配置。
package shoporderscale

import (
	"context"
	"path/filepath"
	"testing"

	orderbusiness "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	"github.com/stretchr/testify/require"
)

func TestIntegrationOrderReplicaPathUsesMachineID(t *testing.T) {
	basePath := t.TempDir()
	require.Equal(t,
		filepath.Join(basePath, "shop-order-integration-test", "dc-0", "machine-3"),
		integrationOrderReplicaPath(basePath, 3),
	)
	require.NotEqual(t,
		integrationOrderReplicaPath(basePath, 3),
		integrationOrderReplicaPath(basePath, 4),
	)
}

func TestIntegrationOrderReplicaRuntimesDoNotSharePending(t *testing.T) {
	basePath := t.TempDir()
	first := newIntegrationOrderReplicaRuntime(t, nil, basePath, 1)
	second := newIntegrationOrderReplicaRuntime(t, nil, basePath, 2)
	command := make07OrderCommands("replica-isolation", newBenchmarkIDFactory(1), 190000001, 1)[0]
	command.ServiceInstanceID = "order-replica-1"

	_, err := (orderbusiness.LocalOrderWriter{Store: first}).Accept(context.Background(), command)
	require.NoError(t, err)
	firstPending, err := first.FindLocalByRequest(context.Background(), command.UserID, command.RequestID)
	require.NoError(t, err)
	require.NotNil(t, firstPending)
	secondPending, err := second.FindLocalByRequest(context.Background(), command.UserID, command.RequestID)
	require.NoError(t, err)
	require.Nil(t, secondPending)
}

func TestBenchmarkReplicaCountsUsesHorizontalScaleMatrix(t *testing.T) {
	t.Setenv("SHOP_BENCH_REPLICAS", "")
	require.Equal(t, []int{1, 2, 4}, benchmarkReplicaCounts())
}

func TestBenchmarkReplicaCountsSupportsOverrideAndDeduplication(t *testing.T) {
	t.Setenv("SHOP_BENCH_REPLICAS", "4, 2, 4")
	require.Equal(t, []int{4, 2}, benchmarkReplicaCounts())
}

func TestBenchmarkReplicaCountsRejectsInvalidReplicaCount(t *testing.T) {
	t.Setenv("SHOP_BENCH_REPLICAS", "2,0")
	require.PanicsWithValue(t, "SHOP_BENCH_REPLICAS 包含无效副本数 \"0\"", func() {
		benchmarkReplicaCounts()
	})
}
