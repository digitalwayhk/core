// 本文件验证阶段指标沿用Runtime白名单且不把累计耗时冒充单次延迟。
package orderservice

import (
	"context"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// TestWritePathSnapshotPreservesCumulativeFacts 验证提供者只读已有累计提交与同步事实。
func TestWritePathSnapshotPreservesCumulativeFacts(t *testing.T) {
	provider := writePathProvider{snapshot: func() nosql.ReliableWriteMetrics {
		return nosql.ReliableWriteMetrics{
			Batch: nosql.BatchCommitMetrics{Committed: 12, Batches: 3, TotalCommitDuration: 2 * time.Second},
			Sync:  nosql.SyncMetrics{Attempts: 4, TotalDuration: 5 * time.Second},
		}
	}}
	snapshot := provider.RuntimeMetricSnapshot(context.Background())
	require.Equal(t, "ok", snapshot.State)
	require.EqualValues(t, 12, snapshot.Gauges["local_committed_total"])
	require.EqualValues(t, 2, snapshot.Gauges["local_commit_seconds_total"])
	require.EqualValues(t, 5, snapshot.Gauges["sync_seconds_total"])
}
