// 本文件定义与 MachineID 无关的稳定注册所有权能力。
package cluster

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrRegistrationFenced 表示注册已由更高启动世代持有。
var ErrRegistrationFenced = errors.New("cluster: registration fenced by another boot")

// ErrRegistrationAuthorityMissing 表示持久注册世代丢失，需要显式恢复。
var ErrRegistrationAuthorityMissing = errors.New("cluster: registration authority missing")

// RegistrationOwner 标识内部启动世代，不参与服务发现身份。
type RegistrationOwner struct {
	BootID string
	Epoch  uint64
}

func (o RegistrationOwner) token() string { return fmt.Sprintf("%020d:%s", o.Epoch, o.BootID) }

// OwnedRegistry 原子比较启动世代并写入/撤销实际发现记录。
type OwnedRegistry interface {
	RegistrationTTL() time.Duration
	AcquireRegistration(context.Context, *NodeInfo, RegistrationOwner) error
	RenewRegistration(context.Context, *NodeInfo, RegistrationOwner) error
	ReleaseRegistration(context.Context, *NodeInfo, RegistrationOwner) error
}

func ownedNode(node *NodeInfo, owner RegistrationOwner) (*NodeInfo, error) {
	if node == nil || node.ID == "" || node.ServiceName == "" || node.ServiceInstanceID == "" || owner.BootID == "" || owner.Epoch == 0 {
		return nil, errors.New("cluster: invalid owned registration")
	}
	n := cloneNode(node)
	n.StableBootID = owner.BootID
	n.StableBootEpoch = owner.Epoch
	now := time.Now().UTC()
	if n.RegisteredAt.IsZero() {
		n.RegisteredAt = now
	}
	n.LastHeartbeat = now
	n.Status = NodeStatusRunning
	if n.Weight <= 0 {
		n.Weight = 1
	}
	return n, nil
}
