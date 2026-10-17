// 本文件提供显式开发模式的本机持久编号账本。
package identity

import (
	"context"
	"errors"
	"os"
	"time"
)

// FileStore 使用独立旁路锁串行比较写入；不承诺跨主机文件系统语义。
type FileStore struct{ path string }

// NewFileStore 使用已配置的绝对路径，不隐式初始化账本。
func NewFileStore(path string) *FileStore { return &FileStore{path: path} }
func (s *FileStore) Load(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrAuthorityMissing
	}
	return string(data), err
}
func (s *FileStore) CompareAndSwap(ctx context.Context, old, next string) (bool, error) {
	var lock *os.File
	var err error
	for attempt := 0; attempt < 1000; attempt++ {
		if err = ctx.Err(); err != nil {
			return false, err
		}
		lock, err = lockFile(s.path + ".lock")
		if err == nil {
			break
		}
		timer := time.NewTimer(2 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, ctx.Err()
		case <-timer.C:
		}
	}
	if err != nil {
		return false, err
	}
	defer unlockFile(lock)
	raw, err := s.Load(ctx)
	if errors.Is(err, ErrAuthorityMissing) {
		raw = ""
	} else if err != nil {
		return false, err
	}
	if raw != old {
		return false, nil
	}
	return true, atomicWrite(s.path, []byte(next))
}
func (s *FileStore) Close() error { return nil }
