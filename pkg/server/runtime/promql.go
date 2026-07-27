package runtime

import (
	"fmt"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/observability"
)

// ServiceRequestRateQuery 生成服务请求率 PromQL（HTTP 入站 code_total）。
func ServiceRequestRateQuery(service, window string) (string, error) {
	if err := validateServiceWindow(service, window); err != nil {
		return "", err
	}
	svc := observability.NormalizeServiceLabel(service)
	return fmt.Sprintf(`sum(rate(http_server_requests_code_total{service=%q}[%s]))`, svc, window), nil
}

// ServiceCallEdgeRateQuery 生成调用边速率查询。
func ServiceCallEdgeRateQuery(window string) (string, error) {
	if _, ok := ParseWindow(window); !ok {
		return "", fmt.Errorf("unsupported window %q", window)
	}
	return fmt.Sprintf(`sum by (source_service,target_service,protocol,result_class) (rate(core_service_call_requests_total[%s]))`, window), nil
}

// ServiceRouteRateQuery 生成服务内路由速率（Core 入站）。
func ServiceRouteRateQuery(service, window string) (string, error) {
	if err := validateServiceWindow(service, window); err != nil {
		return "", err
	}
	svc := observability.NormalizeServiceLabel(service)
	return fmt.Sprintf(`sum by (route,result_class) (rate(core_service_request_requests_total{service=%q}[%s]))`, svc, window), nil
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
