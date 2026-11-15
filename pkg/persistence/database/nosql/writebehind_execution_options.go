// 本文件定义绑定时显式选择的有界独立组执行；不修改持久policy或卷格式。
package nosql

// WriteBehindExecutionOptions控制当前store的target执行；零值保留旧整批语义。
// IndependentGroups为1..64时使用固定数量worker，每次SyncBatch仅接收一个应用顺序组的组首。
// target须支持并发调用；各次调用独立返回真实结果。不能依赖跨组原子事务或跨调用共享可变批次。
// 此选项仅用于UseWriteBehindWithFailureHandlerOptions，不复用旧SyncMaxConcurrency字段。
type WriteBehindExecutionOptions struct {
	IndependentGroups int
}

func (o WriteBehindExecutionOptions) validate() error {
	if o.IndependentGroups < 0 || o.IndependentGroups > 64 {
		return ErrWriteBehindDisposition
	}
	return nil
}
