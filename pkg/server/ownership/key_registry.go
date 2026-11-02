// 本文件定义显式动态目录的冻结预算；注册只代表技术登记，不表示Ready或业务提交。
package ownership

import (
	"context"
	"time"
)

// DynamicKeyPolicy 显式启用追加目录；所有预算冻结，不能在线放大。
type DynamicKeyPolicy struct {
	MaxKeys         int
	PageSize        int
	RefreshInterval time.Duration
	// UnregisteredWindow 为原W之外的有限原PEL预留；仍保留原消息和ordering-key屏障。
	UnregisteredWindow int64
}

// Validate 拒绝无限目录、无限扫描或无限预取。
func (p DynamicKeyPolicy) Validate() error {
	if p.MaxKeys < 1 || p.MaxKeys > 256 || p.PageSize < 1 || p.PageSize > p.MaxKeys || p.RefreshInterval < time.Millisecond || p.RefreshInterval > time.Minute || p.UnregisteredWindow < 1 || p.UnregisteredWindow > 1024 {
		return ErrCapacity
	}
	return nil
}

// PendingWindow 返回原组PEL的实际有限总预算，固定模式保持原W。
func (m Manifest) PendingWindow() int64 {
	if m.DynamicKeys != nil {
		return m.Window + m.DynamicKeys.UnregisteredWindow
	}
	return m.Window
}

// KeyPage 是只追加目录的稳定位置分页，不是业务消息游标。
type KeyPage struct {
	Keys  []string
	Next  int
	Total int
}

// KeyRegistry 是Provider的可选运行期受信注册与有界发现合同。
// 调用方必须持有原冻结Manifest；普通消息与Gateway输入没有注册权限。
type KeyRegistry interface {
	RegisterKey(context.Context, Manifest, string) error
	ListKeys(context.Context, Manifest, int, int) (KeyPage, error)
}
