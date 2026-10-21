// 本文件把跨主题持续归属与原单主题SharedKeys能力分开声明。
package mq

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/ownership"
)

// OwnedReliableOptions 使用原group和W，业务只声明归属选择及SQL/恢复回调。
type OwnedReliableOptions struct {
	Manifest   ownership.Manifest
	InstanceID string
	SessionID  string
	MaxOwned   int
	Key        func(*Message) (string, error)
	Activate   func(context.Context, ownership.Credential) error
	Restore    func(context.Context, ownership.Credential, ownership.ReferenceSnapshot) error
	// Due/NoDueTasks二选一，不能把未接线的到期任务假装为0。
	Due        func(context.Context, ownership.Credential) (int64, error)
	NoDueTasks bool
}

// OwnedReliableMQProvider 显式支持持续归属、业务批次及应用事务凭据。
type OwnedReliableMQProvider interface {
	SharedKeyReliableMQProvider
	// SupportsScopedOwnership 明确声明持续归属能力，不能从普通可靠ACK推导。
	SupportsScopedOwnership() bool
	// OwnershipStore 取得Provider中立的凭据及框架会话引用访问面。
	OwnershipStore(context.Context) (ownership.Store, error)
	// ProvisionOwnership 显式维护初始化，不在正常启动中自动重建元数据。
	ProvisionOwnership(context.Context, ownership.Manifest, int64) error
	// RecoverOwnership 仅在停稳全部owner后按应用SQL正下界维护恢复。
	RecoverOwnership(context.Context, ownership.Manifest, int64) error
	// BindOwnership 保全原Stream、逻辑组、PEL及游标绑定新合同。
	BindOwnership(context.Context, ownership.Manifest) error
	// UnbindOwnership 无活动归属且实际回调退出后恢复原shared模式。
	UnbindOwnership(context.Context, ownership.Manifest) error
	// SubscribeOwnedBatch 按原组有限读取同归属批次，持久成功后才fenced ACK。
	SubscribeOwnedBatch(context.Context, OwnedReliableOptions, func(context.Context, ownership.Credential, []*Message) error) (*ownership.Runtime, error)
}
