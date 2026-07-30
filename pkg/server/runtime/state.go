package runtime

import (
	"context"
	"errors"
	"time"
)

// QueryInput 描述一次指标查询的模式与错误。
type QueryInput struct {
	Mode string
	Err  error
}

// MapQueryState 将配置模式与查询错误映射为 MetricState。
func MapQueryState(in QueryInput) MetricState {
	mode := in.Mode
	if mode == "" || mode == "off" {
		return StateNotCollected
	}
	if in.Err != nil {
		if errors.Is(in.Err, context.DeadlineExceeded) || errors.Is(in.Err, context.Canceled) {
			return StateUnavailable
		}
		return StateUnavailable
	}
	return StateOK
}

// ParseWindow 解析允许的时间窗口。
func ParseWindow(window string) (time.Duration, bool) {
	switch window {
	case "15s":
		return 15 * time.Second, true
	case "5m":
		return 5 * time.Minute, true
	case "1h":
		return time.Hour, true
	default:
		return 0, false
	}
}

// Freshness 根据窗口与最后采样时间判定是否 stale。
// 阈值：max(2×window, 30s)。
func Freshness(window string, now time.Time, last *time.Time) MetricState {
	if last == nil {
		return StateNotCollected
	}
	d, ok := ParseWindow(window)
	if !ok {
		d = 15 * time.Second
	}
	threshold := 2 * d
	if threshold < 30*time.Second {
		threshold = 30 * time.Second
	}
	if now.Sub(*last) > threshold {
		return StateStale
	}
	return StateOK
}

// NullMetric 构造 null + state 的 MetricValue。
func NullMetric(state MetricState) MetricValue {
	return MetricValue{Value: nil, State: state}
}

// ValueMetric 构造有值的 MetricValue。
func ValueMetric(v float64, state MetricState) MetricValue {
	return MetricValue{Value: &v, State: state}
}

// MergeStates 合并多个状态，取最差。
func MergeStates(states ...MetricState) MetricState {
	rank := map[MetricState]int{
		StateOK:           0,
		StateNoTraffic:    0,
		StatePartial:      1,
		StateStale:        2,
		StateNotCollected: 3,
		StateUnavailable:  4,
	}
	worst := StateOK
	for _, s := range states {
		if rank[s] > rank[worst] {
			worst = s
		}
	}
	return worst
}
