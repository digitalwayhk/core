// Package observability 提供 Core 低基数运行时指标的标签规范化与记录入口。
package observability

import (
	"context"
	"errors"
	"net"
	"strings"
	"unicode"
)

// 稳定 result_class 闭集。
const (
	ResultSuccess     = "success"
	ResultClientError = "client_error"
	ResultServerError = "server_error"
	ResultTimeout     = "timeout"
	ResultUnavailable = "unavailable"
	ResultRejected    = "rejected"
)

// NormalizeServiceLabel 将服务名规范为小写 trimmed 标签。
// 空值或包含空白的名称回退为 unknown，避免高基数或非法标签。
func NormalizeServiceLabel(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return "unknown"
	}
	for _, r := range v {
		if unicode.IsSpace(r) {
			return "unknown"
		}
	}
	return v
}

// NormalizeRouteLabel 仅接受稳定路由模板（以 / 开头且无 query/fragment）。
func NormalizeRouteLabel(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || !strings.HasPrefix(v, "/") || strings.ContainsAny(v, "?#") {
		return "invalid_route"
	}
	return v
}

// NormalizeProtocol 规范协议标签。
func NormalizeProtocol(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "grpc":
		return "grpc"
	case "http":
		return "http"
	case "local":
		return "local"
	default:
		return "unknown"
	}
}

// NormalizeResultClass 将结果类别收敛到闭集。
func NormalizeResultClass(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case ResultSuccess:
		return ResultSuccess
	case ResultClientError:
		return ResultClientError
	case ResultServerError:
		return ResultServerError
	case ResultTimeout:
		return ResultTimeout
	case ResultUnavailable:
		return ResultUnavailable
	case ResultRejected:
		return ResultRejected
	default:
		return ResultUnavailable
	}
}

// ClassifyHTTPStatus 将 HTTP 状态码映射为 result_class。
func ClassifyHTTPStatus(code int) string {
	switch {
	case code >= 200 && code < 400:
		return ResultSuccess
	case code >= 400 && code < 500:
		return ResultClientError
	default:
		return ResultServerError
	}
}

// ClassifyError 将错误映射为 result_class。
func ClassifyError(err error) string {
	if err == nil {
		return ResultSuccess
	}
	if errors.Is(err, context.Canceled) {
		return ResultClientError
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ResultTimeout
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return ResultTimeout
	}
	return ResultUnavailable
}

// IsSafePromLabel 校验值是否可安全嵌入 PromQL 标签字面量。
func IsSafePromLabel(v string) bool {
	if v == "" {
		return false
	}
	for _, r := range v {
		if r == '"' || r == '{' || r == '}' || r == ',' || r == '\\' || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}
