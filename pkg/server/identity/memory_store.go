// 本文件仅提供显式开发模式的进程内编号权威。
package identity

import (
	"context"
	"sync"
)

// MemoryStore 只承诺单进程范围的原子性，不是跨进程编号中心。
type MemoryStore struct {
	mu   sync.Mutex
	data string
}

// NewMemoryStore 创建独立的开发账本。
func NewMemoryStore() *MemoryStore { return &MemoryStore{} }
func (s *MemoryStore) Load(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == "" {
		return "", ErrAuthorityMissing
	}
	return s.data, nil
}
func (s *MemoryStore) CompareAndSwap(ctx context.Context, old, next string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data != old {
		return false, nil
	}
	s.data = next
	return true, nil
}
func (s *MemoryStore) Close() error { return nil }
