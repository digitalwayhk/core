// 本文件为正式 REST 服务提供不读取请求 Header、查询参数和 Body 的安全结构化访问日志。
package rest

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/digitalwayhk/core/pkg/server/observability"
	"github.com/felixge/httpsnoop"
	"github.com/zeromicro/go-zero/core/logx"
	zerorest "github.com/zeromicro/go-zero/rest"
)

// secureRESTLogConfiguration 复制 REST 配置并关闭会在 500 响应时 dump 请求的 go-zero 原生日志。
// 返回值 enabled 表示调用方原本启用了访问日志，应改挂 safeHTTPAccessLog。
func secureRESTLogConfiguration(conf zerorest.RestConf) (secured zerorest.RestConf, enabled bool) {
	secured = conf
	enabled = secured.Middlewares.Log
	secured.Middlewares.Log = false
	return secured, enabled
}

// safeHTTPAccessLog 只记录服务、方法、静态路由、状态码、耗时和响应大小。
func safeHTTPAccessLog(service string, next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metrics := captureSafeHTTPMetrics(next, w, r)
		fields := []logx.LogField{
			logx.Field("service", service),
			logx.Field("method", r.Method),
			logx.Field("route", r.URL.Path),
			logx.Field("status", metrics.code),
			logx.Field("duration_ms", metrics.duration.Milliseconds()),
			logx.Field("response_bytes", metrics.written),
		}
		logger := logx.WithContext(r.Context())
		if metrics.code >= http.StatusInternalServerError {
			logger.Errorw("http_request_failed", fields...)
			return
		}
		logger.Infow("http_request_completed", fields...)
	})
}

// runtimeHTTPMetrics 使用逻辑服务名和已注册路由记录 Core 入站 HTTP 指标。
func runtimeHTTPMetrics(service, route string, next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metrics := captureSafeHTTPMetrics(next, w, r)
		observability.RecordInboundRequest(
			service,
			route,
			"http",
			observability.ClassifyHTTPStatus(metrics.code),
			metrics.duration,
		)
	})
}

type safeHTTPMetrics struct {
	code     int
	duration time.Duration
	written  int64
}

// captureSafeHTTPMetrics 保留底层 ResponseWriter 的可选接口，并把成功 Hijack 视为协议切换。
func captureSafeHTTPMetrics(next http.Handler, w http.ResponseWriter, r *http.Request) safeHTTPMetrics {
	metrics := safeHTTPMetrics{code: http.StatusOK}
	headerWritten := false
	started := time.Now()
	markHeader := func(code int) {
		if headerWritten || (code >= 100 && code < 200 && code != http.StatusSwitchingProtocols) {
			return
		}
		metrics.code = code
		headerWritten = true
	}
	wrapped := httpsnoop.Wrap(w, httpsnoop.Hooks{
		WriteHeader: func(writeHeader httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
			return func(code int) {
				writeHeader(code)
				markHeader(code)
			}
		},
		Write: func(write httpsnoop.WriteFunc) httpsnoop.WriteFunc {
			return func(data []byte) (int, error) {
				written, err := write(data)
				markHeader(http.StatusOK)
				metrics.written += int64(written)
				return written, err
			}
		},
		ReadFrom: func(readFrom httpsnoop.ReadFromFunc) httpsnoop.ReadFromFunc {
			return func(source io.Reader) (int64, error) {
				written, err := readFrom(source)
				markHeader(http.StatusOK)
				metrics.written += written
				return written, err
			}
		},
		Hijack: func(hijack httpsnoop.HijackFunc) httpsnoop.HijackFunc {
			return func() (net.Conn, *bufio.ReadWriter, error) {
				connection, buffer, err := hijack()
				if err == nil {
					markHeader(http.StatusSwitchingProtocols)
				}
				return connection, buffer, err
			}
		},
	})
	next.ServeHTTP(wrapped, r)
	metrics.duration = time.Since(started)
	return metrics
}
