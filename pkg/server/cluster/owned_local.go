// 本文件提供 local Provider 的原子稳定注册，不检查生成器槽位。
package cluster

import (
	"context"
	"time"
)

func (p *LocalProvider) writeOwned(ctx context.Context, node *NodeInfo, owner RegistrationOwner, acquire bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	n, err := ownedNode(node, owner)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.owners == nil {
		p.owners = map[string]RegistrationOwner{}
	}
	old, ok := p.owners[n.ID]
	if !ok && !acquire {
		return ErrRegistrationAuthorityMissing
	}
	if ok && old != owner && (!acquire || old.Epoch >= owner.Epoch) {
		return ErrRegistrationFenced
	}
	p.owners[n.ID] = owner
	p.nodes[n.ID] = n
	p.notifyWatchers(n.ServiceName)
	return nil
}

// AcquireRegistration 取得或升级本实例的持久运行世代。
func (p *LocalProvider) AcquireRegistration(ctx context.Context, n *NodeInfo, o RegistrationOwner) error {
	return p.writeOwned(ctx, n, o, true)
}

// RenewRegistration 按同一 owner 重建节点，不重新分配 MachineID。
func (p *LocalProvider) RenewRegistration(ctx context.Context, n *NodeInfo, o RegistrationOwner) error {
	return p.writeOwned(ctx, n, o, false)
}

// ReleaseRegistration 仅删除当前 owner 的节点，保留世代记录。
func (p *LocalProvider) ReleaseRegistration(ctx context.Context, n *NodeInfo, o RegistrationOwner) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := ownedNode(n, o); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	old, ok := p.owners[n.ID]
	if !ok {
		return ErrRegistrationAuthorityMissing
	}
	if old != o {
		return ErrRegistrationFenced
	}
	delete(p.nodes, n.ID)
	p.notifyWatchers(n.ServiceName)
	return nil
}

// RegistrationTTL 返回实际注册有效期，供本地准入保守计时。
func (p *LocalProvider) RegistrationTTL() time.Duration { return p.heartbeatTimeout }
