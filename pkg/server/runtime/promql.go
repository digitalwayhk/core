package runtime

import (
	"fmt"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/observability"
)

// ServiceRequestRateQuery 生成服务请求率 PromQL（HTTP + Core 入站合计优先 HTTP）。
func ServiceRequestRateQuery(service, window string) (string, error) {
	if err := validateServiceWindow(service, window); err != nil {
		return "", err
	}
	svc := observability.NormalizeServiceLabel(service)
	// 合并 HTTP 入站与 Core 入站（gRPC），避免只统计一种协议。
	return fmt.Sprintf(
		`sum(rate(http_server_requests_code_total{service=%q}[%s])) or vector(0) + sum(rate(core_service_request_requests_total{service=%q}[%s])) or vector(0)`,
		svc, window, svc, window,
	), nil
}

// ServiceHTTPRateByCodeQuery 按 code 分组的 HTTP 速率，用于错误率。
func ServiceHTTPRateByCodeQuery(service, window string) (string, error) {
	if err := validateServiceWindow(service, window); err != nil {
		return "", err
	}
	svc := observability.NormalizeServiceLabel(service)
	return fmt.Sprintf(`sum by (code) (rate(http_server_requests_code_total{service=%q}[%s]))`, svc, window), nil
}

// ServiceCoreRateByResultQuery Core 入站按 result_class。
func ServiceCoreRateByResultQuery(service, window string) (string, error) {
	if err := validateServiceWindow(service, window); err != nil {
		return "", err
	}
	svc := observability.NormalizeServiceLabel(service)
	return fmt.Sprintf(`sum by (result_class) (rate(core_service_request_requests_total{service=%q}[%s]))`, svc, window), nil
}

// ServiceHTTPP95Query HTTP 延迟 p95。
func ServiceHTTPP95Query(service, window string) (string, error) {
	return serviceHTTPQuantileQuery(service, window, 0.95)
}

// ServiceHTTPP50Query HTTP 延迟 p50。
func ServiceHTTPP50Query(service, window string) (string, error) {
	return serviceHTTPQuantileQuery(service, window, 0.50)
}

// ServiceHTTPP99Query HTTP 延迟 p99。
func ServiceHTTPP99Query(service, window string) (string, error) {
	return serviceHTTPQuantileQuery(service, window, 0.99)
}

func serviceHTTPQuantileQuery(service, window string, q float64) (string, error) {
	if err := validateServiceWindow(service, window); err != nil {
		return "", err
	}
	svc := observability.NormalizeServiceLabel(service)
	return fmt.Sprintf(
		`histogram_quantile(%g, sum by (le) (rate(http_server_requests_duration_ms_bucket{service=%q}[%s])))`,
		q, svc, window,
	), nil
}

func serviceCoreQuantileQuery(service, window string, q float64) (string, error) {
	if err := validateServiceWindow(service, window); err != nil {
		return "", err
	}
	svc := observability.NormalizeServiceLabel(service)
	return fmt.Sprintf(
		`histogram_quantile(%g, sum by (le) (rate(core_service_request_duration_ms_bucket{service=%q}[%s])))`,
		q, svc, window,
	), nil
}

func serviceCoreRouteQuantileQuery(service, window string, q float64) (string, error) {
	if err := validateServiceWindow(service, window); err != nil {
		return "", err
	}
	svc := observability.NormalizeServiceLabel(service)
	return fmt.Sprintf(
		`histogram_quantile(%g, sum by (le,route) (rate(core_service_request_duration_ms_bucket{service=%q}[%s])))`,
		q, svc, window,
	), nil
}

// ServiceCallEdgeRateQuery 生成调用边速率查询。
func ServiceCallEdgeRateQuery(window string) (string, error) {
	if _, ok := ParseWindow(window); !ok {
		return "", fmt.Errorf("unsupported window %q", window)
	}
	return fmt.Sprintf(`sum by (source_service,target_service,protocol,result_class) (rate(core_service_call_requests_total[%s]))`, window), nil
}

// ServiceRouteRateQuery 生成服务内路由速率（Core gRPC 入站）。
func ServiceRouteRateQuery(service, window string) (string, error) {
	if err := validateServiceWindow(service, window); err != nil {
		return "", err
	}
	svc := observability.NormalizeServiceLabel(service)
	return fmt.Sprintf(`sum by (route,result_class) (rate(core_service_request_requests_total{service=%q}[%s]))`, svc, window), nil
}

// ServiceHTTPRouteRateQuery HTTP 路径模板速率。
func ServiceHTTPRouteRateQuery(service, window string) (string, error) {
	if err := validateServiceWindow(service, window); err != nil {
		return "", err
	}
	svc := observability.NormalizeServiceLabel(service)
	return fmt.Sprintf(`sum by (path,code) (rate(http_server_requests_code_total{service=%q}[%s]))`, svc, window), nil
}

// ServiceCallP95Query 生成目标服务调用 p95。
func ServiceCallP95Query(service, window string) (string, error) {
	if err := validateServiceWindow(service, window); err != nil {
		return "", err
	}
	svc := observability.NormalizeServiceLabel(service)
	return fmt.Sprintf(
		`histogram_quantile(0.95, sum by (le) (rate(core_service_call_duration_ms_bucket{target_service=%q}[%s])))`,
		svc, window,
	), nil
}

// EventPublishRateQuery 事件发布速率。
func EventPublishRateQuery(window string) (string, error) {
	if _, ok := ParseWindow(window); !ok {
		return "", fmt.Errorf("unsupported window %q", window)
	}
	return fmt.Sprintf(`sum by (source_service,subject_family,event_type,result_class) (rate(core_event_publish_total[%s]))`, window), nil
}

// EventSubscriptionInfoQuery 跨进程订阅事实。
func EventSubscriptionInfoQuery() string {
	return `core_event_subscription_info`
}

// ServiceLastSampleTimestampQuery 返回服务请求序列的最后样本时间（Unix 秒）。
// 使用 timestamp() 读取底层 counter 的最后采集时间，而不是 instant 表达式评估时间。
func ServiceLastSampleTimestampQuery(service string) (string, error) {
	svc := observability.NormalizeServiceLabel(service)
	if svc == "unknown" || !observability.IsSafePromLabel(svc) {
		return "", fmt.Errorf("unsafe service name")
	}
	// max 覆盖 HTTP 与 Core 入站；任一有样本即可。
	return fmt.Sprintf(
		`max(timestamp(http_server_requests_code_total{service=%q}) or timestamp(core_service_request_requests_total{service=%q}))`,
		svc, svc,
	), nil
}

// ComponentGaugeQuery 查询服务组件 gauge。
func ComponentGaugeQuery(service string) (string, error) {
	svc := observability.NormalizeServiceLabel(service)
	if svc == "unknown" || !observability.IsSafePromLabel(svc) {
		return "", fmt.Errorf("unsafe service name")
	}
	return fmt.Sprintf(`core_component_gauge{service=%q}`, svc), nil
}

func validateServiceWindow(service, window string) error {
	if _, ok := ParseWindow(window); !ok {
		return fmt.Errorf("unsupported window %q", window)
	}
	svc := observability.NormalizeServiceLabel(service)
	if svc == "unknown" || !observability.IsSafePromLabel(svc) {
		return fmt.Errorf("unsafe service name")
	}
	if strings.ContainsAny(service, `"{},`) {
		return fmt.Errorf("unsafe service name")
	}
	return nil
}
