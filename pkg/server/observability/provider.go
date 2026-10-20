package observability

import "context"

// RuntimeComponentSnapshot 是组件运行时指标的只读快照。
type RuntimeComponentSnapshot struct {
	Component string
	State     string // ok|not_collected|unavailable
	Gauges    map[string]float64
	Counters  map[string]float64
}

// RuntimeMetricProvider 由本进程组件实现；只用于注册 Collector，不由 Aggregator 远程调用。
type RuntimeMetricProvider interface {
	ComponentName() string
	RuntimeMetricSnapshot(ctx context.Context) RuntimeComponentSnapshot
}

// allowedGaugeNames 组件 gauge 名白名单（低基数）。
var allowedGaugeNames = map[string]struct{}{
	"local_queue_wait_seconds_total": {}, "admission_seconds_total": {}, "admission_attempts_total": {},
	"admission_rejected_concurrency_total": {},
	"admission_rejected_pending_total":     {},
	"admission_rejected_disk_total":        {},
	"load_seconds_total":                   {},
	"publish_seconds_total":                {},
	"mark_seconds_total":                   {},
	"load_attempts_total":                  {},
	"publish_attempts_total":               {},
	"mark_attempts_total":                  {},
	"local_commit_seconds_total":           {},
	"local_committed_total":                {},
	"local_batches_total":                  {},
	"sync_seconds_total":                   {},
	"sync_attempts_total":                  {},
	"sql_wait_count_total":                 {},
	"sql_wait_seconds_total":               {},
	"sql_open":                             {},
	"sql_in_use":                           {},
	"sql_idle":                             {},
	"sql_max_open":                         {},
	"sql_idle_closed_total":                {},
	"sql_idle_time_closed_total":           {},
	"sql_lifetime_closed_total":            {},
	"notification_reconcile_seconds":       {},
	"depth":                                {},
	"disk_bytes":                           {},
	"sync_fail":                            {},
	"oldest_age_sec":                       {},
	"publish_fail":                         {},
	"lag":                                  {},
	"connections":                          {},
	"queue_depth":                          {},
	"hit_ratio":                            {},
	"key_concurrency":                      {},
	"worker_inflight":                      {},
	"worker_peak":                          {},
	"active_lanes":                         {},
	"blocked_keys":                         {},
	"batch_size":                           {},
	"batch_limit":                          {},
	"handler_inflight":                     {},
	"handler_peak":                         {},
	"active_keys":                          {},
	"pending_keys":                         {},
	"shared_key_leases":                    {},
	"redis_blocking_subscriptions":         {},
	"redis_command_pool_limit":             {}, "redis_command_pool_total": {}, "redis_command_pool_idle": {}, "redis_command_pool_in_use": {},
	"redis_blocking_pool_limit": {}, "redis_blocking_pool_total": {}, "redis_blocking_pool_idle": {}, "redis_blocking_pool_in_use": {},
	"retained_messages": {},
	"retained_bytes":    {},
	"backlog_messages":  {},
	"pending_messages":  {},
	"prefetch_capacity": {}, "prefetched_messages": {}, "queued_work": {},
}

var allowedCounterNames = map[string]struct{}{
	"redis_command_pool_wait_total": {}, "redis_command_pool_wait_seconds_total": {}, "redis_command_pool_timeouts_total": {},
	"redis_blocking_pool_wait_total": {}, "redis_blocking_pool_wait_seconds_total": {}, "redis_blocking_pool_timeouts_total": {},
	"shared_key_acquired_total": {}, "shared_key_contended_total": {}, "shared_key_owner_lost_total": {},
	"reliable_ack_total": {}, "reliable_ack_fail_total": {}, "reliable_ack_seconds_total": {},
	"notification_published_total":      {},
	"notification_gap_total":            {},
	"notification_reconcile_total":      {},
	"notification_reconcile_fail_total": {},
	"reclaimed_total":                   {},
	"reclaim_fail_total":                {},
	"reclaim_fail_deadline_total":       {},
	"reclaim_fail_owner_lost_total":     {},
	"reclaim_fail_policy_fence_total":   {},
	"reclaim_fail_provider_total":       {},
	"redelivered_total":                 {},
	"dead_letter_total":                 {},
	"dead_letter_fail_total":            {},
	"publish_rejected_total":            {},
	"reliable_new_read_total":           {}, "reliable_reclaim_read_total": {},
	"reliable_publish_to_read_seconds_total": {}, "reliable_schedule_wait_seconds_total": {},
	"reliable_handler_seconds_total": {}, "reliable_pel_wait_seconds_total": {},
	"reliable_pel_wait_samples_total": {}, "reliable_scheduled_total": {}, "reliable_handler_calls_total": {},
}

func filterGauges(in map[string]float64) map[string]float64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]float64, len(in))
	for k, v := range in {
		if _, ok := allowedGaugeNames[k]; ok {
			out[k] = v
		}
	}
	return out
}

func filterCounters(in map[string]float64) map[string]float64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]float64, len(in))
	for key, value := range in {
		if _, ok := allowedCounterNames[key]; ok {
			out[key] = value
		}
	}
	return out
}
