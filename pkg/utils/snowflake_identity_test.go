// 本文件验证稳定模式的实际 WorkerID 映射、范围和兼容边界。
package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStableWorkerEncodingPreservesValidLegacyIDs 验证合法映射不改变现有布局。
func TestStableWorkerEncodingPreservesValidLegacyIDs(t *testing.T) {
	for _, tc := range []struct {
		machine, dc uint
		want        uint16
	}{{0, 1, 10}, {1, 4, 41}, {3, 6, 63}, {0, 0, 0}} {
		got, err := StableSnowflakeWorkerID(tc.machine, tc.dc)
		require.NoError(t, err)
		require.Equal(t, tc.want, got)
	}
}

// TestStableWorkerEncodingRejectsAliasAndOverflow 验证越界先于截断，合法别名留给权威判冲突。
func TestStableWorkerEncodingRejectsAliasAndOverflow(t *testing.T) {
	for _, tc := range []struct{ machine, dc uint }{{10, 1}, {0, 11}, {4, 6}, {1000, 1000}, {^uint(0), ^uint(0)}} {
		_, err := StableSnowflakeWorkerID(tc.machine, tc.dc)
		require.Error(t, err)
	}
	a, err := StableSnowflakeWorkerID(10, 0)
	require.NoError(t, err)
	b, err := StableSnowflakeWorkerID(0, 1)
	require.NoError(t, err)
	require.Equal(t, a, b, "分配器必须按实际 worker 判冲突，不能按不同配置对放行")
}
