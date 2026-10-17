// 本文件以 etcd 事务原子保护持久 owner 和带租约节点。
package cluster

import (
	"context"
	"encoding/json"
	clientv3 "go.etcd.io/etcd/client/v3"
	"math"
	"time"
)

func (p *EtcdProvider) ownerKey(n *NodeInfo) string { return p.prefix + "-owners/" + n.ID }
func (p *EtcdProvider) writeOwned(ctx context.Context, node *NodeInfo, o RegistrationOwner, acquire bool) error {
	n, err := ownedNode(node, o)
	if err != nil {
		return err
	}
	key := p.ownerKey(n)
	resp, err := p.client.Get(ctx, key)
	if err != nil {
		return err
	}
	cmp := clientv3.Compare(clientv3.Version(key), "=", 0)
	if len(resp.Kvs) == 0 {
		if !acquire {
			return ErrRegistrationAuthorityMissing
		}
	} else {
		old := string(resp.Kvs[0].Value)
		if old != o.token() && (!acquire || len(old) < 20 || old[:20] >= o.token()[:20]) {
			return ErrRegistrationFenced
		}
		cmp = clientv3.Compare(clientv3.Value(key), "=", old)
	}
	data, err := json.Marshal(n)
	if err != nil {
		return err
	}
	lease, err := p.client.Grant(ctx, max(1, int64(math.Ceil(p.ttl.Seconds()))))
	if err != nil {
		return err
	}
	result, err := p.client.Txn(ctx).If(cmp).Then(clientv3.OpPut(key, o.token()), clientv3.OpPut(p.nodeKey(n.ServiceName, n.ID), string(data), clientv3.WithLease(lease.ID))).Commit()
	if err != nil || !result.Succeeded {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		_, _ = p.client.Revoke(cleanup, lease.ID)
		cancel()
		if err != nil {
			return err
		}
		return ErrRegistrationFenced
	}
	return nil
}

// AcquireRegistration 原子建立/提升世代并发布节点。
func (p *EtcdProvider) AcquireRegistration(ctx context.Context, n *NodeInfo, o RegistrationOwner) error {
	return p.writeOwned(ctx, n, o, true)
}

// RenewRegistration 同一 owner 可以重建被删除或过期的节点键。
func (p *EtcdProvider) RenewRegistration(ctx context.Context, n *NodeInfo, o RegistrationOwner) error {
	return p.writeOwned(ctx, n, o, false)
}

// ReleaseRegistration 仅删除当前 owner 节点，不撤销其他实例的 lease。
func (p *EtcdProvider) ReleaseRegistration(ctx context.Context, n *NodeInfo, o RegistrationOwner) error {
	if _, err := ownedNode(n, o); err != nil {
		return err
	}
	result, err := p.client.Txn(ctx).If(clientv3.Compare(clientv3.Value(p.ownerKey(n)), "=", o.token())).Then(clientv3.OpDelete(p.nodeKey(n.ServiceName, n.ID))).Commit()
	if err != nil {
		return err
	}
	if !result.Succeeded {
		return ErrRegistrationFenced
	}
	return nil
}

// RegistrationTTL 返回实际注册有效期，供本地准入保守计时。
func (p *EtcdProvider) RegistrationTTL() time.Duration { return p.ttl }
