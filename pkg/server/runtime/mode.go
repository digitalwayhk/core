package runtime

import "strings"

// NormalizeMode 将 RuntimeObservability.Mode 规范为 off|prometheus。
func NormalizeMode(mode string) string {
	m := strings.ToLower(strings.TrimSpace(mode))
	if m == "" {
		return "off"
	}
	return m
}

// IsPrometheusMode 判断是否启用 Prometheus 查询。
func IsPrometheusMode(mode string) bool {
	return NormalizeMode(mode) == "prometheus"
}
