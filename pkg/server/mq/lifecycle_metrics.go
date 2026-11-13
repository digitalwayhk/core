package mq

import (
	"context"
	"sync/atomic"

	"github.com/digitalwayhk/core/pkg/server/observability"
)

type lifecycleProviderMetrics struct {
	redelivered      atomic.Int64
	deadLetters      atomic.Int64
	deadLetterFailed atomic.Int64
}

func (m *lifecycleProviderMetrics) snapshot() map[string]float64 {
	if m == nil {
		return nil
	}
	return map[string]float64{
		"redelivered_total":      float64(m.redelivered.Load()),
		"dead_letter_total":      float64(m.deadLetters.Load()),
		"dead_letter_fail_total": float64(m.deadLetterFailed.Load()),
	}
}

func lifecycleRuntimeSnapshot(snapshot LifecycleSnapshot) observability.RuntimeComponentSnapshot {
	state := string(snapshot.State)
	if state == "" {
		state = "not_collected"
	}
	gauges := make(map[string]float64)
	if snapshot.RetainedMessages != nil {
		gauges["retained_messages"] = float64(*snapshot.RetainedMessages)
	}
	if snapshot.RetainedBytes != nil {
		gauges["retained_bytes"] = float64(*snapshot.RetainedBytes)
	}
	if snapshot.BacklogMessages != nil {
		gauges["backlog_messages"] = float64(*snapshot.BacklogMessages)
	}
	if snapshot.PendingMessages != nil {
		gauges["pending_messages"] = float64(*snapshot.PendingMessages)
	}
	if snapshot.OldestAge != nil {
		gauges["oldest_age_sec"] = snapshot.OldestAge.Seconds()
	}
	return observability.RuntimeComponentSnapshot{Component: "mq", State: state, Gauges: gauges}
}

func (c *lifecycleController) runtimeMetricSnapshot(context.Context) (observability.RuntimeComponentSnapshot, bool) {
	if c == nil {
		return observability.RuntimeComponentSnapshot{}, false
	}
	c.mu.RLock()
	entries := make([]LifecycleSnapshot, 0, len(c.entries))
	var rejected [6]uint64
	var refreshAttempts, refreshSuccesses, refreshFailures uint64
	for _, entry := range c.entries {
		for i, count := range entry.diagnostics.rejected {
			rejected[i] += count
		}
		refreshAttempts += entry.diagnostics.refreshAttempts
		refreshSuccesses += entry.diagnostics.refreshSuccesses
		refreshFailures += entry.diagnostics.refreshFailures
		snapshot := entry.snapshot
		if snapshot.ObservedAt.IsZero() || c.now().Sub(snapshot.ObservedAt) > 2*entry.policy.Reclaim.Interval {
			snapshot = LifecycleSnapshot{State: LifecycleStateStale}
		}
		if entry.capacity != nil {
			if !entry.capacity.ObservedAt.IsZero() && c.now().Sub(entry.capacity.ObservedAt) <= 2*entry.policy.Reclaim.Interval && (entry.capacity.State == LifecycleStateOK || entry.capacity.State == LifecycleStatePartial) {
				snapshot.RetainedMessages = entry.capacity.RetainedMessages
				snapshot.RetainedBytes = entry.capacity.RetainedBytes
			} else {
				// 独立容量失败不能在Runtime里再用旧完整容量伪装已采集；完整组指标仍按自身时间判断。
				snapshot.RetainedMessages = nil
				snapshot.RetainedBytes = nil
				snapshot.State = LifecycleStatePartial
			}
		}
		entries = append(entries, snapshot)
	}
	c.mu.RUnlock()
	if len(entries) == 0 {
		return observability.RuntimeComponentSnapshot{}, false
	}
	result := observability.RuntimeComponentSnapshot{
		Component: "mq", State: "ok", Gauges: make(map[string]float64),
		Counters: map[string]float64{
			"reclaimed_total":        float64(c.reclaimed.Load()),
			"reclaim_fail_total":     float64(c.reclaimFailed.Load()),
			"publish_rejected_total": float64(c.publishRejected.Load()),
		},
	}
	for i, reason := range lifecycleCapacityReasons {
		result.Counters["publish_rejected_reason_"+reason+"_total"] = float64(rejected[i])
	}
	result.Counters["capacity_refresh_attempt_total"] = float64(refreshAttempts)
	result.Counters["capacity_refresh_success_total"] = float64(refreshSuccesses)
	result.Counters["capacity_refresh_fail_total"] = float64(refreshFailures)
	for i, reason := range []string{"deadline", "owner_lost", "policy_fence", "provider"} {
		result.Counters["reclaim_fail_"+reason+"_total"] = float64(c.reclaimReasons[i].Load())
	}
	type metricValue struct {
		count int
		sum   float64
		max   float64
	}
	values := map[string]*metricValue{
		"retained_messages": {}, "retained_bytes": {}, "backlog_messages": {},
		"pending_messages": {}, "oldest_age_sec": {},
	}
	for _, snapshot := range entries {
		if snapshot.State != LifecycleStateOK {
			result.State = "partial"
		}
		mapped := lifecycleRuntimeSnapshot(snapshot)
		for name, value := range mapped.Gauges {
			metric := values[name]
			metric.count++
			metric.sum += value
			if value > metric.max {
				metric.max = value
			}
		}
	}
	for name, value := range values {
		if value.count != len(entries) {
			result.State = "partial"
			continue
		}
		if name == "oldest_age_sec" {
			result.Gauges[name] = value.max
		} else {
			result.Gauges[name] = value.sum
		}
	}
	return result, true
}

func mergeRuntimeMetricSnapshots(
	provider observability.RuntimeComponentSnapshot,
	lifecycle observability.RuntimeComponentSnapshot,
) observability.RuntimeComponentSnapshot {
	result := provider
	result.Component = "mq"
	if result.Gauges == nil {
		result.Gauges = make(map[string]float64)
	}
	if result.Counters == nil {
		result.Counters = make(map[string]float64)
	}
	for name, value := range lifecycle.Gauges {
		result.Gauges[name] = value
	}
	for name, value := range lifecycle.Counters {
		result.Counters[name] += value
	}
	if result.State == "" || result.State == "not_collected" {
		result.State = lifecycle.State
	} else if lifecycle.State != "" && lifecycle.State != "ok" {
		result.State = "partial"
	}
	return result
}
