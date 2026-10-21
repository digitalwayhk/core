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
	SupportsScopedOwnership() bool
	OwnershipStore(context.Context) (ownership.Store, error)
	ProvisionOwnership(context.Context, ownership.Manifest, int64) error
	RecoverOwnership(context.Context, ownership.Manifest, int64) error
	BindOwnership(context.Context, ownership.Manifest) error
	UnbindOwnership(context.Context, ownership.Manifest) error
	SubscribeOwnedBatch(context.Context, OwnedReliableOptions, func(context.Context, ownership.Credential, []*Message) error) (*ownership.Runtime, error)
}
