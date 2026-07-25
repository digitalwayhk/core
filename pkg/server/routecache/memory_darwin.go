//go:build darwin

package routecache

import "golang.org/x/sys/unix"

func physicalMemoryBytes() int64 {
	value, err := unix.SysctlUint64("hw.memsize")
	if err != nil || value > uint64(^uint64(0)>>1) {
		return 0
	}
	return int64(value)
}
