package stats

import (
	"fmt"
	"sync"
)

var (
	regMu    sync.RWMutex
	registry = map[string]StatSpec{}
)

// Register 注册一份或多份 StatSpec；Code 重复时 panic（启动期 fail-fast）。
func Register(specs ...StatSpec) {
	regMu.Lock()
	defer regMu.Unlock()
	for _, s := range specs {
		if err := Validate(s); err != nil {
			panic(fmt.Sprintf("stats.Register invalid %q: %v", s.Code, err))
		}
		if _, ok := registry[s.Code]; ok {
			panic(fmt.Sprintf("stats.Register duplicate code %q", s.Code))
		}
		registry[s.Code] = normalizeSpec(s)
	}
}

// Get 按 Code 取已注册 Spec。
func Get(code string) (StatSpec, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	s, ok := registry[code]
	return s, ok
}

// All 返回已注册 Spec 的副本列表。
func All() []StatSpec {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]StatSpec, 0, len(registry))
	for _, s := range registry {
		out = append(out, s)
	}
	return out
}

// ResetRegistryForTest 清空注册表（仅测试）。
func ResetRegistryForTest() {
	regMu.Lock()
	defer regMu.Unlock()
	registry = map[string]StatSpec{}
}

func normalizeSpec(s StatSpec) StatSpec {
	if s.TimeField == "" {
		s.TimeField = "CreatedAt"
	}
	for i := range s.Metrics {
		if s.Metrics[i].Alias == "" {
			s.Metrics[i].Alias = defaultMetricAlias(s.Metrics[i])
		}
	}
	for i := range s.Dimensions {
		if s.Dimensions[i].Alias == "" {
			s.Dimensions[i].Alias = defaultDimAlias(s.Dimensions[i].Field)
		}
	}
	return s
}

func defaultMetricAlias(m StatMetric) string {
	switch m.Kind {
	case MetricCount:
		return "row_count"
	case MetricSum:
		return "sum_" + lowerFirst(m.Field)
	case MetricAvg:
		return "avg_" + lowerFirst(m.Field)
	default:
		return string(m.Kind)
	}
}

func defaultDimAlias(field string) string {
	f := field
	if len(f) > 2 && (f[len(f)-2:] == "ID" || f[len(f)-2:] == "Id") {
		f = f[:len(f)-2]
	}
	return lowerFirst(f)
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] >= 'A' && b[0] <= 'Z' {
		b[0] += 'a' - 'A'
	}
	return string(b)
}
