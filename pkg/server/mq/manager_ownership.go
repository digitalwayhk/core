// 本文件统一校验持续归属能力与生命周期，禁止不支持Provider或在线切换静默降级。
package mq

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/ownership"
)

func (m *MQManager) ownedProvider() (OwnedReliableMQProvider, error) {
	if m == nil {
		return nil, ErrNotConnected
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed || m.current == nil {
		return nil, ErrNotConnected
	}
	p, ok := m.current.(OwnedReliableMQProvider)
	if !ok || !p.SupportsScopedOwnership() || !validSharedKeyProvider(p) || m.switcher != nil {
		return nil, ownership.ErrUnsupported
	}
	m.sharedRequired.Store(true)
	return p, nil
}

// OwnershipStore 取得已校验的Provider归属/WS引用入口，禁止退到本地模拟。
func (m *MQManager) OwnershipStore(ctx context.Context) (ownership.Store, error) {
	p, err := m.ownedProvider()
	if err != nil {
		return nil, err
	}
	return p.OwnershipStore(ctx)
}

// ProvisionOwnership 显式维护初始化；正常服务启动不得用它重建丢失序列。
func (m *MQManager) ProvisionOwnership(ctx context.Context, manifest ownership.Manifest, floor int64) error {
	p, err := m.ownedProvider()
	if err != nil {
		return err
	}
	return p.ProvisionOwnership(ctx, manifest, floor)
}

// RecoverOwnership 仅供停稳全部owner后的显式维护；floor必须来自应用SQL权威核验。
func (m *MQManager) RecoverOwnership(ctx context.Context, manifest ownership.Manifest, floor int64) error {
	p, err := m.ownedProvider()
	if err != nil {
		return err
	}
	return p.RecoverOwnership(ctx, manifest, floor)
}

// BindOwnership 在维护窗口保全原group/PEL/游标绑定，不在线迁移活跃旧handler。
func (m *MQManager) BindOwnership(ctx context.Context, manifest ownership.Manifest) error {
	p, err := m.ownedProvider()
	if err != nil {
		return err
	}
	if err := m.validateOwnedLifecycle(manifest.Normalize()); err != nil {
		return err
	}
	return p.BindOwnership(ctx, manifest)
}

// UnbindOwnership 在所有持续归属租约结束后恢复原shared/W标记。
func (m *MQManager) UnbindOwnership(ctx context.Context, manifest ownership.Manifest) error {
	p, err := m.ownedProvider()
	if err != nil {
		return err
	}
	return p.UnbindOwnership(ctx, manifest)
}

func (m *MQManager) validateOwnedLifecycle(manifest ownership.Manifest) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	for _, subject := range manifest.Subjects {
		if err := m.lifecycle.validateRequiredGroup(subject, manifest.Group); err != nil {
			return err
		}
		policy := m.lifecycle.policyForSubject(subject)
		if policy == nil {
			continue
		}
		// 批次事务不能逐条应用有限次DLQ策略；未提供整批DLQ合同前明确拒绝。
		if policy.Retry.MaxDeliveries > 0 {
			return ownership.ErrUnsupported
		}
		if policy.Retry.MaxAckPending > 0 && manifest.Window > int64(policy.Retry.MaxAckPending) {
			return ErrLifecyclePolicyInvalid
		}
		if policy.Retry.HandlerTimeout > 0 && manifest.Timing.Execution > policy.Retry.HandlerTimeout {
			return ErrLifecyclePolicyInvalid
		}
	}
	return nil
}

// SubscribeOwnedBatch 把启动校验、原逻辑组与Provider原生机制连接起来。
func (m *MQManager) SubscribeOwnedBatch(ctx context.Context, options OwnedReliableOptions, handler func(context.Context, ownership.Credential, []*Message) error) (*ownership.Runtime, error) {
	p, err := m.ownedProvider()
	if err != nil {
		return nil, err
	}
	options.Manifest = options.Manifest.Normalize()
	if err := m.validateOwnedLifecycle(options.Manifest); err != nil {
		return nil, err
	}
	return p.SubscribeOwnedBatch(ctx, options, handler)
}
