// 本文件仅测统计入口本身的开销；不能把微基准结果当成业务容量收益。
package oltp

import "testing"

// BenchmarkCleanupStatsObservation 比较原入口与显式编译overlay移除强制GC后的单次统计成本。
func BenchmarkCleanupStatsObservation(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		logCleanupStats()
	}
}
