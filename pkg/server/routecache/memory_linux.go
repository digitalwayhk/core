//go:build linux

package routecache

import (
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func physicalMemoryBytes() int64 {
	for _, path := range []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		text := strings.TrimSpace(string(data))
		if text == "max" {
			continue
		}
		if value, err := strconv.ParseInt(text, 10, 64); err == nil && value > 0 && value < 1<<60 {
			return value
		}
	}
	var info unix.Sysinfo_t
	if unix.Sysinfo(&info) != nil {
		return 0
	}
	return int64(info.Totalram) * int64(info.Unit)
}
