// 本文件覆盖容量补读的失败、语义错误优先级、500ms总预算、关闭和策略替换；不放宽回收门禁。
package mq

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func capacityRefreshFixture(t *testing.T) (*lifecycleController, *capacityRefreshDiagnosticProvider, *lifecycleEntry, LifecyclePolicy) {
	t.Helper()
	policy := validLifecyclePolicy("required")
	policy.Reclaim.Interval = 2 * time.Second
	policy.Reclaim.TimeBudget = 500 * time.Millisecond
	policy.Capacity.HardMessages = 10
	policy.Capacity.HardBytes = 1024
	p := &capacityRefreshDiagnosticProvider{workerTestProvider: workerTestProvider{owner: true}}
	p.full = func(context.Context, LifecyclePolicy) (LifecycleSnapshot, error) {
		return LifecycleSnapshot{}, context.DeadlineExceeded
	}
	c := newLifecycleController()
	t.Cleanup(c.stop)
	n, b, pending, lag := int64(1), int64(64), int64(7), int64(9)
	entry := &lifecycleEntry{policy: policy, provider: p, snapshot: LifecycleSnapshot{Subject: policy.Subject, PolicyFingerprint: policy.Fingerprint(), ObservedAt: time.Now().Add(-5 * time.Second), State: LifecycleStateOK, RetainedMessages: &n, RetainedBytes: &b, PendingMessages: &pending, BacklogMessages: &lag, SafeFrontier: "original-frontier"}}
	c.entries[policy.Subject] = entry
	return c, p, entry, policy
}

func capacityRefreshSnapshot(policy LifecyclePolicy) LifecycleSnapshot {
	n, b := int64(1), int64(64)
	return LifecycleSnapshot{Subject: policy.Subject, PolicyFingerprint: policy.Fingerprint(), ObservedAt: time.Now(), State: LifecycleStatePartial, RetainedMessages: &n, RetainedBytes: &b}
}

// TestOwnerCapacityRefreshEvidenceIsolation仅容量可刷新，旧完整组/前沿/时间保持，完整deadline仍计一次。
func TestOwnerCapacityRefreshEvidenceIsolation(t *testing.T) {
	c, p, e, policy := capacityRefreshFixture(t)
	old := e.snapshot
	p.light = func(_ context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error) {
		return capacityRefreshSnapshot(policy), nil
	}
	require.ErrorIs(t, c.runOnce(context.Background(), policy.Subject), context.DeadlineExceeded)
	require.NoError(t, c.allowPublish(policy.Subject))
	require.Equal(t, old, e.snapshot)
	require.EqualValues(t, 1, p.fullCalls.Load())
	require.EqualValues(t, 1, p.capacityCalls.Load())
	require.Zero(t, p.reclaimCalls)
	require.Equal(t, "owner", e.diagnostics.role)
	require.Equal(t, "deadline", e.diagnostics.fullReadError)
	require.Equal(t, "none", e.diagnostics.capacityReadError)
	m, _ := c.runtimeMetricSnapshot(context.Background())
	require.NotContains(t, m.Gauges, "pending_messages")
	require.NotContains(t, m.Gauges, "backlog_messages")
	require.EqualValues(t, 64, m.Gauges["retained_bytes"])
	require.EqualValues(t, 1, m.Counters["reclaim_fail_deadline_total"])
	require.EqualValues(t, 1, m.Counters["capacity_refresh_success_total"])
}

