// Package nosql 验证可靠写入并发、持续积压和磁盘背压的 typed error 与指标。
package nosql

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWriteAdmissionRejectsHardPendingAndDiskLimits(t *testing.T) {
	controller := newWriteAdmissionController(WriteAdmissionConfig{
		MaxConcurrent:  1,
		AcquireTimeout: time.Millisecond,
		HardPending:    2,
		HardDiskBytes:  100,
	})

	_, err := controller.Acquire(context.Background(), 2, 0, time.Now())
	require.ErrorIs(t, err, ErrWriteRejectedPending)
	_, err = controller.Acquire(context.Background(), 0, 100, time.Now())
	require.ErrorIs(t, err, ErrWriteRejectedDisk)
	metrics := controller.Metrics()
	require.Equal(t, uint64(1), metrics.RejectedPending)
	require.Equal(t, uint64(1), metrics.RejectedDisk)
}

func TestWriteAdmissionRejectsSustainedSoftPendingAndResetsAfterRecovery(t *testing.T) {
	controller := newWriteAdmissionController(WriteAdmissionConfig{
		MaxConcurrent:      1,
		AcquireTimeout:     time.Millisecond,
		SoftPending:        2,
		MaxBacklogDuration: 10 * time.Second,
	})
	started := time.Unix(100, 0)

	release, err := controller.Acquire(context.Background(), 2, 0, started)
	require.NoError(t, err)
	release()
	release, err = controller.Acquire(context.Background(), 2, 0, started.Add(9*time.Second))
	require.NoError(t, err)
	release()
	_, err = controller.Acquire(context.Background(), 2, 0, started.Add(10*time.Second))
	require.ErrorIs(t, err, ErrWriteRejectedPending)

	release, err = controller.Acquire(context.Background(), 1, 0, started.Add(11*time.Second))
	require.NoError(t, err)
	release()
	release, err = controller.Acquire(context.Background(), 2, 0, started.Add(20*time.Second))
	require.NoError(t, err, "低于 soft limit 后必须重置持续积压计时")
	release()
}

func TestWriteAdmissionRejectsConcurrencyAndReleaseIsIdempotent(t *testing.T) {
	controller := newWriteAdmissionController(WriteAdmissionConfig{
		MaxConcurrent:  1,
		AcquireTimeout: 10 * time.Millisecond,
	})
	release, err := controller.Acquire(context.Background(), 0, 0, time.Now())
	require.NoError(t, err)

	_, err = controller.Acquire(context.Background(), 0, 0, time.Now())
	require.ErrorIs(t, err, ErrWriteRejectedConcurrency)
	release()
	release()
	releaseAgain, err := controller.Acquire(context.Background(), 0, 0, time.Now())
	require.NoError(t, err)
	releaseAgain()
	require.Equal(t, uint64(1), controller.Metrics().RejectedConcurrency)
}

func TestWriteAdmissionHonorsCanceledContext(t *testing.T) {
	controller := newWriteAdmissionController(WriteAdmissionConfig{MaxConcurrent: 1, AcquireTimeout: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := controller.Acquire(ctx, 0, 0, time.Now())
	require.ErrorIs(t, err, context.Canceled)
}
