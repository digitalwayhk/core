// 本文件用受控观测和原监视循环验证解除阻挡的资格时钟，不增加调度线程或伪造确认。
package ownership

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type blockedWatchStore struct {
	Store
	mode   *atomic.Int32
	renews chan int32
	fail   atomic.Bool
}

func (s *blockedWatchStore) Renew(context.Context, Credential, time.Duration) error {
	if s.fail.Load() {
		return ErrFenced
	}
	select {
	case s.renews <- s.mode.Load():
	default:
	}
	return nil
}
func (*blockedWatchStore) Release(context.Context, Credential) error { return nil }

// TestBlockedDueWatchRearmsOnlyEligibility保留旧实际进度；恢复可执行后给予原窗口，随后零进展仍撤销。
func TestBlockedDueWatchRearmsOnlyEligibility(t *testing.T) {
	timing := Timing{Lease: time.Second, Renew: 30 * time.Millisecond, Poll: 10 * time.Millisecond, Execution: 200 * time.Millisecond, Stall: 500 * time.Millisecond, Observation: 150 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mode atomic.Int32
	s := &blockedWatchStore{mode: &mode, renews: make(chan int32, 128)}
	r := &Runtime{store: s, options: Options{Manifest: Manifest{Timing: timing}, Observe: func(context.Context, Credential) (Observation, error) {
		o := Observation{At: time.Now(), Due: 1, BlockedDue: 1}
		if mode.Load() == 1 {
			o.Due = 2
		}
		return o, nil
	}}}
	var stage, progress atomic.Int64
	var stepFailed atomic.Bool
	original := time.Now().Add(-time.Hour).UnixNano()
	progress.Store(original)
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.watchKey(ctx, validCredential(), cancel, &stage, &progress, &stepFailed, &runtimeReadyState{})
	}()
	// 持续超出stall的明确业务阻挡不能取消；原业务进度时钟不动。
	timer := time.NewTimer(2 * timing.Stall)
	select {
	case <-ctx.Done():
		t.Fatal("合法阻挡被撤销")
	case <-timer.C:
	}
	require.Equal(t, original, progress.Load())
	mode.Store(1)
	observed := 0
	deadline := time.After(timing.Stall)
	for observed < 5 {
		select {
		case m := <-s.renews:
			if m == 1 {
				observed++
			}
		case <-ctx.Done():
			t.Fatal("解除阻挡后按旧确认时间立即撤销")
		case <-deadline:
			t.Fatal("未观测到恢复可执行后的正常续租")
		}
	}
	require.Equal(t, original, progress.Load(), "资格变化不能假装业务确认")
	select {
	case <-done:
	case <-time.After(3 * timing.Stall):
		t.Fatal("真实可执行零进展未按原stall撤销")
	}
	require.EqualValues(t, 1, r.stalledRevocations.Load())
}

// TestBlockedDueDoesNotMaskUnknownOrRenewFailure未知/非法分类与续租错误仍撤销，不借暂停无限续租。
func TestBlockedDueDoesNotMaskUnknownOrRenewFailure(t *testing.T) {
	for _, kind := range []string{"unknown", "invalid", "renew", "step_error"} {
		t.Run(kind, func(t *testing.T) {
			timing := Timing{Lease: time.Second, Renew: 20 * time.Millisecond, Poll: time.Millisecond, Execution: 200 * time.Millisecond, Stall: 500 * time.Millisecond, Observation: 100 * time.Millisecond}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var mode atomic.Int32
			s := &blockedWatchStore{mode: &mode, renews: make(chan int32, 64)}
			r := &Runtime{store: s, options: Options{Manifest: Manifest{Timing: timing}, Observe: func(context.Context, Credential) (Observation, error) {
				if kind == "unknown" {
					return Observation{}, ErrUnknown
				}
				o := Observation{At: time.Now(), Due: 1, BlockedDue: 1}
				if kind == "invalid" {
					o.BlockedDue = 2
				}
				return o, nil
			}}}
			if kind == "renew" {
				s.fail.Store(true)
			}
			var stage, progress atomic.Int64
			var stepFailed atomic.Bool
			progress.Store(time.Now().UnixNano())
			stepFailed.Store(kind == "step_error")
			done := make(chan struct{})
			go func() {
				defer close(done)
				r.watchKey(ctx, validCredential(), cancel, &stage, &progress, &stepFailed, &runtimeReadyState{})
			}()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("未知或续租失败未撤销")
			}
			if kind == "step_error" {
				require.EqualValues(t, 1, r.stalledRevocations.Load())
			} else if kind == "renew" {
				require.EqualValues(t, 1, r.renewFailures.Load())
			} else {
				require.EqualValues(t, 1, r.observationRevocations.Load())
			}
		})
	}
}