// TestOwnerCapacityRefreshRejectsBadEvidence补读失败/缺证据不得重用旧容量，真实硬限仍是Backpressure而非unknown。
func TestOwnerCapacityRefreshRejectsBadEvidence(t *testing.T) {
	for _, name := range []string{"provider", "deadline", "missing_bytes", "missing_messages", "wrong_subject", "wrong_policy", "stale", "unavailable", "zero_time", "negative_bytes", "hard_bytes", "hard_messages"} {
		t.Run(name, func(t *testing.T) {
			c, p, e, policy := capacityRefreshFixture(t)
			// 即使旧完整容量仍新鲜，失败补读也不能将其伪装成独立容量成功。
			e.snapshot.ObservedAt = time.Now()
			old := e.snapshot
			p.light = func(_ context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error) {
				s := capacityRefreshSnapshot(policy)
				switch name {
				case "provider":
					return LifecycleSnapshot{}, errors.New("Core合成轻量读取失败")
				case "deadline":
					return LifecycleSnapshot{}, context.DeadlineExceeded
				case "missing_bytes":
					s.RetainedBytes = nil
				case "missing_messages":
					s.RetainedMessages = nil
				case "wrong_subject":
					s.Subject = "different"
				case "wrong_policy":
					s.PolicyFingerprint = "different"
				case "stale":
					s.ObservedAt = time.Now().Add(-5 * time.Second)
				case "unavailable":
					s.State = LifecycleStateUnavailable
				case "zero_time":
					s.ObservedAt = time.Time{}
				case "negative_bytes":
					n := int64(-1)
					s.RetainedBytes = &n
				case "hard_bytes":
					n := policy.Capacity.HardBytes
					s.RetainedBytes = &n
				case "hard_messages":
					n := policy.Capacity.HardMessages
					s.RetainedMessages = &n
				}
				return s, nil
			}
			require.ErrorIs(t, c.runOnce(context.Background(), policy.Subject), context.DeadlineExceeded)
			require.Equal(t, old, e.snapshot)
			require.EqualValues(t, 1, p.capacityCalls.Load())
			require.Zero(t, p.reclaimCalls)
			want := ErrLifecycleCapacityUnknown
			if name == "hard_bytes" || name == "hard_messages" {
				want = ErrLifecycleBackpressure
			}
			require.ErrorIs(t, c.allowPublish(policy.Subject), want)
			m, _ := c.runtimeMetricSnapshot(context.Background())
			require.EqualValues(t, 1, m.Counters["reclaim_fail_total"])
			if want == ErrLifecycleCapacityUnknown {
				require.NotContains(t, m.Gauges, "retained_bytes")
				require.EqualValues(t, 1, m.Counters["capacity_refresh_fail_total"])
			} else {
				require.EqualValues(t, 1, m.Counters["capacity_refresh_success_total"])
			}
		})
	}
}

// TestOwnerCapacityRefreshSemanticDeadlineRace语义错误和Inspect子deadline同时发生仍不得降级。
func TestOwnerCapacityRefreshSemanticDeadlineRace(t *testing.T) {
	for _, semantic := range []error{ErrLifecyclePolicyConflict, ErrLifecycleStateUncertain, ErrLifecycleUnsafeBrokerPolicy, errLifecycleOwnerLost, context.Canceled} {
		t.Run(semantic.Error(), func(t *testing.T) {
			c, p, e, policy := capacityRefreshFixture(t)
			p.full = func(ctx context.Context, _ LifecyclePolicy) (LifecycleSnapshot, error) {
				<-ctx.Done()
				return LifecycleSnapshot{}, errors.Join(semantic, ctx.Err())
			}
			require.ErrorIs(t, c.runOnce(context.Background(), policy.Subject), semantic)
			require.Zero(t, p.capacityCalls.Load())
			require.Zero(t, p.reclaimCalls)
			require.Nil(t, e.capacity)
			require.ErrorIs(t, c.allowPublish(policy.Subject), ErrLifecycleCapacityUnknown)
			if semantic != context.Canceled {
				require.NotEqual(t, "deadline", e.diagnostics.fullReadError)
			}
		})
	}
	t.Run("invalid_snapshot_at_deadline", func(t *testing.T) {
		c, p, _, policy := capacityRefreshFixture(t)
		p.full = func(ctx context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error) {
			<-ctx.Done()
			s := capacityRefreshSnapshot(policy)
			s.PolicyFingerprint = "wrong"
			return s, nil
		}
		require.ErrorIs(t, c.runOnce(context.Background(), policy.Subject), ErrLifecyclePolicyConflict)
		require.Zero(t, p.capacityCalls.Load())
		require.Zero(t, p.reclaimCalls)
	})
}

