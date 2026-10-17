//go:build linux || darwin

// 本文件使用独立 inode 的平台文件锁保护本地持久卷。
package identity

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
)

func lockFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("identity: state already locked: %w", err)
	}
	return f, nil
}
func unlockFile(f *os.File) error {
	if f == nil {
		return nil
	}
	err := unix.Flock(int(f.Fd()), unix.LOCK_UN)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
