//go:build !windows && !plan9 && !js && !wasip1 && !aix

// 本文件与固定Badger版本使用相同的目录flock；仅处理规范命名、普通且确实零字节的mem文件。
package badgeropen

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func recoverEmptyMemtables(path string) (int, error) {
	dir, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer dir.Close()
	// 目录本身才是Badger的锁对象；LOCK文本不是互斥，不可删除LOCK或绕过活owner。
	if err = unix.Flock(int(dir.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return 0, fmt.Errorf("Cannot acquire directory lock for Badger empty memtable recovery: %w", err)
	}
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".mem") {
			continue
		}
		id, e := strconv.ParseInt(strings.TrimSuffix(name, ".mem"), 10, 64)
		if e != nil || id < 0 || fmt.Sprintf("%05d.mem", id) != name {
			continue
		}
		// 不跟随符号链接，不打开设备/FIFO；Fstat和Unlinkat均相对于已锁目录。
		if !entry.Type().IsRegular() {
			continue
		}
		fd, e := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if e != nil {
			return removed, e
		}
		var stat unix.Stat_t
		e = unix.Fstat(fd, &stat)
		if e == nil && stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Size == 0 && stat.Nlink == 1 {
			e = unix.Unlinkat(int(dir.Fd()), name, 0)
			if e == nil {
				removed++
			}
		}
		closeErr := unix.Close(fd)
		if e != nil {
			return removed, e
		}
		if closeErr != nil {
			return removed, closeErr
		}
	}
	if removed > 0 {
		if err = dir.Sync(); err != nil {
			return removed, err
		}
	}
	// 关闭预检锁后仍由badger.Open重新获取其正式锁；若其它进程抢先，只能正常拒绝，不绕锁。
	return removed, nil
}
