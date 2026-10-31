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
	Step    func(context.Context, Credential) (bool, error)
	Observe func(context.Context, Credential) (Observation, error)
}
