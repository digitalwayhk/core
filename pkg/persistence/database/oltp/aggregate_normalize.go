// 本文件补齐中性聚合请求的别名与默认字段。
package oltp

import "github.com/digitalwayhk/core/pkg/persistence/types"

func normalizeSpec(s types.AggregateQuery) types.AggregateQuery {
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

func defaultMetricAlias(m types.AggregateMetric) string {
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
