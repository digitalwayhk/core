// 本文件定义内置 Provider 的轮次协调，保持公共生命周期接口和策略指纹不变。
package mq

import (
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"strings"
	"time"
)

var errLifecycleOwnerLost = errors.New("mq: lifecycle owner lost")

// 只映射固定原因；原始错误不成为指标标签或公开字段。
func lifecycleFailureReason(err error) int {
	if errors.Is(err, errLifecycleOwnerLost) || strings.Contains(err.Error(), "LIFECYCLE_OWNER_LOST") {
		return 1
	}
	if errors.Is(err, ErrLifecyclePolicyConflict) || errors.Is(err, ErrLifecycleStateUncertain) || errors.Is(err, ErrLifecycleUnsafeBrokerPolicy) {
		return 2
	}
	if errors.Is(err, context.Canceled) {
		return 3
	}
	var timeout net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
		return 0
	}
	return 3
}

// lifecyclePhaseError保留取消和已知语义错误，避免超时竞态将其误判为可补容量的deadline。
func lifecyclePhaseError(err, phaseErr error) error {
	if err != nil {
		reason := lifecycleFailureReason(err)
		if reason == 1 || reason == 2 || errors.Is(err, context.Canceled) {
			return err
		}
	}
	if phaseErr != nil {
		return phaseErr
	}
	return err
}

type lifecycleWorkerProvider interface {
	acquireLifecycleRound(context.Context, LifecyclePolicy, string) (context.Context, bool, error)
	inspectLifecycleCapacity(context.Context, LifecyclePolicy) (LifecycleSnapshot, error)
}

type lifecycleWorkerReleaser interface {
	releaseLifecycleRound(context.Context, LifecyclePolicy, string) error
}

type lifecycleRoundKey struct{}

// lifecycleRoundLease 只在包内传递本轮已取得的凭据，不能跨 provider/subject 复用。
type lifecycleRoundLease struct {
	provider LifecycleMQProvider
	subject  string
	owner    string
	revision uint64
}

func lifecycleLease(ctx context.Context, provider LifecycleMQProvider, subject string) (lifecycleRoundLease, bool) {
	lease, ok := ctx.Value(lifecycleRoundKey{}).(lifecycleRoundLease)
	return lease, ok && lease.provider == provider && lease.subject == subject
}

func lifecycleWorkerLeaseTTL(policy LifecyclePolicy) time.Duration {
	return max(time.Second, 3*policy.Reclaim.Interval+3*policy.Reclaim.TimeBudget)
}

func lifecycleWorkerDelay(interval time.Duration, startup bool) time.Duration {
	if startup {
		return 1 + time.Duration(rand.Int64N(max(1, int64(interval))))
	}
	// 每次完成后重新计时，避免慢轮次追赶 tick，也避免固定同相位。
	return interval*3/4 + time.Duration(rand.Int64N(max(1, int64(interval/2))))
}
