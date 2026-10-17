// 本文件定义编号账本的原子存储接口。
package identity

import (
	"context"
	"errors"
)

// ErrAuthorityMissing 表示持久权威丢失，不允许业务启动自动重建。
var ErrAuthorityMissing = errors.New("identity: authority missing; explicit initialization or recovery required")

// ErrAuthorityInvalid 表示持久世代、布局或账本内部索引损坏，不属于暂时连接错误。
var ErrAuthorityInvalid = errors.New("identity: authority epoch or schema invalid")

// ErrOwnerConflict 表示实例已由另一启动世代持有。
var ErrOwnerConflict = errors.New("identity: boot owner conflict")

// ErrBindingConflict 表示实际 worker 或持久绑定冲突。
var ErrBindingConflict = errors.New("identity: generator binding conflict")

// Store 通过原子比较整个小容量账本，避免分配/世代/时间状态部分提交。
type Store interface {
	Load(context.Context) (string, error)
	CompareAndSwap(context.Context, string, string) (bool, error)
	Close() error
}

// ErrAdmissionUnavailable 表示临时未就绪，不能把短断连转为永久生成器故障。
var ErrAdmissionUnavailable = errors.New("identity: service admission unavailable")
