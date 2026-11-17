// 本文件定义框架归属运行时回调，应用只提供事务和恢复逻辑，不自建worker。
package ownership

import "context"

// Options 把持续归属的生命周期与应用SQL事务、恢复和有界执行相连。
type Options struct {
	Manifest   Manifest
	InstanceID string
	SessionID  string
	MaxOwned   int
	// Activate必须在应用SQL事务中ActivateFence，并等待实际旧事务的锁。
	Activate func(context.Context, Credential) error
	// Restore在Ready前及后续revision变更时收敛活跃引用。
	Restore func(context.Context, Credential, ReferenceSnapshot) error
	// Step仅在真实进展后返回true；无可执行项返回false,nil，错误不能借BlockedDue免除停滞。
	Step func(context.Context, Credential) (bool, error)
	// GroupWork显式启用同租约内有限独立组执行；与Step二选一。
	GroupWork *GroupWorkOptions
	// Observe完整覆盖本Runtime输入；主计数未知为-1，BlockedDue只接受已核实的非负子集。
	Observe func(context.Context, Credential) (Observation, error)
}