// TestOwnerCapacityRefreshOriginalBudget轻量上下文只剩原整轮余量，不另开一份500ms，也不吞完整错误。
func TestOwnerCapacityRefreshOriginalBudget(t *testing.T) {
	c, p, _, policy := capacityRefreshFixture(t)
	var fullDeadline, lightDeadline time.Time
	p.full = func(ctx context.Context, _ LifecyclePolicy) (LifecycleSnapshot, error) {
		fullDeadline, _ = ctx.Deadline()
		<-ctx.Done()
		return LifecycleSnapshot{}, ctx.Err()
	}
	p.light = func(ctx context.Context, _ LifecyclePolicy) (LifecycleSnapshot, error) {
		lightDeadline, _ = ctx.Deadline()
		<-ctx.Done()
		return LifecycleSnapshot{}, ctx.Err()
	}
	started := time.Now()
	require.ErrorIs(t, c.runOnce(context.Background(), policy.Subject), context.DeadlineExceeded)
	require.GreaterOrEqual(t, lightDeadline.Sub(fullDeadline), time.Duration(0))
	require.LessOrEqual(t, lightDeadline.Sub(fullDeadline), policy.Reclaim.TimeBudget*3/10)
	require.ErrorIs(t, c.allowPublish(policy.Subject), ErrLifecycleCapacityUnknown)
	require.EqualValues(t, 1, p.capacityCalls.Load())
	require.Zero(t, p.reclaimCalls)
	t.Logf("总预算=%s 实际轮次=%s full/light截止差=%s", policy.Reclaim.TimeBudget, time.Since(started), lightDeadline.Sub(fullDeadline))
}

// TestOwnerCapacityRefreshPolicyReplacement迟到结果不能写入替换entry或变更后的policy。
func TestOwnerCapacityRefreshPolicyReplacement(t *testing.T) {
	for _, mode := range []string{"entry", "policy"} {
		t.Run(mode, func(t *testing.T) {
			c, p, e, policy := capacityRefreshFixture(t)
			replacement := &lifecycleEntry{policy: policy, provider: p}
			p.light = func(_ context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error) {
				c.mu.Lock()
				if mode == "entry" {
					c.entries[policy.Subject] = replacement
				} else {
					e.policy.Capacity.HardBytes++
				}
				c.mu.Unlock()
				return capacityRefreshSnapshot(policy), nil
			}
			require.ErrorIs(t, c.runOnce(context.Background(), policy.Subject), context.DeadlineExceeded)
			require.Nil(t, replacement.capacity)
			require.Nil(t, e.capacity)
			require.Zero(t, p.reclaimCalls)
		})
	}
}

// TestOwnerCapacityRefreshCanceledLateSuccess关闭等待原轻量回调真实退出，迟到nil不能刷新或回收。
func TestOwnerCapacityRefreshCanceledLateSuccess(t *testing.T) {
	c, p, e, policy := capacityRefreshFixture(t)
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	p.light = func(ctx context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error) {
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-release
		return capacityRefreshSnapshot(policy), nil
	}
	result := make(chan error, 1)
	c.wg.Add(1)
	go func() { defer c.wg.Done(); result <- c.runOnce(c.ctx, policy.Subject) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("补读未进入")
	}
	closed := make(chan struct{})
	go func() { c.stop(); close(closed) }()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("取消未到达补读")
	}
	select {
	case <-closed:
		t.Fatal("真实回调未退出时关闭不得完成")
	default:
	}
	close(release)
	released = true
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(time.Second):
		t.Fatal("补读未退出")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("回调退出后关闭未完成")
	}
	require.Equal(t, "canceled", e.diagnostics.capacityReadError)
	require.ErrorIs(t, c.allowPublish(policy.Subject), ErrLifecycleCapacityUnknown)
	require.Zero(t, p.reclaimCalls)
}

// TestOwnerCapacityRefreshLegacyProvider原公共Provider不具内置轻量能力，保留旧检查和deadline返回行为。
func TestOwnerCapacityRefreshLegacyProvider(t *testing.T) {
	policy := validLifecyclePolicy("required")
	policy.Reclaim.TimeBudget = 50 * time.Millisecond
	policy.Capacity.HardBytes = 1024
	p := &lifecycleTestProvider{blockInspect: true, inspectCalls: 1}
	c := newLifecycleController()
	defer c.stop()
	e := &lifecycleEntry{policy: policy, provider: p}
	c.entries[policy.Subject] = e
	require.ErrorIs(t, c.runOnce(context.Background(), policy.Subject), context.DeadlineExceeded)
	require.Nil(t, e.capacity)
	require.Zero(t, e.diagnostics.refreshAttempts)
	require.Zero(t, p.reclaimCalls)
}

