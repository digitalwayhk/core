package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOrderWriteGuardLimitsConcurrentSubmissions(t *testing.T) {
	guard := newOrderWriteGuard(orderWriteGuardConfig{MaxConcurrent: 1, AcquireTimeout: 10 * time.Millisecond})
	release, err := guard.Acquire(0, 0, time.Now())
	require.NoError(t, err)
	require.NotNil(t, release)

	_, err = guard.Acquire(0, 0, time.Now())
	require.ErrorContains(t, err, "并发上限")
	release()

	release, err = guard.Acquire(0, 0, time.Now())
	require.NoError(t, err)
	release()
}

func TestOrderWriteGuardRejectsHardBacklog(t *testing.T) {
	guard := newOrderWriteGuard(orderWriteGuardConfig{
		MaxConcurrent: 10,
		SoftPending:   10,
		HardPending:   20,
	})

	_, err := guard.Acquire(20, 0, time.Now())
	require.ErrorContains(t, err, "积压已达硬上限")
}

func TestOrderWriteGuardRejectsSustainedSoftBacklog(t *testing.T) {
	guard := newOrderWriteGuard(orderWriteGuardConfig{
		MaxConcurrent:      10,
		SoftPending:        10,
		HardPending:        20,
		MaxBacklogDuration: time.Minute,
	})
	started := time.Unix(100, 0)
	release, err := guard.Acquire(10, 0, started)
	require.NoError(t, err, "刚进入软阈值时不应立即拒绝")
	release()

	_, err = guard.Acquire(10, 0, started.Add(time.Minute))
	require.ErrorContains(t, err, "持续积压超时")

	release, err = guard.Acquire(9, 0, started.Add(2*time.Minute))
	require.NoError(t, err, "积压降到软阈值以下后应自动恢复")
	release()
}

func TestOrderWriteGuardRejectsHardDiskUsage(t *testing.T) {
	guard := newOrderWriteGuard(orderWriteGuardConfig{
		MaxConcurrent: 10,
		HardDiskBytes: 1024,
	})

	_, err := guard.Acquire(0, 1024, time.Now())
	require.ErrorContains(t, err, "磁盘使用已达硬上限")
	snapshot := guard.Snapshot()
	require.Equal(t, uint64(1), snapshot.RejectedDisk)
}
