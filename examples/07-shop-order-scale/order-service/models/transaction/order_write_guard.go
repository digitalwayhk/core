// Package transaction 提供 07 订单写入热路径的背压保护能力。
package transaction

import (
	"errors"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/syncx"
)

const (
	orderWriteMaxConcurrent            = 500
	orderWritePendingSoftLimit         = 10_000
	orderWritePendingHardLimit         = 50_000
	orderWriteMaxBacklogDuration       = 30 * time.Second
	orderWriteHardDiskBytes      int64 = 1 << 30
	orderWriteAcquireTimeout           = 2 * time.Second
)

type orderWriteGuardConfig struct {
	MaxConcurrent      int
	SoftPending        int
	HardPending        int
	MaxBacklogDuration time.Duration
	HardDiskBytes      int64
	AcquireTimeout     time.Duration
}

type orderWriteGuard struct {
	limit               syncx.TimeoutLimit
	config              orderWriteGuardConfig
	backlogMu           sync.Mutex
	backlogAt           time.Time
	rejectedConcurrency atomic.Uint64
	rejectedBacklog     atomic.Uint64
	rejectedDisk        atomic.Uint64
}

// OrderWriteGuardSnapshot 是订单写入背压指标快照。
type OrderWriteGuardSnapshot struct {
	RejectedConcurrency uint64
	RejectedBacklog     uint64
	RejectedDisk        uint64
}

func defaultOrderWriteGuardConfig() orderWriteGuardConfig {
	config := orderWriteGuardConfig{
		MaxConcurrent:      orderWriteMaxConcurrent,
		SoftPending:        orderWritePendingSoftLimit,
		HardPending:        orderWritePendingHardLimit,
		MaxBacklogDuration: orderWriteMaxBacklogDuration,
		HardDiskBytes:      orderWriteHardDiskBytes,
		AcquireTimeout:     orderWriteAcquireTimeout,
	}
	if value, ok := envIntForGuard("SHOP_ORDER_WRITE_MAX_CONCURRENT"); ok {
		config.MaxConcurrent = value
	}
	if value, ok := envIntForGuard("SHOP_ORDER_WRITE_SOFT_PENDING"); ok {
		config.SoftPending = value
	}
	if value, ok := envIntForGuard("SHOP_ORDER_WRITE_HARD_PENDING"); ok {
		config.HardPending = value
	}
	if value, ok := envInt64ForGuard("SHOP_ORDER_WRITE_HARD_DISK_BYTES"); ok {
		config.HardDiskBytes = value
	}
	if value, ok := envDurationMillisForGuard("SHOP_ORDER_WRITE_ACQUIRE_TIMEOUT_MS"); ok {
		config.AcquireTimeout = value
	}
	if value, ok := envDurationMillisForGuard("SHOP_ORDER_WRITE_BACKLOG_DURATION_MS"); ok {
		config.MaxBacklogDuration = value
	}
	return config
}

func newOrderWriteGuard(config orderWriteGuardConfig) *orderWriteGuard {
	if config.MaxConcurrent <= 0 {
		config.MaxConcurrent = 1
	}
	if config.AcquireTimeout <= 0 {
		config.AcquireTimeout = time.Millisecond
	}
	return &orderWriteGuard{limit: syncx.NewTimeoutLimit(config.MaxConcurrent), config: config}
}

func (g *orderWriteGuard) Acquire(pending int, diskBytes int64, now time.Time) (func(), error) {
	if g == nil {
		return func() {}, nil
	}
	if g.config.HardDiskBytes > 0 && diskBytes >= g.config.HardDiskBytes {
		g.rejectedDisk.Add(1)
		return nil, errors.New("订单本地存储磁盘使用已达硬上限，请稍后重试")
	}
	if g.config.HardPending > 0 && pending >= g.config.HardPending {
		g.rejectedBacklog.Add(1)
		return nil, errors.New("订单同步积压已达硬上限，请稍后重试")
	}
	if g.backlogExpired(pending, now) {
		g.rejectedBacklog.Add(1)
		return nil, errors.New("订单同步持续积压超时，请稍后重试")
	}
	if err := g.limit.Borrow(g.config.AcquireTimeout); err != nil {
		g.rejectedConcurrency.Add(1)
		return nil, errors.New("订单写入已达单实例并发上限，请稍后重试")
	}
	var once sync.Once
	return func() { once.Do(func() { _ = g.limit.Return() }) }, nil
}

func (g *orderWriteGuard) Snapshot() OrderWriteGuardSnapshot {
	if g == nil {
		return OrderWriteGuardSnapshot{}
	}
	return OrderWriteGuardSnapshot{
		RejectedConcurrency: g.rejectedConcurrency.Load(),
		RejectedBacklog:     g.rejectedBacklog.Load(),
		RejectedDisk:        g.rejectedDisk.Load(),
	}
}

func (g *orderWriteGuard) backlogExpired(pending int, now time.Time) bool {
	g.backlogMu.Lock()
	defer g.backlogMu.Unlock()
	if g.config.SoftPending <= 0 || pending < g.config.SoftPending {
		g.backlogAt = time.Time{}
		return false
	}
	if g.backlogAt.IsZero() {
		g.backlogAt = now
		return false
	}
	return g.config.MaxBacklogDuration > 0 && now.Sub(g.backlogAt) >= g.config.MaxBacklogDuration
}

func envIntForGuard(name string) (int, bool) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, false
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, false
	}
	return value, true
}

func envInt64ForGuard(name string) (int64, bool) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, false
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, false
	}
	return value, true
}

func envDurationMillisForGuard(name string) (time.Duration, bool) {
	value, ok := envIntForGuard(name)
	if !ok {
		return 0, false
	}
	return time.Duration(value) * time.Millisecond, true
}
