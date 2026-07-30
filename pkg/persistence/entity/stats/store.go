package stats

import (
	"context"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/types"
)

// Store 保存任务刷新的统计快照；API 只读，无 TTL 过期删除（由 Put 覆盖）。
type Store struct {
	mu   sync.RWMutex
	data map[string]Snapshot
}

// NewStore 创建内存快照存储。
func NewStore() *Store {
	return &Store{data: make(map[string]Snapshot)}
}

// Put 写入或覆盖快照。
func (s *Store) Put(snap Snapshot) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = make(map[string]Snapshot)
	}
	if snap.ComputedAt.IsZero() {
		snap.ComputedAt = time.Now().UTC()
	}
	s.data[snap.Code] = snap
}

// Get 读取快照。
func (s *Store) Get(code string) (Snapshot, bool) {
	if s == nil {
		return Snapshot{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap, ok := s.data[code]
	return snap, ok
}

// List 返回所有快照的浅拷贝。
func (s *Store) List() []Snapshot {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Snapshot, 0, len(s.data))
	for _, v := range s.data {
		out = append(out, v)
	}
	return out
}

// DefaultStore 进程默认 Store。
var DefaultStore = NewStore()

// Refresh 使用 OLTP 引擎执行 Spec 并写入 Store（兼容旧调用）。
// 新代码请优先 RefreshWithEngine + 全局 Engine 配置。
// 失败时若已有成功快照则保留旧数据并返回错误。
func Refresh(ctx context.Context, store *Store, action types.IDataAction, spec StatSpec, opt ExecOptions) (Snapshot, error) {
	return RefreshWithEngine(ctx, store, NewOLTPEngine(action), spec, opt)
}
