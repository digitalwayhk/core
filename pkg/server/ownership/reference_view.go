// 本文件拒绝迟到快照覆盖更新的退订，支持跨Gateway引用收敛。
package ownership

import "sync"

// ReferenceView 只接受更新或相同revision的恢复快照。
type ReferenceView struct {
	mu         sync.Mutex
	revision   int64
	references []Reference
}

// NewReferenceView 创建空引用视图。
func NewReferenceView() *ReferenceView { return &ReferenceView{} }

// Apply 原子替换完整集合；旧revision不能复活已经移除的引用。
func (v *ReferenceView) Apply(snapshot ReferenceSnapshot) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if snapshot.Revision <= 0 {
		return ErrUnknown
	}
	if snapshot.Revision < v.revision {
		return ErrStaleSnapshot
	}
	v.revision = snapshot.Revision
	v.references = append([]Reference(nil), snapshot.References...)
	return nil
}

// References 返回防止调用方修改的引用快照。
func (v *ReferenceView) References() []Reference {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]Reference(nil), v.references...)
}
