// 本文件将Ready租约有效性与仍在退出的业务worker区分，防止观测误报。
package ownership

import "sync"

type runtimeReadyState struct {
	mu    sync.Mutex
	ready bool
	lost  bool
}

func (s *runtimeReadyState) lose(r *Runtime) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lost = true
	if s.ready {
		s.ready = false
		r.ready.Add(-1)
	}
}
