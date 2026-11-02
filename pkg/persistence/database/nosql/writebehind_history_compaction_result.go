// 本文件定义有界终态维护一页的实际提交结果，不把逻辑删除当物理空间释放。
package nosql

// WriteBehindHistoryCompaction只累计真实提交的维护项；Deferred含诊断宽限、审计及未解除依赖。
type WriteBehindHistoryCompaction struct {
	Scanned       int
	Stripped      int
	Removed       int
	Deferred      int
	CycleComplete bool
}
