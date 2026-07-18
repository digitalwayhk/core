package models

import (
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/syncx"
)

const (
	// orderWriteMaxConcurrent 把单实例确认中的下单数限制在实测甜点附近，
	// 避免 1000+ 并发只堆高 P95/P99；跨过该量级应优先水平扩容。
	orderWriteMaxConcurrent = 500
	// orderWritePendingSoftLimit 允许短时 SQLite 抖动，但会启动持续积压计时。
	orderWritePendingSoftLimit = 10_000
	// orderWritePendingHardLimit 防止远端长时间不可用时无界吃满本地磁盘。
	orderWritePendingHardLimit = 50_000
	// orderWriteMaxBacklogDuration 超过软阈值后的最长容忍时间。
	orderWriteMaxBacklogDuration = 30 * time.Second
	// orderWriteHardDiskBytes 是示例保护值，实际服务应根据磁盘配额和订单大小配置。
	orderWriteHardDiskBytes = int64(1 << 30)
	// orderWriteAcquireTimeout 允许超出 500 的客户端并发在边界外短暂排队。
	orderWriteAcquireTimeout = 2 * time.Second
)

type orderWriteGuardConfig struct {
	MaxConcurrent      int
	SoftPending        int
	HardPending        int
	MaxBacklogDuration time.Duration
	HardDiskBytes      int64
	AcquireTimeout     time.Duration
}

// orderWriteGuard 复用 go-zero TimeoutLimit 控制在途写入，并对持续积压和磁盘上限执行背压。
type orderWriteGuard struct {
	limit               syncx.TimeoutLimit
	config              orderWriteGuardConfig
	backlogMu           sync.Mutex
	backlogAt           time.Time
	rejectedConcurrency atomic.Uint64
	rejectedBacklog     atomic.Uint64
	rejectedDisk        atomic.Uint64
}

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
	// 环境变量仅覆盖示例保护阈值，便于高并发基准与生产演示分离默认值。
	// 未设置时保持 README 中的示例数字。
	if value, ok := envInt("SHOP_ORDER_WRITE_MAX_CONCURRENT"); ok {
		config.MaxConcurrent = value
	}
	if value, ok := envInt("SHOP_ORDER_WRITE_SOFT_PENDING"); ok {
		config.SoftPending = value
	}
	if value, ok := envInt("SHOP_ORDER_WRITE_HARD_PENDING"); ok {
		config.HardPending = value
	}
	if value, ok := envInt64("SHOP_ORDER_WRITE_HARD_DISK_BYTES"); ok {
		config.HardDiskBytes = value
	}
	if value, ok := envDurationMillis("SHOP_ORDER_WRITE_ACQUIRE_TIMEOUT_MS"); ok {
		config.AcquireTimeout = value
	}
	if value, ok := envDurationMillis("SHOP_ORDER_WRITE_BACKLOG_DURATION_MS"); ok {
		config.MaxBacklogDuration = value
	}
	return config
}

func envInt(name string) (int, bool) {
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

func envInt64(name string) (int64, bool) {
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

func envDurationMillis(name string) (time.Duration, bool) {
	value, ok := envInt(name)
	if !ok {
		return 0, false
	}
	return time.Duration(value) * time.Millisecond, true
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

// Acquire 检查当前容量并尝试获取一个写入槽位。
// 返回的 release 是幂等的，调用方必须在离开写入热路径时调用。
func (g *orderWriteGuard) Acquire(pending int, diskBytes int64, now time.Time) (func(), error) {
	if g == nil {
		return func() {}, nil
	}
	if g.config.HardDiskBytes > 0 && diskBytes >= g.config.HardDiskBytes {
		g.rejectedDisk.Add(1)
		return nil, NewBusinessError("订单本地存储磁盘使用已达硬上限，请稍后重试")
	}
	if g.config.HardPending > 0 && pending >= g.config.HardPending {
		g.rejectedBacklog.Add(1)
		return nil, NewBusinessError("订单同步积压已达硬上限，请稍后重试")
	}
	if g.backlogExpired(pending, now) {
		g.rejectedBacklog.Add(1)
		return nil, NewBusinessError("订单同步持续积压超时，请稍后重试")
	}
	if err := g.limit.Borrow(g.config.AcquireTimeout); err != nil {
		g.rejectedConcurrency.Add(1)
		return nil, NewBusinessError("订单写入已达单实例并发上限，请稍后重试")
	}
	var once sync.Once
	return func() {
		once.Do(func() { _ = g.limit.Return() })
	}, nil
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
