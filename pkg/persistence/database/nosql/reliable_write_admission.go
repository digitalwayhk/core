// Package nosql 提供 ReliableWriteStore 的并发、积压和磁盘背压控制。
package nosql

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/syncx"
)

var (
	// ErrWriteRejectedConcurrency 表示单实例可靠写入并发已达上限。
	ErrWriteRejectedConcurrency = errors.New("可靠写入并发已达上限")
	// ErrWriteRejectedPending 表示可靠写入 pending 已达到容量或持续时间上限。
	ErrWriteRejectedPending = errors.New("可靠写入积压已达上限")
	// ErrWriteRejectedDisk 表示 Badger 原生磁盘大小已达到硬上限。
	ErrWriteRejectedDisk = errors.New("可靠写入磁盘已达上限")
)

// WriteAdmissionMetrics 描述三类背压拒绝的累计次数。
type WriteAdmissionMetrics struct {
	RejectedConcurrency uint64
	RejectedPending     uint64
	RejectedDisk        uint64
}

// WriteAdmissionController 在进入 Group Commit 前执行可靠写入容量准入。
type WriteAdmissionController struct {
	limit               syncx.TimeoutLimit
	config              WriteAdmissionConfig
	backlogMu           sync.Mutex
	backlogAt           time.Time
	rejectedConcurrency atomic.Uint64
	rejectedPending     atomic.Uint64
	rejectedDisk        atomic.Uint64
}

func newWriteAdmissionController(config WriteAdmissionConfig) *WriteAdmissionController {
	if config.MaxConcurrent <= 0 {
		config.MaxConcurrent = 1
	}
	if config.AcquireTimeout <= 0 {
		config.AcquireTimeout = time.Millisecond
	}
	return &WriteAdmissionController{
		limit:  syncx.NewTimeoutLimit(config.MaxConcurrent),
		config: config,
	}
}

// Acquire 检查 pending 和磁盘边界后获取一个并发写入槽位。
func (controller *WriteAdmissionController) Acquire(
	ctx context.Context,
	pending int,
	diskBytes int64,
	now time.Time,
) (func(), error) {
	if controller == nil {
		return func() {}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if controller.config.HardDiskBytes > 0 && diskBytes >= controller.config.HardDiskBytes {
		controller.rejectedDisk.Add(1)
		return nil, fmt.Errorf("%w: bytes=%d limit=%d", ErrWriteRejectedDisk, diskBytes, controller.config.HardDiskBytes)
	}
	if controller.config.HardPending > 0 && pending >= controller.config.HardPending {
		controller.rejectedPending.Add(1)
		return nil, fmt.Errorf("%w: pending=%d hard_limit=%d", ErrWriteRejectedPending, pending, controller.config.HardPending)
	}
	if controller.backlogExpired(pending, now) {
		controller.rejectedPending.Add(1)
		return nil, fmt.Errorf("%w: pending=%d soft_limit=%d duration=%s",
			ErrWriteRejectedPending,
			pending,
			controller.config.SoftPending,
			controller.config.MaxBacklogDuration,
		)
	}
	timeout := controller.config.AcquireTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, ctx.Err()
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	if err := controller.limit.Borrow(timeout); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		controller.rejectedConcurrency.Add(1)
		return nil, fmt.Errorf("%w: timeout=%s", ErrWriteRejectedConcurrency, timeout)
	}
	var once sync.Once
	return func() {
		once.Do(func() { _ = controller.limit.Return() })
	}, nil
}

// Metrics 返回当前准入控制器的无锁累计指标。
func (controller *WriteAdmissionController) Metrics() WriteAdmissionMetrics {
	if controller == nil {
		return WriteAdmissionMetrics{}
	}
	return WriteAdmissionMetrics{
		RejectedConcurrency: controller.rejectedConcurrency.Load(),
		RejectedPending:     controller.rejectedPending.Load(),
		RejectedDisk:        controller.rejectedDisk.Load(),
	}
}

func (controller *WriteAdmissionController) backlogExpired(pending int, now time.Time) bool {
	controller.backlogMu.Lock()
	defer controller.backlogMu.Unlock()
	if controller.config.SoftPending <= 0 || pending < controller.config.SoftPending {
		controller.backlogAt = time.Time{}
		return false
	}
	if controller.backlogAt.IsZero() {
		controller.backlogAt = now
		return false
	}
	return controller.config.MaxBacklogDuration > 0 && now.Sub(controller.backlogAt) >= controller.config.MaxBacklogDuration
}
