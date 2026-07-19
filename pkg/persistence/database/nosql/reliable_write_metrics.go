// Package nosql 定义 ReliableWriteStore 的统一只读指标快照。
package nosql

import "time"

// ReliableWriteMetrics 汇总本地积压、磁盘、Group Commit、准入和同步指标。
type ReliableWriteMetrics struct {
	StartedAt       time.Time
	Pending         int
	BadgerLSMBytes  int64
	BadgerVLogBytes int64
	Batch           BatchCommitMetrics
	Admission       WriteAdmissionMetrics
	Sync            SyncMetrics
}
