package routecache

import (
	"math"
	"runtime/debug"
	"sync"

	"github.com/digitalwayhk/core/pkg/server/config"
)

type processL1Budget struct {
	mu    sync.Mutex
	max   int64
	used  int64
	users int
}

var sharedL1Budget processL1Budget

func (b *processL1Budget) acquire(maxBytes int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.users == 0 || maxBytes < b.max {
		b.max = maxBytes
	}
	b.users++
}

func (b *processL1Budget) reserve(bytes int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if bytes <= 0 || b.used+bytes > b.max {
		return false
	}
	b.used += bytes
	return true
}

func (b *processL1Budget) release(bytes int64) {
	b.mu.Lock()
	b.used -= bytes
	if b.used < 0 {
		b.used = 0
	}
	b.mu.Unlock()
}

func (b *processL1Budget) closeUser() {
	b.mu.Lock()
	if b.users > 0 {
		b.users--
	}
	if b.users == 0 {
		b.max = 0
		b.used = 0
	}
	b.mu.Unlock()
}

const (
	minAutoL1Bytes = int64(16 << 20)
	maxAutoL1Bytes = int64(256 << 20)
	defaultMemory  = int64(8 << 30)
)

func resolveL1Config(cfg config.RouteCacheL1Config) config.RouteCacheL1Config {
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = autoL1Bytes(effectiveMemoryBytes())
	}
	if cfg.MaxEntries == 0 {
		cfg.MaxEntries = int(cfg.MaxBytes / (4 << 10))
		if cfg.MaxEntries < 256 {
			cfg.MaxEntries = 256
		}
		if cfg.MaxEntries > 10000 {
			cfg.MaxEntries = 10000
		}
	}
	return cfg
}

func autoL1Bytes(effective int64) int64 {
	budget := effective / 50
	if budget < minAutoL1Bytes {
		return minAutoL1Bytes
	}
	if budget > maxAutoL1Bytes {
		return maxAutoL1Bytes
	}
	return budget
}

func effectiveMemoryBytes() int64 {
	limit := debug.SetMemoryLimit(-1)
	physical := physicalMemoryBytes()
	if limit > 0 && limit < math.MaxInt64 && (physical <= 0 || limit < physical) {
		return limit
	}
	if physical > 0 {
		return physical
	}
	return defaultMemory
}