// TestLifecycleCapacityDiagnosticsBoundedLogState固定entry原因计数不丢；日志同状态只一次，状态变化仍受Interval限制。
func TestLifecycleCapacityDiagnosticsBoundedLogState(t *testing.T) {
	c, _, e, policy := capacityRefreshFixture(t)
	now := time.Now()
	c.now = func() time.Time { return now }
	e.snapshot.ObservedAt = time.Time{}
	for range 100 {
		require.ErrorIs(t, c.allowPublish(policy.Subject), ErrLifecycleCapacityUnknown)
	}
	firstSignature, firstAt := e.diagnostics.logSignature, e.diagnostics.logAt
	require.Equal(t, now, firstAt)
	require.EqualValues(t, 100, e.diagnostics.rejected[0])
	e.snapshot.ObservedAt = now.Add(-5 * time.Second)
	now = now.Add(time.Millisecond)
	require.Error(t, c.allowPublish(policy.Subject))
	require.Equal(t, firstSignature, e.diagnostics.logSignature)
	now = now.Add(policy.Reclaim.Interval)
	require.Error(t, c.allowPublish(policy.Subject))
	require.NotEqual(t, firstSignature, e.diagnostics.logSignature)
	require.Equal(t, now, e.diagnostics.logAt)
	m, _ := c.runtimeMetricSnapshot(context.Background())
	require.EqualValues(t, 100, m.Counters["publish_rejected_reason_not_observed_total"])
	require.EqualValues(t, 2, m.Counters["publish_rejected_reason_stale_total"])
}

// TestOwnerCapacityRefreshSuccessfulFullAndNoLimit成功完整检查不额外读容量；无硬限超时也不增加读取。
func TestOwnerCapacityRefreshSuccessfulFullAndNoLimit(t *testing.T) {
	for _, mode := range []string{"full_success", "no_hard_limit", "provider_error"} {
		t.Run(mode, func(t *testing.T) {
			c, p, e, policy := capacityRefreshFixture(t)
			sentinel := errors.New("Core合成完整读取失败")
			switch mode {
			case "full_success":
				p.full = func(_ context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error) {
					s := capacityRefreshSnapshot(policy)
					s.State = LifecycleStateOK
					return s, nil
				}
			case "no_hard_limit":
				policy.Capacity = CapacityPolicy{}
				e.policy = policy
			case "provider_error":
				p.full = func(context.Context, LifecyclePolicy) (LifecycleSnapshot, error) {
					return LifecycleSnapshot{}, sentinel
				}
			}
			err := c.runOnce(context.Background(), policy.Subject)
			if mode == "full_success" {
				require.NoError(t, err)
				require.Equal(t, 1, p.reclaimCalls)
			} else {
				require.Error(t, err)
				require.Zero(t, p.reclaimCalls)
			}
			require.Zero(t, p.capacityCalls.Load())
			require.Zero(t, e.diagnostics.refreshAttempts)
		})
	}
}

// capacityLockCheckingError验证外部error分类不在controller全局锁内执行，避免可重入Error实现卡住发布。
type capacityLockCheckingError struct {
	c      *lifecycleController
	called atomic.Bool
	locked atomic.Bool
}

func (e *capacityLockCheckingError) Error() string {
	e.called.Store(true)
	if e.c.mu.TryLock() {
		e.c.mu.Unlock()
	} else {
		e.locked.Store(true)
	}
	return "Core合成锁边界错误"
}

// TestLifecycleReadClassificationDoesNotHoldControllerLock错误分类可调用外部Error方法，必须在控制器锁之外。
func TestLifecycleReadClassificationDoesNotHoldControllerLock(t *testing.T) {
	c, _, e, policy := capacityRefreshFixture(t)
	probe := &capacityLockCheckingError{c: c}
	c.recordLifecycleRead(policy.Subject, e, "full", probe)
	require.True(t, probe.called.Load())
	require.False(t, probe.locked.Load(), "不能持全局锁执行外部error.Error")
}
