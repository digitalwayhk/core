// 本文件在已登记entry内保存固定诊断与拒绝原因；不按业务消息建map，不新增指标标签。
package mq

import (
	"context"
	"errors"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

var lifecycleCapacityReasons = [...]string{"not_observed", "stale", "state", "messages_missing", "bytes_missing", "hard_reached"}

// lifecycleDiagnostics全部字段由controller.mu保护；空间只随已登记策略entry增长。
type lifecycleDiagnostics struct {
	role              string
	fullReadError     string
	capacityReadError string
	rejected          [6]uint64
	refreshAttempts   uint64
	refreshSuccesses  uint64
	refreshFailures   uint64
	logAt             time.Time
	logSignature      string
}

func lifecycleReadErrorClass(err error) string {
	if err == nil {
		return "none"
	}
	if errors.Is(err, context.Canceled) && lifecycleFailureReason(err) == 3 {
		return "canceled"
	}
	return [4]string{"deadline", "owner_lost", "policy_fence", "provider"}[lifecycleFailureReason(err)]
}

func (c *lifecycleController) beginLifecycleRound(subject string, entry *lifecycleEntry, role string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries[subject] != entry {
		return
	}
	entry.diagnostics.role = role
	entry.diagnostics.fullReadError = "not_attempted"
	entry.diagnostics.capacityReadError = "not_attempted"
}

func (c *lifecycleController) recordLifecycleRead(subject string, entry *lifecycleEntry, phase string, err error) {
	// 外部error.Error可能可重入；先在锁外归类，只在锁内保存固定值。
	class := lifecycleReadErrorClass(err)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries[subject] != entry {
		return
	}
	if phase == "full" {
		entry.diagnostics.fullReadError = class
	} else {
		entry.diagnostics.capacityReadError = class
	}
}

func lifecycleCapacityDecision(policy LifecyclePolicy, snapshot LifecycleSnapshot, now time.Time) (error, int) {
	if snapshot.ObservedAt.IsZero() {
		return ErrLifecycleCapacityUnknown, 0
	}
	if now.Sub(snapshot.ObservedAt) > 2*policy.Reclaim.Interval {
		return ErrLifecycleCapacityUnknown, 1
	}
	if snapshot.State != LifecycleStateOK && snapshot.State != LifecycleStatePartial {
		return ErrLifecycleCapacityUnknown, 2
	}
	if policy.Capacity.HardMessages > 0 {
		if snapshot.RetainedMessages == nil {
			return ErrLifecycleCapacityUnknown, 3
		}
		if *snapshot.RetainedMessages >= policy.Capacity.HardMessages {
			return ErrLifecycleBackpressure, 5
		}
	}
	if policy.Capacity.HardBytes > 0 {
		if snapshot.RetainedBytes == nil {
			return ErrLifecycleCapacityUnknown, 4
		}
		if *snapshot.RetainedBytes >= policy.Capacity.HardBytes {
			return ErrLifecycleBackpressure, 5
		}
	}
	return nil, -1
}

func (c *lifecycleController) recordCapacityRejection(subject string, entry *lifecycleEntry, snapshot LifecycleSnapshot, source string, reason int, now time.Time) {
	c.mu.Lock()
	if c.entries[subject] != entry {
		c.mu.Unlock()
		return
	}
	d := &entry.diagnostics
	d.rejected[reason]++
	role, full, light := d.role, d.fullReadError, d.capacityReadError
	if role == "" {
		role = "not_observed"
	}
	if full == "" {
		full = "not_attempted"
	}
	if light == "" {
		light = "not_attempted"
	}
	signature := lifecycleCapacityReasons[reason] + "|" + source + "|" + role + "|" + full + "|" + light
	// 同状态只记录一次；状态变化也至多每既有Interval记录一次，不放大发布拒绝风暴。
	log := signature != d.logSignature && (d.logAt.IsZero() || now.Sub(d.logAt) >= entry.policy.Reclaim.Interval)
	if log {
		d.logAt = now
		d.logSignature = signature
	}
	c.mu.Unlock()
	if !log {
		return
	}
	fields := []logx.LogField{logx.Field("subject", subject), logx.Field("reason", lifecycleCapacityReasons[reason]), logx.Field("snapshot_source", source), logx.Field("last_round_role", role), logx.Field("full_read_error", full), logx.Field("capacity_read_error", light)}
	if !snapshot.ObservedAt.IsZero() {
		fields = append(fields, logx.Field("snapshot_age_ms", now.Sub(snapshot.ObservedAt).Milliseconds()))
	}
	logx.Infow("mq_lifecycle_publish_blocked", fields...)
}
